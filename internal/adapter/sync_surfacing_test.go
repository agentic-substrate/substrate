package adapter

import (
	"os"
	"path/filepath"
	"testing"
)

// A remote the server accepts but the adapter cannot key is unmanaged: its
// files are not rendered and its drift is not proposed. That is the same class
// of failure as an unbound remote, so it must reach the caller in SyncResult,
// not only a log line. Swallowing it lets a version skew or a schema gap
// unmanage every repo on the machine while Sync returns nil and both result
// slices are empty -- a daemon reporting healthy while doing nothing.
func TestRemoteWithoutRepoKeyIsSurfacedNotSwallowed(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "known")
	bad := filepath.Join(root, "legacy")
	initRepo(t, good, "https://github.com/acme/known.git")
	// A hostless local-path remote: the server may well know it, but it names
	// no host and so yields no repo key.
	initRepo(t, bad, "/srv/git/legacy")

	home := t.TempDir()
	srv := newFake(t)
	srv.bound["github.com/acme/known"] = true
	srv.bound["/srv/git/legacy"] = true
	tgt := agentsTarget(t)
	srv.setTargets(tgt)
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	res, err := Sync(t.Context(), db, cfg)
	if err != nil {
		t.Fatalf("one unkeyable remote must not fail Sync: %v", err)
	}
	if len(res.UnknownRemotes) != 0 {
		t.Fatalf("UnknownRemotes = %v, want none; the server accepted both remotes", res.UnknownRemotes)
	}
	if len(res.UnscopedRemotes) != 1 || res.UnscopedRemotes[0] != "/srv/git/legacy" {
		t.Fatalf("UnscopedRemotes = %v, want [/srv/git/legacy]; an unmanaged checkout must not vanish from the result", res.UnscopedRemotes)
	}

	// The claim has teeth: the keyable checkout really was managed, so an
	// empty UnscopedRemotes could not be explained by nothing having run.
	if got := readFile(t, filepath.Join(good, "AGENTS.md")); got != tgt.Content {
		t.Fatalf("keyable checkout was not rendered: %q", got)
	}
	if _, err := os.Stat(filepath.Join(bad, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("unkeyable checkout was written to anyway: %v", err)
	}
}

// A file whose proposal can never land is re-detected every render tick. The
// operator signal must survive -- the path stays in UnproposedDrift on every
// cycle -- but the metric and the ERROR line must back off instead of
// repeating forever, once per file, per tick, for the life of the daemon.
func TestUnproposableDriftReportBacksOffButStaysInResult(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "rejected")
	initRepo(t, repo, "https://github.com/acme/rejected.git")
	const handEdit = "hand-edited, never proposable\n"
	dest := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(dest, []byte(handEdit), 0o600); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	srv := newFake(t)
	srv.bound["github.com/acme/rejected"] = true
	srv.rejectPath = repo
	srv.setTargets(agentsTarget(t))
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}
	got := &captureMetrics{}
	cfg.Metrics = got

	for i := 1; i <= 3; i++ {
		res, err := Sync(t.Context(), db, cfg)
		if err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		if len(res.UnproposedDrift) != 1 || res.UnproposedDrift[0] != dest {
			t.Fatalf("cycle %d: UnproposedDrift = %v, want [%s]; back-off must throttle the report, never the result", i, res.UnproposedDrift, dest)
		}
		if got := readFile(t, dest); got != handEdit {
			t.Fatalf("cycle %d: un-proposed file was overwritten", i)
		}
	}
	if got.drift != 1 {
		t.Fatalf("RecordRenderDrift called %d times over 3 cycles, want 1; a permanently unproposable file floods metrics", got.drift)
	}
}

// The backoff grows and is capped, and a landed proposal forgets the path so a
// later, unrelated failure on the same file reports at once.
func TestDriftReportBackoffGrowsAndResets(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "adapter.sqlite"))
	const path = "/tmp/AGENTS.md"
	var now int64 = 1000

	if ok, _ := db.shouldReportDrift(path, now); !ok {
		t.Fatal("first occurrence must report")
	}
	if ok, _ := db.shouldReportDrift(path, now); ok {
		t.Fatal("second occurrence in the same cycle must be suppressed")
	}
	now += int64(DriftReportBackoffBase.Seconds())
	ok, suppressed := db.shouldReportDrift(path, now)
	if !ok {
		t.Fatal("report must resume once the backoff elapses")
	}
	if suppressed != 1 {
		t.Fatalf("suppressed = %d, want 1; the operator must be told what was skipped", suppressed)
	}
	if got, want := driftBackoffFor(2), 2*DriftReportBackoffBase; got != want {
		t.Fatalf("backoff after 2 reports = %s, want %s", got, want)
	}
	if got := driftBackoffFor(50); got != MaxDriftReportBackoff {
		t.Fatalf("backoff is uncapped: %s", got)
	}

	db.clearDriftBackoff(path)
	if ok, _ := db.shouldReportDrift(path, now); !ok {
		t.Fatal("a landed proposal must clear the backoff for that path")
	}
}
