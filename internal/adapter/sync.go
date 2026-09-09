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

// RenderVersion identifies the shape of the render the adapter writes, not the
// content. Bump it whenever a change to the rendering rules makes the correct
// content of an already-managed file different -- as #111 did, when
// home-scoped files stopped carrying every repo's instructions. Drift review
// cannot catch that class: the file on disk still matches the hash the adapter
// stored, so applyTarget falls straight through to the write and the operator
// sees a smaller file with no diff and no warning (#112). Do not bump it for
// an ordinary instruction change; every bump costs one backup per managed file
// on every machine.
const RenderVersion = 1

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
	// Backups is the backup files written this cycle because the render rules
	// changed shape under an already-managed file (#112). Empty on every
	// steady-state cycle; non-empty exactly once per path per RenderVersion
	// bump.
	Backups []string
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
	plan, err := fetchTargets(ctx, a, cfg, remotes)
	if err != nil {
		return SyncResult{}, err
	}
	var unproposed []string
	var backups []string
	var skillsSkipped string
	res := func() SyncResult {
		return SyncResult{
			UnknownRemotes:  plan.unknown,
			UnscopedRemotes: plan.unscoped,
			UnproposedDrift: unproposed,
			Backups:         backups,
			SkillsSkipped:   skillsSkipped,
		}
	}
	apply := func(tgt renderTarget, dests []destTarget) error {
		for _, dest := range dests {
			skipped, backup, err := applyTarget(ctx, db, a, cfg, dest, tgt, now)
			if err != nil {
				return err
			}
			if skipped {
				unproposed = append(unproposed, dest.path)
			}
			if backup != "" {
				backups = append(backups, backup)
			}
		}
		return nil
	}
	// Each checkout is written from the render compiled for its own repo, and
	// from no other (SCOPE-1, INST-4). Asking about every remote at once and
	// fanning the one answer out gave every repo on the machine every other
	// repo's instructions, with first-occurrence-wins deciding which of two
	// conflicting rules a project actually got.
	for _, rr := range plan.repos {
		mine := checkoutsWithRemotes(checkouts, []string{rr.remote})
		for _, tgt := range rr.targets {
			if isHomePath(tgt.Path) {
				continue
			}
			dests, err := destTargets(cfg, tgt.Path, mine, plan.repoKeys)
			if err != nil {
				return res(), err
			}
			if err := apply(tgt, dests); err != nil {
				return res(), err
			}
		}
	}
	// Every target of the machine-wide render is validated, including the
	// repo-relative ones: with no checkouts they yield no destination and are
	// written nowhere, but an unsafe path must still be refused on a machine
	// that happens to have discovered nothing.
	for _, tgt := range plan.home {
		dests, err := destTargets(cfg, tgt.Path, nil, nil)
		if err != nil {
			return res(), err
		}
		if err := apply(tgt, dests); err != nil {
			return res(), err
		}
	}
	skillsSkipped, err = linkSkills(ctx, db, a, cfg, plan.known)
	if err != nil {
		// Assigned before the check, not after: an error here means skills were
		// definitively not linked, and a result reporting no skip would be a
		// lie in exactly the case that most needs reporting.
		return res(), err
	}
	return res(), nil
}

// repoRender is one checkout-scoped render: the remote it was compiled for and
// the targets the server returned for that remote alone.
type repoRender struct {
	remote  string
	targets []renderTarget
}

// renderPlan is everything one cycle needs to write: one render per known
// remote, plus the machine-wide render behind the home-scoped files.
type renderPlan struct {
	repos    []repoRender
	home     []renderTarget
	known    []string
	unknown  []string
	unscoped []string
	repoKeys map[string]string
}

// isHomePath reports whether a server target path is the machine-wide copy
// rather than a per-checkout file. The server returns both in one flat array
// (EDD §6), so the split is made here, by path.
func isHomePath(serverPath string) bool { return strings.HasPrefix(serverPath, "~/") }

// fetchTargets renders each remote on its own so an unknown one is isolated
// (R18: logged, never auto-created), so its content reaches only its own
// checkouts, and so the repo key a drift proposal must name is recorded. The
// key is the normalized remote itself; the chain behind it belongs to the
// server and is never sent to the adapter.
//
// The home-scoped files get their own request naming no repo at all, which the
// server resolves to the global scope. There is exactly one of each per
// machine, so they cannot carry any one repo's rules — and Claude Code loads
// ~/.claude/CLAUDE.md on every session, which makes a merged blob there the
// most damaging instance of the defect, not the mildest. Global is the only
// content that is true on the whole machine.
//
// A remote the server accepts but ParseRemote cannot key is returned in
// `unscoped` rather than dropped: an unmanaged checkout must be visible in the
// result, not only in a log line.
func fetchTargets(ctx context.Context, a *api, cfg Config, remotes []string) (renderPlan, error) {
	plan := renderPlan{repoKeys: map[string]string{}}
	for _, r := range remotes {
		res, code, err := a.getRender(ctx, cfg.Machine, []string{r})
		if err != nil && deniedStatus(code) {
			plan.unknown = append(plan.unknown, r)
			continue
		}
		if err != nil {
			return renderPlan{}, err
		}
		id, ok := ParseRemote(r)
		if !ok {
			// The checkout is not managed: its repo key is never guessed at
			// and never defaulted to global.
			plan.unscoped = append(plan.unscoped, r)
			continue
		}
		plan.repoKeys[r] = id.Key
		plan.known = append(plan.known, r)
		plan.repos = append(plan.repos, repoRender{remote: r, targets: res.Targets})
	}
	res, _, err := a.getRender(ctx, cfg.Machine, nil)
	if err != nil {
		return renderPlan{
			known: plan.known, unknown: plan.unknown,
			unscoped: plan.unscoped, repoKeys: plan.repoKeys,
		}, err
	}
	plan.home = res.Targets
	return plan, nil
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
// could not be proposed, in which case the file on disk is left untouched, and
// the path of any backup it took before overwriting.
//
// The POST-before-write ordering is load-bearing: the proposal is filed before
// the local file is overwritten, and the write is refused when the POST fails,
// so a hand edit is never destroyed without having been reported. A failure
// degrades this one target; it must not stop the loop or the daemon (#59).
func applyTarget(ctx context.Context, db *DB, a *api, cfg Config, dest destTarget, tgt renderTarget, now int64) (bool, string, error) {
	disk, err := os.ReadFile(dest.path) //nolint:gosec // dest is confined to Home or a discovered checkout
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return false, "", fmt.Errorf("adapter: read %s: %w", dest.path, err)
	}
	stored, hasStored, err := db.getManaged(dest.path)
	if err != nil {
		return false, "", err
	}
	var backup string

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
			return true, "", nil
		}
		diff := unifiedDiff(tgt.Path, string(disk), tgt.Content)
		if err := a.postReview(ctx, dest.scope, dest.repo, dest.path, diff); err != nil {
			if report {
				slog.Error("drift proposal rejected; leaving file untouched",
					"path", dest.path, "scope", dest.scope, "repo", dest.repo,
					"suppressed_cycles", suppressed, "err", err)
			}
			return true, "", nil
		}
		db.clearDriftBackoff(dest.path)
		if err := AtomicWrite(dest.path, []byte(tgt.Content)); err != nil {
			return false, "", err
		}
	} else if !exists || string(disk) != tgt.Content {
		// The rules changed under a file the adapter itself wrote, so the
		// content about to be lost was never reviewed by anyone. Keep it
		// where the operator can find it before overwriting (#112). Only the
		// first cycle after a bump takes this path: the write below records
		// the current RenderVersion, so the next cycle compares equal and
		// Gotcha 9 still holds.
		if exists && hasStored && stored.RenderVersion < RenderVersion {
			backup = fmt.Sprintf("%s.v%d.bak", dest.path, stored.RenderVersion)
			if err := AtomicWrite(backup, disk); err != nil {
				return false, "", fmt.Errorf("adapter: backup %s: %w", dest.path, err)
			}
		}
		if err := AtomicWrite(dest.path, []byte(tgt.Content)); err != nil {
			return false, "", err
		}
	}

	sum := render.DriftHash(tgt.Content)
	return false, backup, db.upsertManaged(dest.path, targetName(tgt.Path), sum, now)
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
	if len(r.Backups) > 0 {
		slog.Warn("the render changed shape; the previous content of those files was backed up before overwriting",
			"backups", r.Backups, "render_version", RenderVersion)
	}
	if r.SkillsSkipped != "" {
		slog.Warn("skills not linked this cycle", "reason", r.SkillsSkipped)
	}
}
