package adapter

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRejectedProposalDegradesOneTargetOnly is the #59 acceptance. One of two
// checkouts has its drift proposal rejected: that file keeps its hand-edited
// content, the other checkout is still rendered, Sync returns no error, and the
// rejection is reported rather than swallowed.
func TestRejectedProposalDegradesOneTargetOnly(t *testing.T) {
	root := t.TempDir()
	rejected := filepath.Join(root, "rejected")
	accepted := filepath.Join(root, "accepted")
	initRepo(t, rejected, "https://github.com/acme/rejected.git")
	initRepo(t, accepted, "https://github.com/acme/accepted.git")
	const handEdit = "hand-edited, must survive a rejected proposal\n"
	for _, dir := range []string{rejected, accepted} {
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(handEdit), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	home := t.TempDir()
	srv := newFake(t)
	srv.bound["github.com/acme/rejected"] = true
	srv.bound["github.com/acme/accepted"] = true
	srv.rejectPath = rejected
	tgt := agentsTarget(t)
	srv.setTargets(tgt)
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	res, err := Sync(t.Context(), db, cfg)
	if err != nil {
		t.Fatalf("one rejected proposal must not fail Sync: %v", err)
	}

	rejectedFile := filepath.Join(rejected, "AGENTS.md")
	if got := readFile(t, rejectedFile); got != handEdit {
		t.Fatalf("un-proposed file was overwritten: %q", got)
	}
	if got := readFile(t, filepath.Join(accepted, "AGENTS.md")); got != tgt.Content {
		t.Fatal("a later target was not processed after an earlier one was rejected")
	}
	if len(res.UnproposedDrift) != 1 || res.UnproposedDrift[0] != rejectedFile {
		t.Fatalf("UnproposedDrift = %v, want [%s]; a permanently unproposable file must not fail silently", res.UnproposedDrift, rejectedFile)
	}

	// The surviving target's proposal was still filed.
	posted := srv.reviews()
	if len(posted) != 1 || posted[0].Path != filepath.Join(accepted, "AGENTS.md") {
		t.Fatalf("proposals = %+v, want exactly the accepted checkout's", posted)
	}
}

// TestDriftIsCountedEvenWhenTheProposalFails: an unreportable drift must be
// visible, not silent (#59).
func TestDriftIsCountedEvenWhenTheProposalFails(t *testing.T) {
	home := t.TempDir()
	dest := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("hand-edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newFake(t)
	srv.rejectPath = dest
	srv.setTargets(homeTarget(claudeAt(t, "2026-07-02T00:00:00Z")))
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	m := &captureMetrics{}
	cfg.Metrics = m

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if m.drift != 1 {
		t.Fatalf("render drift counted %d times, want 1; an unproposable file would otherwise be retried forever in silence", m.drift)
	}
}
