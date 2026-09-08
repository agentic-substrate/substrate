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
	// UnscopedRemotes is the remotes the server knows but the adapter cannot
	// name a repo key for, so their checkouts are not managed this cycle. A
	// silent skip here unmanages every repo on the machine while Sync returns
	// nil, which is exactly the failure mode UnknownRemotes exists to prevent.
	UnscopedRemotes []string
	// UnproposedDrift is the on-disk paths whose drift proposal failed. Those
	// files were left exactly as found: the proposal is filed before the file
	// is overwritten, and a failed proposal means no write (#59).
	UnproposedDrift []string
	// SkillsSkipped is why skills were not linked this cycle, empty when they
	// were. Both reasons -- no remote configured, and a remote that could not
	// be cloned or fetched -- are deliberately non-fatal: skills failing is not
	// a reason to stop rendering instructions. But a silent non-fatal skip
	// makes a daemon with skills entirely off indistinguishable from one that
	// linked every skill, which is the same failure mode UnscopedRemotes exists
	// to prevent. deploy/server/configmap.yaml ships SUBSTRATE_SKILLS_REPO
	// empty, so the unconfigured case is the default, not an edge case.
	SkillsSkipped string
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
	targets, known, unknown, unscoped, repoKeys, err := fetchTargets(ctx, a, cfg, remotes)
	if err != nil {
		return SyncResult{}, err
	}
	for _, u := range unknown {
		slog.Warn("unknown git remote; bind it with substrate repo bind", "remote", u)
	}
	for _, u := range unscoped {
		slog.Warn("no repo key for remote; checkout not managed", "remote", u)
	}

	applyTo := checkoutsWithRemotes(checkouts, known)
	var unproposed []string
	var skillsSkipped string
	res := func() SyncResult {
		return SyncResult{
			UnknownRemotes:  unknown,
			UnscopedRemotes: unscoped,
			UnproposedDrift: unproposed,
			SkillsSkipped:   skillsSkipped,
		}
	}
	for _, tgt := range targets {
		dests, err := destTargets(cfg, tgt.Path, applyTo, repoKeys)
		if err != nil {
			return res(), err
		}
		for _, dest := range dests {
			skipped, err := applyTarget(ctx, db, a, cfg, dest, tgt, now)
			if err != nil {
				return res(), err
			}
			if skipped {
				unproposed = append(unproposed, dest.path)
			}
		}
	}
	skipped, err := linkSkills(ctx, db, a, cfg, known)
	if err != nil {
		return res(), err
	}
	skillsSkipped = skipped
	return res(), nil
}

// fetchTargets probes each remote on its own so an unknown one is isolated
// (R18: logged, never auto-created), and records the repo key a drift proposal
// about that checkout must name. The key is the normalized remote itself; the
// chain behind it belongs to the server and is never sent to the adapter.
//
// A remote the server accepts but ParseRemote cannot key is returned in
// `unscoped` rather than dropped: an unmanaged checkout must be visible in the
// result, not only in a log line.
func fetchTargets(ctx context.Context, a *api, cfg Config, remotes []string) ([]renderTarget, []string, []string, []string, map[string]string, error) {
	var known, unknown, unscoped []string
	repoKeys := map[string]string{}
	for _, r := range remotes {
		_, code, err := a.getRender(ctx, cfg.Machine, []string{r})
		if err != nil && deniedStatus(code) {
			unknown = append(unknown, r)
			continue
		}
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		id, ok := ParseRemote(r)
		if !ok {
			// The checkout is not managed: its repo key is never guessed at
			// and never defaulted to global.
			unscoped = append(unscoped, r)
			continue
		}
		repoKeys[r] = id.Key
		known = append(known, r)
	}
	res, _, err := a.getRender(ctx, cfg.Machine, known)
	if err != nil {
		return nil, known, unknown, unscoped, repoKeys, err
	}
	return res.Targets, known, unknown, unscoped, repoKeys, nil
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

// destTarget is one file to write plus how a drift proposal about it names its
// scope. A file inside a checkout carries `repo`, the key derived from that
// checkout's git remote rather than from where it sits on disk, and the server
// resolves it to a chain. A home file has no repo and carries `scope`. Exactly
// one is set; with neither, drift cannot be proposed and the file is left
// alone.
type destTarget struct {
	path  string
	scope string
	repo  string
}

// proposable reports whether a drift proposal about this target can name a
// scope at all.
func (d destTarget) proposable() bool { return d.scope != "" || d.repo != "" }

func destTargets(cfg Config, serverPath string, checkouts []Workspace, repoKeys map[string]string) ([]destTarget, error) {
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
			out = append(out, destTarget{path: d, repo: repoKeys[c.Remote]})
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
		// A file that can never be proposed would otherwise emit one metric
		// increment and one ERROR line every interval forever. The report is
		// backed off per path; the target still appears in UnproposedDrift on
		// every cycle, so the operator signal is throttled, never silenced.
		report, suppressed := db.shouldReportDrift(dest.path, now)
		if report && cfg.Metrics != nil {
			cfg.Metrics.RecordRenderDrift(ctx, cfg.Machine)
		}
		if !dest.proposable() {
			if report {
				slog.Error("drift not proposed: no scope for target; leaving file untouched",
					"path", dest.path, "suppressed_cycles", suppressed)
			}
			return true, nil
		}
		diff := unifiedDiff(tgt.Path, string(disk), tgt.Content)
		if err := a.postReview(ctx, dest.scope, dest.repo, dest.path, diff); err != nil {
			if report {
				slog.Error("drift proposal rejected; leaving file untouched",
					"path", dest.path, "scope", dest.scope, "repo", dest.repo,
					"suppressed_cycles", suppressed, "err", err)
			}
			return true, nil
		}
		db.clearDriftBackoff(dest.path)
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

// Report logs whatever this cycle did not manage. Sync returns nil for all of
// these -- an unknown remote, an unkeyable one, a refused drift proposal and a
// missing skills repo are each non-fatal by design -- so without this the
// daemon reports healthy while managing less than it should, and every field
// on SyncResult is asserted by tests and then discarded by the binary.
func (r SyncResult) Report() {
	if len(r.UnknownRemotes) > 0 {
		slog.Warn("unknown git remotes; bind them with substrate repo bind", "remotes", r.UnknownRemotes)
	}
	if len(r.UnscopedRemotes) > 0 {
		slog.Warn("no repo key for remotes; those checkouts are not managed", "remotes", r.UnscopedRemotes)
	}
	if len(r.UnproposedDrift) > 0 {
		slog.Warn("drift proposal failed; those files were left as found", "paths", r.UnproposedDrift)
	}
	if r.SkillsSkipped != "" {
		slog.Warn("skills not linked this cycle", "reason", r.SkillsSkipped)
	}
}
