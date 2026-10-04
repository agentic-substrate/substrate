package artifacts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-substrate/substrate/internal/authority"
)

type fixture struct {
	auth            *authority.Store
	store           *Store
	session         *Session
	token, checkout string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	f := &fixture{auth: &authority.Store{Dir: filepath.Join(home, "private?mode=ro#state")}, checkout: filepath.Join(home, "repo")}
	if err := os.Mkdir(f.checkout, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "init", "-q")
	if err := f.auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Register(f.checkout, "Personal"); err != nil {
		t.Fatal(err)
	}
	var err error
	f.token, err = f.auth.CreateSession(f.checkout, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatalf("open artifact storage: %v", err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	return f
}
func git(t *testing.T, checkout string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", checkout}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func capture(t *testing.T, s *Session, c Contribution) Receipt {
	t.Helper()
	r, err := s.Contribute(c)
	if err != nil {
		t.Fatal(err)
	}
	if r.ArtifactID == "" || r.RevisionID == "" {
		t.Fatalf("incomplete receipt: %+v", r)
	}
	return r
}
func memory(operation, content string) Contribution {
	return Contribution{OperationID: operation, Kind: "memory", Content: content, Provenance: "synthetic observation from a harness session"}
}

func TestAcknowledgedObservationSurvivesReopen(t *testing.T) {
	f := setup(t)
	c := memory("capture-1", "Use the documented build command.")
	r := capture(t, f.session, c)
	if r.State != "pending-local" {
		t.Fatalf("unexpected save state: %+v", r)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.session.Inspect(r.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if a.HeadRevision != r.RevisionID || a.Lifecycle != "active" || len(a.Revisions) != 1 || a.Revisions[0].Content != c.Content || a.Revisions[0].Verification != "unverified" || a.Revisions[0].Provenance != c.Provenance {
		t.Fatalf("lost observation state: %+v", a)
	}
	if retry := capture(t, f.session, c); retry != r {
		t.Fatalf("retry changed receipt: %+v %+v", r, retry)
	}
	pending, err := f.session.Pending()
	if err != nil || len(pending) != 1 || pending[0].RevisionID != r.RevisionID {
		t.Fatalf("pending work: %+v %v", pending, err)
	}
}

func TestFailureRollsBackObservationReceiptAndPendingWork(t *testing.T) {
	f := setup(t)
	_, err := f.store.db.Exec(`CREATE TRIGGER fail_receipt BEFORE INSERT ON contributions BEGIN SELECT RAISE(ABORT, 'injected storage failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.session.Contribute(memory("fail-1", "Must not be reported saved."))
	if !errors.Is(err, ErrUnavailable) || r != (Receipt{}) {
		t.Fatalf("failure acknowledged: %+v %v", r, err)
	}
	for _, table := range []string{"artifacts", "revisions", "contributions", "pending"} {
		var count int
		if err := f.store.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial %s persisted: %d %v", table, count, err)
		}
	}
	if _, err := f.store.db.Exec("DROP TRIGGER fail_receipt"); err != nil {
		t.Fatal(err)
	}
	capture(t, f.session, memory("fail-1", "Must not be reported saved."))
}

func TestStaleCandidatesRetryAndRetirement(t *testing.T) {
	f := setup(t)
	first := capture(t, f.session, memory("first", "first"))
	current := memory("current", "second")
	current.ArtifactID, current.ExpectedRevision = first.ArtifactID, first.RevisionID
	second := capture(t, f.session, current)
	stale := memory("stale", "competing")
	stale.ArtifactID, stale.ExpectedRevision = first.ArtifactID, first.RevisionID
	conflict := capture(t, f.session, stale)
	if conflict.State != "conflict" {
		t.Fatalf("stale edit replaced head: %+v", conflict)
	}
	if retry := capture(t, f.session, stale); retry != conflict {
		t.Fatal("conflict retry changed identity")
	}
	stale.Content = "changed payload"
	if _, err := f.session.Contribute(stale); !errors.Is(err, ErrOperation) {
		t.Fatalf("changed retry allowed: %v", err)
	}
	a, err := f.session.Inspect(first.ArtifactID)
	if err != nil || a.HeadRevision != second.RevisionID || len(a.Revisions) != 3 {
		t.Fatalf("competing revisions lost: %+v %v", a, err)
	}
	if _, err := f.session.Retire(first.ArtifactID, first.RevisionID, "stale-retirement"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale retirement: %v", err)
	}
	retired, err := f.session.Retire(first.ArtifactID, second.RevisionID, "retire")
	if err != nil || retired.State != "retired" {
		t.Fatalf("retire: %+v %v", retired, err)
	}
	if retry, err := f.session.Retire(first.ArtifactID, second.RevisionID, "retire"); err != nil || retry != retired {
		t.Fatalf("retirement retry: %+v %v", retry, err)
	}
	after := memory("after-retirement", "old offline edit")
	after.ArtifactID, after.ExpectedRevision = first.ArtifactID, second.RevisionID
	if r := capture(t, f.session, after); r.State != "conflict-retired" {
		t.Fatalf("retired edit: %+v", r)
	}
	a, err = f.session.Inspect(first.ArtifactID)
	if err != nil || a.Lifecycle != "retired" || a.HeadRevision != second.RevisionID || len(a.Revisions) != 4 {
		t.Fatalf("retirement undone: %+v %v", a, err)
	}
}

func TestScopeFilteringAndCurrentSessionAuthority(t *testing.T) {
	f := setup(t)
	r := capture(t, f.session, memory("private", "personal content"))
	sibling := filepath.Join(filepath.Dir(f.checkout), "another-personal-repo")
	if err := os.Mkdir(sibling, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, sibling, "init", "-q")
	if _, err := f.auth.Register(sibling, "Personal"); err != nil {
		t.Fatal(err)
	}
	siblingToken, err := f.auth.CreateSession(sibling, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	siblingSession, err := f.store.Session(siblingToken, sibling)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := siblingSession.Inspect(r.ArtifactID); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("same-space repository leaked: %v", err)
	}
	if pending, err := siblingSession.Pending(); err != nil || len(pending) != 0 {
		t.Fatalf("same-space pending leaked: %+v %v", pending, err)
	}
	other := capture(t, siblingSession, memory("private", "personal content"))
	if other.ArtifactID == r.ArtifactID {
		t.Fatal("operation receipt crossed repository boundary")
	}
	work := filepath.Join(filepath.Dir(f.checkout), "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, work, "init", "-q")
	if err := f.auth.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Register(work, "Work"); err != nil {
		t.Fatal(err)
	}
	token, err := f.auth.CreateSession(work, "Work")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := f.store.Session(token, work)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{r.ArtifactID, "missing"} {
		if a, err := ws.Inspect(id); !errors.Is(err, authority.ErrDenied) || a.ID != "" {
			t.Fatalf("object leaked: %+v %v", a, err)
		}
	}
	if pending, err := ws.Pending(); err != nil || len(pending) != 0 {
		t.Fatalf("pending leaked: %+v %v", pending, err)
	}
	ctx, err := f.auth.Authenticate(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	c := memory("cross-space", "wrong destination")
	c.SpaceID, c.RepositoryID = ctx.SpaceID, ctx.RepositoryID
	if _, err := ws.Contribute(c); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("destination widened: %v", err)
	}
	c = memory("object-edit", "wrong object")
	c.ArtifactID, c.ExpectedRevision = r.ArtifactID, r.RevisionID
	if _, err := ws.Contribute(c); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("object mutation widened: %v", err)
	}
	if _, err := ws.Retire(r.ArtifactID, r.RevisionID, "wrong-retirement"); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("retirement widened: %v", err)
	}
	if err := f.auth.RevokeSession(f.token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.session.Contribute(memory("revoked", "no access")); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("stale credential wrote: %v", err)
	}
	if _, err := f.session.Inspect(r.ArtifactID); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("stale credential read: %v", err)
	}
	if _, err := f.session.Pending(); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("stale credential disclosed queue: %v", err)
	}
}

func TestGitSourceCandidatesRetainImmutableBytes(t *testing.T) {
	f := setup(t)
	content := "# Synthetic source\n\nExact committed bytes.\n"
	if err := os.WriteFile(filepath.Join(f.checkout, "source.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("source.md", filepath.Join(f.checkout, "link.md")); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", "source.md", "link.md")
	git(t, f.checkout, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "test fixture")
	commit := git(t, f.checkout, "rev-parse", "HEAD")
	blob := git(t, f.checkout, "rev-parse", "HEAD:source.md")
	for _, kind := range []string{"skill", "agent-definition"} {
		c := Contribution{OperationID: kind, Kind: kind, Provenance: "source import", Source: &Source{Commit: commit, Path: "source.md"}}
		r := capture(t, f.session, c)
		a, err := f.session.Inspect(r.ArtifactID)
		if err != nil || r.State != "candidate" || a.HeadRevision != "" || len(a.Revisions) != 1 || a.Revisions[0].Content != content || a.Revisions[0].Source.Blob != blob || a.Revisions[0].Source.Commit != commit {
			t.Fatalf("source activated or lost: %+v %v", a, err)
		}
	}
	for _, src := range []Source{{Commit: "HEAD", Path: "source.md"}, {Commit: commit, Path: "../source.md"}, {Commit: commit, Path: "link.md"}} {
		if _, err := f.session.Contribute(Contribution{OperationID: "unsafe-source", Kind: "skill", Source: &src}); err == nil {
			t.Fatalf("unsafe source accepted: %+v", src)
		}
	}
}

func TestConcurrentIdenticalRetriesProduceOneRevision(t *testing.T) {
	f := setup(t)
	other, err := Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	s, err := other.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]Receipt, 8)
	errs := make([]error, len(results))
	for i := range results {
		wg.Go(func() {
			session := f.session
			if i%2 != 0 {
				session = s
			}
			results[i], errs[i] = session.Contribute(memory("concurrent", "one observation"))
		})
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil || !reflect.DeepEqual(results[i], results[0]) {
			t.Fatalf("retry %d: %+v %v", i, results[i], errs[i])
		}
	}
	a, err := f.session.Inspect(results[0].ArtifactID)
	if err != nil || len(a.Revisions) != 1 {
		t.Fatalf("duplicates: %+v %v", a, err)
	}
}

func TestCommitFailureDoesNotAcknowledge(t *testing.T) {
	f := setup(t)
	_, err := f.store.db.Exec(`CREATE TABLE commit_failure (artifact_id TEXT REFERENCES artifacts(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_commit AFTER INSERT ON contributions BEGIN INSERT INTO commit_failure VALUES('missing'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.session.Contribute(memory("commit-failure", "rollback the entire transaction"))
	if !errors.Is(err, ErrUnavailable) || r != (Receipt{}) {
		t.Fatalf("failed commit acknowledged: %+v %v", r, err)
	}
	var count int
	if err := f.store.db.QueryRow("SELECT count(*) FROM artifacts").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed commit retained artifact: %d %v", count, err)
	}
	if _, err := f.store.db.Exec("DROP TRIGGER fail_commit"); err != nil {
		t.Fatal(err)
	}
	capture(t, f.session, memory("commit-failure", "rollback the entire transaction"))
}

func TestPrivateStorageAndUnknownSchemaFailClosed(t *testing.T) {
	for _, scenario := range []string{"permissions", "symlink", "schema", "journal"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			path := filepath.Join(f.auth.Dir, "artifacts.db")
			switch scenario {
			case "permissions":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			case "schema":
				if _, err := f.store.db.Exec("PRAGMA user_version=99"); err != nil {
					t.Fatal(err)
				}
			case "journal":
				if err := os.Symlink(path, path+"-journal"); err != nil {
					t.Fatal(err)
				}
			}
			if store, err := Open(f.auth); !errors.Is(err, ErrUnavailable) || store != nil {
				if store != nil {
					store.Close()
				}
				t.Fatalf("unsafe storage opened: %v", err)
			}
		})
	}
}

func TestDriverProvidesFTS5AndDurableSettings(t *testing.T) {
	f := setup(t)
	if _, err := f.store.db.Exec(`CREATE VIRTUAL TABLE temp.probe USING fts5(content); INSERT INTO probe VALUES('durable observation');`); err != nil {
		t.Fatalf("selected driver lacks FTS5: %v", err)
	}
	var count, synchronous, foreignKeys int
	var journal, version string
	if err := f.store.db.QueryRow("SELECT count(*) FROM probe WHERE probe MATCH 'observation'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("FTS5 lookup: %d %v", count, err)
	}
	if err := f.store.db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil || synchronous != 3 {
		t.Fatalf("synchronous=%d: %v", synchronous, err)
	}
	if err := f.store.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign_keys=%d: %v", foreignKeys, err)
	}
	if err := f.store.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "delete" {
		t.Fatalf("journal=%s: %v", journal, err)
	}
	if err := f.store.db.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("selected driver SQLite %s, FTS5 verified", version)
}

func TestGitReplacementDoesNotAlterCapturedSourceIdentity(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.checkout, "source.md")
	if err := os.WriteFile(path, []byte("original bytes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", "source.md")
	git(t, f.checkout, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "original")
	original := git(t, f.checkout, "rev-parse", "HEAD")
	if err := os.WriteFile(path, []byte("replacement bytes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", "source.md")
	git(t, f.checkout, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "replacement")
	git(t, f.checkout, "replace", original, "HEAD")
	r := capture(t, f.session, Contribution{OperationID: "replacement", Kind: "skill", Source: &Source{Commit: original, Path: "source.md"}})
	a, err := f.session.Inspect(r.ArtifactID)
	if err != nil || a.Revisions[0].Content != "original bytes\n" {
		t.Fatalf("replacement changed immutable source: %+v %v", a, err)
	}
	git(t, f.checkout, "replace", "-d", original)
	originalBlob := git(t, f.checkout, "rev-parse", original+":source.md")
	replacementBlob := git(t, f.checkout, "rev-parse", "HEAD:source.md")
	git(t, f.checkout, "replace", originalBlob, replacementBlob)
	r = capture(t, f.session, Contribution{OperationID: "blob-replacement", Kind: "skill", Source: &Source{Commit: original, Path: "source.md"}})
	a, err = f.session.Inspect(r.ArtifactID)
	if err != nil || a.Revisions[0].Content != "original bytes\n" || a.Revisions[0].Source.Blob != originalBlob {
		t.Fatalf("blob replacement changed immutable source: %+v %v", a, err)
	}
}
