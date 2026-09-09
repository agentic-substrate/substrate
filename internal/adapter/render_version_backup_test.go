package adapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentic-substrate/substrate/internal/render"
)

// A machine synced before the per-checkout split holds a merged all-repos blob
// in ~/.claude/CLAUDE.md, and the adapter's own stored hash matches it -- so
// drift review never fires and the much smaller global-only render replaces it
// with no diff, no warning and nothing to recover (#112). The first sync after
// a RenderVersion bump must leave the old content on disk somewhere.
func TestFirstSyncAfterRenderVersionBumpBacksUpTheHomeFile(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	initRepo(t, alpha, "https://github.com/acme/alpha.git")

	const globalBody = "global-only rules\n"
	const mergedBlob = "rules for github.com/acme/alpha\nrules for github.com/acme/beta\n"
	srv := newPerRepoFake(t, func(repos []string) string {
		if len(repos) == 0 {
			return globalBody
		}
		return "rules for " + repos[0] + "\n"
	})

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	// Seed the pre-upgrade steady state: the merged blob on disk, and a
	// managed_file row whose hash matches it. Nothing here is drift.
	claude := filepath.Join(home, ".claude/CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(claude), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte(mergedBlob), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.upsertManaged(claude, "claude", render.DriftHash(mergedBlob), 1); err != nil {
		t.Fatal(err)
	}
	// upsertManaged stamps the current version; an adapter that wrote this file
	// before the bump did not.
	if _, err := db.sql.Exec(`UPDATE managed_file SET render_version = 0 WHERE path = ?`, claude); err != nil {
		t.Fatal(err)
	}

	res, err := Sync(t.Context(), db, cfg)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// The new content is correct and must still land.
	if got := readFile(t, claude); got != globalBody {
		t.Fatalf("~/.claude/CLAUDE.md = %q, want the global render %q", got, globalBody)
	}
	// But the merged blob must be recoverable.
	backup := claude + ".v0.bak"
	if got := readFile(t, backup); got != mergedBlob {
		t.Fatalf("backup = %q, want the pre-upgrade content %q; the old rules were deleted with nothing to recover", got, mergedBlob)
	}
	// AC2: the operator learns the file changed shape, not just that a sync
	// succeeded. A backup nobody is told about is not a recovery path.
	found := false
	for _, p := range res.Backups {
		if p == backup {
			found = true
		}
	}
	if !found {
		t.Fatalf("SyncResult.Backups = %v, want it to name %q", res.Backups, backup)
	}
}

// AC3 and AC4: the bump costs exactly one backup per path. An ordinary content
// change on a machine already at the current RenderVersion writes straight
// through, and a second cycle backs nothing up -- otherwise every cycle after
// an upgrade litters the home directory and Gotcha 9's noise problem returns.
func TestSteadyStateRerenderTakesNoBackup(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	initRepo(t, alpha, "https://github.com/acme/alpha.git")

	body := "first body\n"
	srv := newPerRepoFake(t, func([]string) string { return body })

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	// Cycle 1 creates the files, stamping the current RenderVersion.
	if res, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("cycle 1: %v", err)
	} else if len(res.Backups) != 0 {
		t.Fatalf("cycle 1 backed up %v; a first render over no file has nothing to lose", res.Backups)
	}

	// Cycle 2: the server's instructions changed. This is the ordinary case,
	// not a rules change, and must not be treated as one.
	body = "second body, quite different\n"
	res, err := Sync(t.Context(), db, cfg)
	if err != nil {
		t.Fatalf("cycle 2: %v", err)
	}
	if len(res.Backups) != 0 {
		t.Fatalf("an ordinary instruction change backed up %v; only a RenderVersion bump may", res.Backups)
	}
	claude := filepath.Join(home, ".claude/CLAUDE.md")
	if got := readFile(t, claude); got != body {
		t.Fatalf("~/.claude/CLAUDE.md = %q, want %q", got, body)
	}
	if _, err := os.Stat(claude + ".v0.bak"); !os.IsNotExist(err) {
		t.Fatalf("a backup file exists beside a steady-state render: %v", err)
	}
}
