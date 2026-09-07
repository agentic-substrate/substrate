package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/agentic-substrate/substrate/internal/render"
)

// SyncResult reports remotes the server does not know, which must never
// become a scope (EDD R18), and targets whose drift could not be proposed.
type SyncResult struct {
	UnknownRemotes []string
	// UnproposedDrift is the on-disk paths whose drift proposal failed. Those
	// files were left exactly as found: the proposal is filed before the file
	// is overwritten, and a failed proposal means no write (#59).
	UnproposedDrift []string
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
	keep := map[string]struct{}{}
	var remotes []string
	seen := map[string]struct{}{}
	for _, ws := range checkouts {
		if err := db.upsertWorkspace(ws, now); err != nil {
			return SyncResult{}, err
		}
		keep[ws.Path] = struct{}{}
		if ws.Remote == "" {
			continue
		}
		if _, ok := seen[ws.Remote]; ok {
			continue
		}
		seen[ws.Remote] = struct{}{}
		remotes = append(remotes, ws.Remote)
	}
	if err := db.pruneWorkspaces(keep); err != nil {
		return SyncResult{}, err
	}

	a := newAPI(cfg)
	targets, known, unknown, scopes, err := fetchTargets(ctx, a, cfg, remotes)
	if err != nil {
		return SyncResult{}, err
	}
	for _, u := range unknown {
		slog.Warn("unknown git remote; bind it with substrate repo bind", "remote", u)
	}

	applyTo := checkoutsWithRemotes(checkouts, known)
	var unproposed []string
	for _, tgt := range targets {
		dests, err := destTargets(cfg, tgt.Path, applyTo, scopes)
		if err != nil {
			return SyncResult{UnknownRemotes: unknown}, err
		}
		for _, dest := range dests {
			skipped, err := applyTarget(ctx, db, a, cfg, dest, tgt, now)
			if err != nil {
				return SyncResult{UnknownRemotes: unknown, UnproposedDrift: unproposed}, err
			}
			if skipped {
				unproposed = append(unproposed, dest.path)
			}
		}
	}
	if err := linkSkills(ctx, db, a, cfg, known); err != nil {
		return SyncResult{UnknownRemotes: unknown, UnproposedDrift: unproposed}, err
	}
	return SyncResult{UnknownRemotes: unknown, UnproposedDrift: unproposed}, nil
}

// fetchTargets probes each remote on its own so an unknown one is isolated
// (R18: logged, never auto-created), and records the scope chain the server
// compiled that remote's targets for. That per-remote scope is what a drift
// proposal carries.
func fetchTargets(ctx context.Context, a *api, cfg Config, remotes []string) ([]renderTarget, []string, []string, map[string]string, error) {
	var known, unknown []string
	scopes := map[string]string{}
	for _, r := range remotes {
		res, code, err := a.getRender(ctx, cfg.Machine, []string{r})
		if err != nil && deniedStatus(code) {
			unknown = append(unknown, r)
			continue
		}
		if err != nil {
			return nil, nil, nil, nil, err
		}
		sc, err := scopeForRemote(r, res.Scope)
		if err != nil {
			// A remote whose scope cannot be established is not managed: it is
			// never guessed at and never defaulted to global.
			slog.Warn("no usable scope for remote; skipping checkout", "remote", r, "err", err)
			continue
		}
		scopes[r] = sc
		known = append(known, r)
	}
	res, _, err := a.getRender(ctx, cfg.Machine, known)
	if err != nil {
		return nil, known, unknown, scopes, err
	}
	return res.Targets, known, unknown, scopes, nil
}

func checkoutsWithRemotes(checkouts []Workspace, remotes []string) []Workspace {
	allow := map[string]struct{}{}
	for _, r := range remotes {
		allow[r] = struct{}{}
	}
	var out []Workspace
	for _, c := range checkouts {
		if _, ok := allow[c.Remote]; ok {
			out = append(out, c)
		}
	}
	return out
}

// destTarget is one file to write plus the scope a drift proposal about it
// must carry. The scope comes from the checkout's git remote (via the server's
// binding for it), not from where the checkout happens to sit on disk.
type destTarget struct {
	path  string
	scope string
}

func destTargets(cfg Config, serverPath string, checkouts []Workspace, scopes map[string]string) ([]destTarget, error) {
	if strings.HasPrefix(serverPath, "~/") {
		dests, err := destsFor(cfg, serverPath, nil)
		if err != nil {
			return nil, err
		}
		sc, err := cfg.homeScope()
		if err != nil {
			// Not fatal: the file is still rendered. Only a drift proposal
			// needs a scope, and applyTarget refuses to overwrite without one.
			slog.Warn("no scope for home-scoped target; drift cannot be proposed", "path", serverPath, "err", err)
		}
		out := make([]destTarget, 0, len(dests))
		for _, d := range dests {
			out = append(out, destTarget{path: d, scope: sc})
		}
		return out, nil
	}
	// Validate the path once even with no checkouts, so an unsafe target is
	// refused on a machine that happens to have discovered nothing.
	if _, err := destsFor(cfg, serverPath, nil); err != nil {
		return nil, err
	}
	var out []destTarget
	for _, c := range checkouts {
		dests, err := destsFor(cfg, serverPath, []Workspace{c})
		if err != nil {
			return nil, err
		}
		for _, d := range dests {
			out = append(out, destTarget{path: d, scope: scopes[c.Remote]})
		}
	}
	return out, nil
}

// applyTarget renders one target. It reports whether drift was detected but
// could not be proposed, in which case the file on disk is left untouched.
//
// The POST-before-write ordering is load-bearing: the proposal is filed before
// the local file is overwritten, and the write is refused when the POST fails,
// so a hand edit is never destroyed without having been reported. A failure
// degrades this one target; it must not stop the loop or the daemon (#59).
func applyTarget(ctx context.Context, db *DB, a *api, cfg Config, dest destTarget, tgt renderTarget, now int64) (bool, error) {
	disk, err := os.ReadFile(dest.path) //nolint:gosec // dest is confined to Home or a discovered checkout
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("adapter: read %s: %w", dest.path, err)
	}
	stored, hasStored, err := db.getManaged(dest.path)
	if err != nil {
		return false, err
	}

	// Compare with DriftHash, never a plain hash of the file (Gotcha 9).
	// A missing managed_file row is drift, not a blank disk: first run over a
	// preexisting file, or after adapter.sqlite was deleted, must not clobber.
	diskHash := render.DriftHash(string(disk))
	tgtHash := render.DriftHash(tgt.Content)
	if exists && (!hasStored || diskHash != stored.SHA256) && diskHash != tgtHash {
		// Drift is counted whether or not the proposal lands, so a file that
		// can never be proposed is visible instead of silently retried.
		if cfg.Metrics != nil {
			cfg.Metrics.RecordRenderDrift(ctx, cfg.Machine)
		}
		if dest.scope == "" {
			slog.Error("drift not proposed: no scope for target; leaving file untouched", "path", dest.path)
			return true, nil
		}
		diff := unifiedDiff(tgt.Path, string(disk), tgt.Content)
		if err := a.postReview(ctx, dest.scope, dest.path, diff); err != nil {
			slog.Error("drift proposal rejected; leaving file untouched", "path", dest.path, "scope", dest.scope, "err", err)
			return true, nil
		}
		if err := AtomicWrite(dest.path, []byte(tgt.Content)); err != nil {
			return false, err
		}
	} else if !exists || string(disk) != tgt.Content {
		if err := AtomicWrite(dest.path, []byte(tgt.Content)); err != nil {
			return false, err
		}
	}

	sum := render.DriftHash(tgt.Content)
	return false, db.upsertManaged(dest.path, targetName(tgt.Path), sum, now)
}
