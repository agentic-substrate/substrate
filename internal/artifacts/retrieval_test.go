package artifacts

import (
	"errors"
	"github.com/agentic-substrate/substrate/internal/authority"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSearchScopesRankingAssociationsAndDirectReads(t *testing.T) {
	f := setup(t)
	c := memory("personal", "Use the npm build command for the frontend")
	c.Associations = Associations{Identifiers: []string{"UI-42"}, Aliases: []string{"browser compile"}, Topics: []string{"delivery"}}
	saved := capture(t, f.session, c)
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	before, err := f.session.Search(SearchRequest{Query: "browser compile"})
	if err != nil || len(before.Results) != 1 || before.Results[0].RevisionID != saved.RevisionID {
		t.Fatalf("missing explicit alias: %+v %v", before, err)
	}
	if err := f.auth.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(t.TempDir(), "work")
	os.Mkdir(checkout, 0700)
	git(t, checkout, "init", "-q")
	if _, err := f.auth.Register(checkout, "Work"); err != nil {
		t.Fatal(err)
	}
	token, err := f.auth.CreateSession(checkout, "Work")
	if err != nil {
		t.Fatal(err)
	}
	work, err := f.store.Session(token, checkout)
	if err != nil {
		t.Fatal(err)
	}
	hidden := memory("work", "browser compile secret Work material")
	hidden.Associations = Associations{Identifiers: []string{"SECRET-1"}, Aliases: []string{"browser compile"}, Topics: []string{"delivery"}}
	secret := capture(t, work, hidden)
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	after, err := f.session.Search(SearchRequest{Query: "browser compile"})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("Work changed Personal ranking/coverage: before %+v after %+v %v", before, after, err)
	}
	for _, query := range []string{"SECRET-1", secret.ArtifactID, secret.RevisionID, "secret"} {
		got, err := f.session.Search(SearchRequest{Query: query})
		if err != nil || len(got.Results) != 0 {
			t.Fatalf("hidden search %q: %+v %v", query, got, err)
		}
	}
	for _, request := range []ReadRequest{{ArtifactID: secret.ArtifactID}, {RevisionID: secret.RevisionID}, {Selector: "SECRET-1"}} {
		if _, err := f.session.Read(request); !errors.Is(err, authority.ErrDenied) {
			t.Fatalf("hidden read: %v", err)
		}
	}
	bad := memory("bad-edge", "related observation")
	bad.Associations.Related = []string{secret.ArtifactID}
	if _, err := f.session.Contribute(bad); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("hidden edge accepted: %v", err)
	}
}

func TestIndexPauseCurrentReadAndStaleRevisionInvalidation(t *testing.T) {
	f := setup(t)
	first := capture(t, f.session, memory("first", "old zebra evidence"))
	read, err := f.session.Read(ReadRequest{ArtifactID: first.ArtifactID})
	if err != nil || read.Revision.ID != first.RevisionID {
		t.Fatalf("unindexed read %+v %v", read, err)
	}
	pending, _ := f.session.Pending()
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	after, _ := f.session.Pending()
	if !reflect.DeepEqual(pending, after) {
		t.Fatal("index consumed durable operation ledger")
	}
	edit := memory("edit", "new otter evidence")
	edit.ArtifactID = first.ArtifactID
	edit.ExpectedRevision = first.RevisionID
	second := capture(t, f.session, edit)
	old, err := f.session.Search(SearchRequest{Query: "zebra"})
	if err != nil || len(old.Results) != 0 || old.Index.Pending != 1 {
		t.Fatalf("stale index delivered: %+v %v", old, err)
	}
	if _, err := f.session.Read(ReadRequest{RevisionID: first.RevisionID}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("stale revision read %v", err)
	}
	f.store.Close()
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	found, err := f.session.Search(SearchRequest{Query: "otter"})
	if err != nil || len(found.Results) != 1 || found.Results[0].RevisionID != second.RevisionID {
		t.Fatalf("resume %+v %v", found, err)
	}
	if _, err := f.session.Retire(second.ArtifactID, second.RevisionID, "retire"); err != nil {
		t.Fatal(err)
	}
	found, err = f.session.Search(SearchRequest{Query: "otter"})
	if err != nil || len(found.Results) != 0 {
		t.Fatalf("retired index %+v %v", found, err)
	}
}

func TestSearchHonorsSourceApprovalAndKeepsSearchAliasesSeparate(t *testing.T) {
	f := setup(t)
	candidate := gitCandidate(t, f, "candidate", "", "", "approved otter skill")
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	got, err := f.session.Search(SearchRequest{Query: "otter"})
	if err != nil || len(got.Results) != 0 {
		t.Fatalf("candidate searched %+v %v", got, err)
	}
	owner, choice := registered(t, f, candidate, "repo", "otter", false)
	approve(t, owner, candidate, "")
	c := memory("alias-memory", "otter note")
	c.Associations.Aliases = []string{"otter"}
	capture(t, f.session, c)
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	delivery, err := f.session.Read(ReadRequest{Selector: "otter"})
	if err != nil || delivery.Revision.ID != candidate.RevisionID {
		t.Fatalf("memory search alias changed approved resolution %+v %v", delivery, err)
	}
	got, err = f.session.Search(SearchRequest{Query: "otter"})
	if err != nil || len(got.Results) != 2 {
		t.Fatalf("approved search %+v %v", got, err)
	}
	if _, err := f.session.Retire(choice.ArtifactID, candidate.RevisionID, "retire-skill"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.session.Read(ReadRequest{ArtifactID: choice.ArtifactID}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("retired direct read %v", err)
	}
	got, err = f.session.Search(SearchRequest{Query: "otter"})
	if err != nil || len(got.Results) != 1 || got.Results[0].Kind != "memory" {
		t.Fatalf("retired indexed skill %+v %v", got, err)
	}
}

func TestIndexFailurePreservesQueueReceiptAndResumes(t *testing.T) {
	f := setup(t)
	saved := capture(t, f.session, memory("fail-index", "bounded lexical observation"))
	if _, err := f.store.db.Exec("CREATE TRIGGER fail_index BEFORE INSERT ON indexed BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.IndexBatch(100); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("failed index %v", err)
	}
	var pending, tokens int
	f.store.db.QueryRow("SELECT count(*) FROM index_queue").Scan(&pending)
	f.store.db.QueryRow("SELECT count(*) FROM tokens").Scan(&tokens)
	if pending != 1 || tokens != 0 {
		t.Fatalf("partial index committed pending=%d tokens=%d", pending, tokens)
	}
	if retry := capture(t, f.session, memory("fail-index", "bounded lexical observation")); retry != saved {
		t.Fatal("index failure lost receipt")
	}
	if _, err := f.session.Read(ReadRequest{ArtifactID: saved.ArtifactID}); err != nil {
		t.Fatal(err)
	}
	f.store.db.Exec("DROP TRIGGER fail_index")
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	got, err := f.session.Search(SearchRequest{Query: "lex*"})
	if err != nil || len(got.Results) != 1 {
		t.Fatalf("resumed prefix %+v %v", got, err)
	}
}

func TestVersionTwoMigrationPreservesLegacyRetryFingerprints(t *testing.T) {
	f := setup(t)
	c := memory("legacy", "legacy durable observation")
	saved := capture(t, f.session, c)
	// This shape is the original version 2 contribution wire contract.
	type legacy struct {
		OperationID      string  `json:"operation_id"`
		ArtifactID       string  `json:"artifact_id,omitempty"`
		ExpectedRevision string  `json:"expected_revision,omitempty"`
		SpaceID          string  `json:"space_id,omitempty"`
		RepositoryID     string  `json:"repo_id,omitempty"`
		Kind             string  `json:"kind"`
		Content          string  `json:"content"`
		Provenance       string  `json:"provenance"`
		Source           *Source `json:"source,omitempty"`
	}
	ctx, err := f.session.authenticate()
	if err != nil {
		t.Fatal(err)
	}
	old := legacy{OperationID: c.OperationID, SpaceID: ctx.SpaceID, RepositoryID: ctx.RepositoryID, Kind: c.Kind, Content: c.Content, Provenance: c.Provenance}
	if _, err := f.store.db.Exec("UPDATE contributions SET fingerprint=? WHERE operation_id=?", fingerprint(old), c.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec("DROP TABLE review_grants; DROP TABLE publication_revisions; DROP TABLE publication_proposals; DROP TABLE publication_policy; DROP TABLE tokens; DROP TABLE indexed; DROP TABLE index_queue; DROP TABLE revision_associations; PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	f.store.Close()
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	if retry := capture(t, f.session, c); retry != saved {
		t.Fatal("upgrade changed legacy receipt")
	}
	c.Associations.Aliases = []string{"new alias"}
	if _, err := f.session.Contribute(c); !errors.Is(err, ErrOperation) {
		t.Fatalf("changed association reused legacy receipt %v", err)
	}
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	got, err := f.session.Search(SearchRequest{Query: "legacy"})
	if err != nil || len(got.Results) != 1 || got.Results[0].RevisionID != saved.RevisionID {
		t.Fatalf("upgrade index %+v %v", got, err)
	}
}

func TestRelatedResultsCheckBothEndpointsAndCurrentEligibility(t *testing.T) {
	f := setup(t)
	target := capture(t, f.session, memory("target", "target evidence"))
	source := memory("source", "source evidence")
	source.Associations.Related = []string{target.ArtifactID}
	origin := capture(t, f.session, source)
	got, err := f.session.Search(SearchRequest{RelatedTo: origin.ArtifactID})
	if err != nil || len(got.Results) != 1 || got.Results[0].ArtifactID != target.ArtifactID {
		t.Fatalf("one-hop relationship %+v %v", got, err)
	}
	if _, err := f.session.Retire(target.ArtifactID, target.RevisionID, "remove-target"); err != nil {
		t.Fatal(err)
	}
	got, err = f.session.Search(SearchRequest{RelatedTo: origin.ArtifactID})
	if err != nil || len(got.Results) != 0 {
		t.Fatalf("retired endpoint %+v %v", got, err)
	}
	if _, err := f.session.Search(SearchRequest{RelatedTo: "unknown"}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("missing relationship origin %v", err)
	}
}

func TestIndexCapsPostingsAndReportsLimitedCoverage(t *testing.T) {
	f := setup(t)
	content := strings.Repeat("bounded ", MaxIndexedTokens+10) + "trailingunique"
	saved := capture(t, f.session, memory("bounded-index", content))
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	got, err := f.session.Search(SearchRequest{Query: "bounded"})
	if err != nil || len(got.Results) != 1 || got.Index.Limited != 1 {
		t.Fatalf("bounded index %+v %v", got, err)
	}
	var count int
	f.store.db.QueryRow("SELECT count(*) FROM tokens").Scan(&count)
	if count != MaxIndexedTokens {
		t.Fatalf("unbounded postings %d", count)
	}
	read, err := f.session.Read(ReadRequest{ArtifactID: saved.ArtifactID})
	if err != nil || read.Revision.Content != content {
		t.Fatal("index cap truncated durable content")
	}
}

func TestInspectPreservesRevisionAssociationsAndRetryIdentity(t *testing.T) {
	f := setup(t)
	c := memory("association-save", "observation")
	c.Associations = Associations{Identifiers: []string{"ISSUE-8"}, Aliases: []string{"explicit alias"}, Topics: []string{"retrieval"}}
	saved := capture(t, f.session, c)
	history, err := f.session.Inspect(saved.ArtifactID)
	if err != nil || len(history.Revisions) != 1 || !reflect.DeepEqual(history.Revisions[0].Associations, c.Associations) {
		t.Fatalf("revision lost associations %+v %v", history, err)
	}
	c.Associations.Aliases = []string{"changed alias"}
	if _, err := f.session.Contribute(c); !errors.Is(err, ErrOperation) {
		t.Fatalf("changed metadata accepted %v", err)
	}
}

func TestPrefixMinimumCountsUnicodeCharacters(t *testing.T) {
	f := setup(t)
	saved := capture(t, f.session, memory("unicode-prefix", "猫咪喜欢 quiet observations"))
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"猫*", "猫咪*"} {
		got, err := f.session.Search(SearchRequest{Query: query})
		if err != nil || len(got.Results) != 0 {
			t.Fatalf("short Unicode prefix expanded %q: %+v %v", query, got, err)
		}
	}
	got, err := f.session.Search(SearchRequest{Query: "猫咪喜*"})
	if err != nil || len(got.Results) != 1 || got.Results[0].RevisionID != saved.RevisionID {
		t.Fatalf("three-character prefix failed %+v %v", got, err)
	}
}
