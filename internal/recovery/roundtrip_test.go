package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
	"github.com/ncruces/go-sqlite3/driver"
)

func databaseState(t *testing.T, directory string) map[string]string {
	t.Helper()
	uri := (&url.URL{Scheme: "file", Path: filepath.Join(directory, "artifacts.db"), RawQuery: "mode=ro&immutable=1"}).String()
	db, err := driver.Open(uri)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state := map[string]string{}
	for _, table := range []string{"artifacts", "revisions", "contributions", "pending", "registrations", "approvals", "revision_associations", "index_queue", "indexed", "tokens", "publication_policy", "publication_proposals", "publication_revisions", "maintenance"} {
		rows, err := db.Query("SELECT rowid,* FROM " + table + " ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		values := [][]any{}
		for rows.Next() {
			row := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range row {
				pointers[i] = &row[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			values = append(values, row)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		state[table] = string(encoded)
	}
	return state
}

func TestCompleteRecoveryRoundTrip(t *testing.T) {
	f := setup(t)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	git := func(path string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", path}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	for name, content := range map[string]string{"skill.md": "bundleneedle skill", "reference.md": "bundleneedle reference", "agent.toml": "definitionneedle agent"} {
		check(os.WriteFile(filepath.Join(f.repo, name), []byte(content), 0600))
	}
	git(f.repo, "add", ".")
	git(f.repo, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "synthetic source")
	commit := git(f.repo, "rev-parse", "HEAD")
	store, err := artifacts.Open(f.auth)
	check(err)
	session, err := store.Session(f.token, f.repo)
	check(err)
	owner, err := store.Owner(f.repo)
	check(err)
	capture := func(s *artifacts.Session, c artifacts.Contribution) artifacts.Receipt {
		t.Helper()
		r, err := s.Contribute(c)
		check(err)
		return r
	}
	skill := capture(session, artifacts.Contribution{OperationID: "skill", Kind: "skill", Source: &artifacts.Source{Commit: commit, Path: "skill.md", Files: []artifacts.SourceFile{{Path: "reference.md"}}}})
	definition := capture(session, artifacts.Contribution{OperationID: "definition", Kind: "agent-definition", Source: &artifacts.Source{Commit: commit, Path: "agent.toml"}})
	for i, r := range []artifacts.Receipt{skill, definition} {
		name := []string{"bundle-skill", "agent"}[i]
		_, err := owner.Register(artifacts.Registration{ArtifactID: r.ArtifactID, Source: "repository", Name: name, Alias: name, Overridable: true})
		check(err)
		_, err = owner.Approve(artifacts.Approval{OperationID: "approve-" + name, ArtifactID: r.ArtifactID, RevisionID: r.RevisionID})
		check(err)
	}
	retired := capture(session, artifacts.Contribution{OperationID: "retired", Kind: "memory", Content: "retired observation"})
	_, err = session.Retire(retired.ArtifactID, retired.RevisionID, "retire")
	check(err)
	conflict := capture(session, artifacts.Contribution{OperationID: "conflict", Kind: "memory", ArtifactID: f.receipt.ArtifactID, ExpectedRevision: "unknown stale base", Content: "preserved conflict", Associations: artifacts.Associations{Topics: []string{"historical"}, Related: []string{retired.ArtifactID}}})
	if conflict.State != "conflict" {
		t.Fatal(conflict)
	}
	workRepo := filepath.Join(f.root, "work")
	check(os.Mkdir(workRepo, 0700))
	git(workRepo, "init", "-q")
	check(f.auth.CreateSpace("Work"))
	_, err = f.auth.Register(workRepo, "Work")
	check(err)
	workToken, err := f.auth.CreateSession(workRepo, "Work")
	check(err)
	work, err := store.Session(workToken, workRepo)
	check(err)
	workOwner, err := store.Owner(workRepo)
	check(err)
	source := capture(work, artifacts.Contribution{OperationID: "work-source", Kind: "memory", Content: "private Work source"})
	personalContext, err := f.auth.Authenticate(f.token, f.repo)
	check(err)
	check(workOwner.SetPublicationPolicy("export", "", true))
	check(owner.SetPublicationPolicy("publish", "", true))
	input := artifacts.PublicationInput{OperationID: "proposal", Content: "reviewed lesson", Sources: []artifacts.PublicationSource{{ArtifactID: source.ArtifactID, RevisionID: source.RevisionID}}, Destination: artifacts.PublicationDestination{SpaceID: personalContext.SpaceID, RepositoryID: personalContext.RepositoryID}}
	proposal, err := work.ProposePublication(input)
	check(err)
	consumed, err := workOwner.IssuePublicationReview(proposal.ID, proposal.RevisionID, f.repo)
	check(err)
	review, err := store.ReviewPublication(consumed)
	check(err)
	approval := artifacts.PublicationApproval{OperationID: "publish", RevisionID: proposal.RevisionID, Snapshot: review.Snapshot}
	_, err = store.Publish(consumed, approval)
	check(err)
	input.OperationID, input.Content = "draft", "unapproved lesson"
	draft, err := work.ProposePublication(input)
	check(err)
	active, err := workOwner.IssuePublicationReview(draft.ID, draft.RevisionID, f.repo)
	check(err)
	_, err = store.IndexBatch(100)
	check(err)
	failed := capture(session, artifacts.Contribution{OperationID: "failed", Kind: "memory", Content: "failure retained"})
	uri := (&url.URL{Scheme: "file", Path: filepath.Join(f.auth.Dir, "artifacts.db"), RawQuery: "mode=rw"}).String()
	db, err := driver.Open(uri)
	check(err)
	_, err = db.Exec("CREATE TRIGGER fail_index BEFORE INSERT ON indexed BEGIN SELECT RAISE(FAIL,'synthetic failure'); END")
	check(err)
	if attempted, err := store.IndexNext(context.Background(), false); !attempted || !errors.Is(err, artifacts.ErrUnavailable) {
		t.Fatalf("failure fixture did not persist: %v %v", attempted, err)
	}
	_, err = db.Exec("DROP TRIGGER fail_index")
	check(err)
	check(db.Close())
	check(store.SetIndexPaused(true))
	check(store.RebuildIndex())
	pending := capture(session, artifacts.Contribution{OperationID: "pending", Kind: "memory", Content: "resumeneedle durable pending capture"})
	maintenance, err := store.Maintenance()
	check(err)
	if !maintenance.Paused || maintenance.Failed != 1 || maintenance.Deferred < 1 || maintenance.Queued != 1 {
		t.Fatal(maintenance)
	}
	bindings, err := f.auth.Inventory()
	check(err)
	check(store.Close())
	before := databaseState(t, f.auth.Dir)
	check(Backup(f.auth.Dir, f.backup))
	store, err = artifacts.Open(f.auth)
	check(err)
	session, err = store.Session(f.token, f.repo)
	check(err)
	capture(session, artifacts.Contribution{OperationID: "after-snapshot", Kind: "memory", Content: "excluded post snapshot"})
	check(store.Close())
	destination := filepath.Join(f.root, "restored")
	check(Restore(f.backup, destination))
	if after := databaseState(t, destination); !reflect.DeepEqual(before, after) {
		for table := range before {
			if before[table] != after[table] {
				t.Errorf("restored table changed: %s", table)
			}
		}
		t.FailNow()
	}
	restored := &authority.Store{Dir: destination}
	afterBindings, err := restored.Inventory()
	check(err)
	if !reflect.DeepEqual(bindings, afterBindings) {
		t.Fatal("bindings changed")
	}
	for token, checkout := range map[string]string{f.token: f.repo, workToken: workRepo} {
		if _, err := restored.Authenticate(token, checkout); !errors.Is(err, authority.ErrDenied) {
			t.Fatalf("old session accepted: %v", err)
		}
	}
	store, err = artifacts.Open(restored)
	check(err)
	if _, err := store.Publish(consumed, approval); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("old consumed review retry accepted: %v", err)
	}
	if _, err := store.ReviewPublication(active); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("old active review accepted: %v", err)
	}
	token, err := restored.CreateSession(f.repo, "Personal")
	check(err)
	session, err = store.Session(token, f.repo)
	check(err)
	for _, selector := range []string{"bundle-skill", "agent"} {
		delivery, err := session.Resolve(selector)
		check(err)
		if delivery.Revision.Source == nil || selector == "bundle-skill" && len(delivery.Revision.Source.Files) != 1 {
			t.Fatal("source bundle lost")
		}
	}
	if _, err := session.Read(artifacts.ReadRequest{ArtifactID: retired.ArtifactID}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("retired content revived: %v", err)
	}
	check(store.Close())
	runtime, err := node.Start(restored, nil)
	check(err)
	defer runtime.Close()
	call := func(req node.Request) node.Response { return node.Call(context.Background(), destination, req) }
	var status artifacts.Maintenance
	response := call(node.Request{Action: "index", Control: "status"})
	check(json.Unmarshal(response.Result, &status))
	if status != maintenance {
		t.Fatalf("restart changed maintenance: %+v %+v", maintenance, status)
	}
	for _, req := range []node.Request{{Action: "index", Pause: new(false)}, {Action: "index", Control: "retry"}, {Action: "index", Control: "run"}} {
		if response := call(req); response.Error != "" {
			t.Fatal(response)
		}
	}
	response = call(node.Request{Action: "search", Token: token, Checkout: f.repo, Search: artifacts.SearchRequest{Query: "resumeneedle"}})
	var search artifacts.SearchResponse
	check(json.Unmarshal(response.Result, &search))
	if len(search.Results) != 1 || search.Results[0].RevisionID != pending.RevisionID {
		t.Fatalf("restored pending capture did not resume: %+v", search)
	}
	response = call(node.Request{Action: "read", Token: token, Checkout: f.repo, Read: artifacts.ReadRequest{ArtifactID: failed.ArtifactID}})
	if response.Error != "" {
		t.Fatal(response)
	}
}
