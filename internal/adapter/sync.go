package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/agentic-substrate/substrate/internal/render"
)

// SyncResult reports remotes the server does not know, which must never
// become a scope (EDD R18).
type SyncResult struct {
	UnknownRemotes []string
}

// Sync discovers checkouts, pulls /v1/render, files drift, then writes.
func Sync(ctx context.Context, db *DB, cfg Config) (SyncResult, error) {
	if err := cfg.validate(); err != nil {
		return SyncResult{}, err
	}
	if db == nil {
		return SyncResult{}, fmt.Errorf("adapter: nil db")
	}
	checkouts, err := Discover(cfg.Roots)
	if err != nil {
		return SyncResult{}, err
	}
	now := time.Now().Unix()
	var remotes []string
	seen := map[string]struct{}{}
	for _, ws := range checkouts {
		if err := db.upsertWorkspace(ws, now); err != nil {
			return SyncResult{}, err
		}
		if ws.Remote == "" {
			continue
		}
		if _, ok := seen[ws.Remote]; ok {
			continue
		}
		seen[ws.Remote] = struct{}{}
		remotes = append(remotes, ws.Remote)
	}

	a := newAPI(cfg)
	targets, unknown, err := fetchTargets(ctx, a, cfg, remotes)
	if err != nil {
		return SyncResult{}, err
	}
	for _, u := range unknown {
		slog.Warn("unknown git remote; bind it with substrate repo bind", "remote", u)
	}

	for _, tgt := range targets {
		dests, err := destsFor(cfg, tgt.Path, checkouts)
		if err != nil {
			return SyncResult{UnknownRemotes: unknown}, err
		}
		for _, dest := range dests {
			if err := applyTarget(ctx, db, a, cfg, dest, tgt, now); err != nil {
				return SyncResult{UnknownRemotes: unknown}, err
			}
		}
	}
	return SyncResult{UnknownRemotes: unknown}, nil
}

func fetchTargets(ctx context.Context, a *api, cfg Config, remotes []string) ([]renderTarget, []string, error) {
	var known, unknown []string
	for _, r := range remotes {
		_, code, err := a.getRender(ctx, cfg.Machine, []string{r})
		if err != nil && deniedStatus(code) {
			unknown = append(unknown, r)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		known = append(known, r)
	}
	targets, _, err := a.getRender(ctx, cfg.Machine, known)
	if err != nil {
		return nil, unknown, err
	}
	return targets, unknown, nil
}

func applyTarget(ctx context.Context, db *DB, a *api, cfg Config, dest string, tgt renderTarget, now int64) error {
	disk, err := os.ReadFile(dest) //nolint:gosec // dest is confined to Home or a discovered checkout
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("adapter: read %s: %w", dest, err)
	}
	stored, hasStored, err := db.getManaged(dest)
	if err != nil {
		return err
	}

	// Compare with DriftHash, never a plain hash of the file (Gotcha 9).
	if exists && hasStored && render.DriftHash(string(disk)) != stored.SHA256 {
		diff := unifiedDiff(tgt.Path, string(disk), tgt.Content)
		if err := a.postReview(ctx, cfg.scope(), dest, diff); err != nil {
			return err
		}
		if err := AtomicWrite(dest, []byte(tgt.Content)); err != nil {
			return err
		}
	} else if !exists || string(disk) != tgt.Content {
		if err := AtomicWrite(dest, []byte(tgt.Content)); err != nil {
			return err
		}
	}

	sum := render.DriftHash(tgt.Content)
	return db.upsertManaged(dest, targetName(tgt.Path), sum, now)
}
