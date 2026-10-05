package artifacts

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func TestGitConflictResolutionRequiresNewExplicitApproval(t *testing.T) {
	f := setup(t)
	first := gitCandidate(t, f, "first", "", "", "approved original")
	owner, _ := registered(t, f, first, "source", "build", false)
	competing := gitCandidate(t, f, "competing", first.ArtifactID, "", "unreviewed competing")
	approve(t, owner, first, "")
	request := Approval{OperationID: "resolve", ArtifactID: first.ArtifactID, RevisionID: competing.RevisionID, ExpectedRevision: first.RevisionID}
	if _, err := owner.Approve(request); !errors.Is(err, ErrConflict) {
		t.Fatalf("ordinary approval accepted stale candidate: %v", err)
	}
	if _, err := f.session.ResolveConflict(Resolution{OperationID: "agent-resolve", ArtifactID: first.ArtifactID, RevisionID: competing.RevisionID, ExpectedRevision: first.RevisionID}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("session resolved source approval: %v", err)
	}
	if delivered, err := f.session.Resolve("build"); err != nil || delivered.Revision.Content != "approved original" {
		t.Fatalf("candidate activated before owner review: %+v %v", delivered, err)
	}
	request.ResolveConflict = true
	resolved, err := owner.Approve(request)
	if err != nil || resolved.State != "approved" || resolved.RevisionID == competing.RevisionID {
		t.Fatalf("explicit conflict review did not create reviewed resolution: %+v %v", resolved, err)
	}
	if retry, err := owner.Approve(request); err != nil || retry != resolved {
		t.Fatalf("reviewed resolution retry: %+v %v", retry, err)
	}
	stale := request
	stale.OperationID = "stale-source-resolution"
	if _, err := owner.Approve(stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale owner resolution replaced accepted source: %v", err)
	}
	if delivered, err := f.session.Resolve("build"); err != nil || delivered.Revision.Content != "unreviewed competing" || delivered.Revision.ID != resolved.RevisionID {
		t.Fatalf("reviewed resolution unavailable: %+v %v", delivered, err)
	}
	if _, err := f.session.Retire(first.ArtifactID, resolved.RevisionID, "retire-source"); err != nil {
		t.Fatal(err)
	}
	restored, err := owner.Restore(first.ArtifactID, resolved.RevisionID, "restore-source")
	if err != nil || restored.State != "candidate" || restored.RevisionID == resolved.RevisionID {
		t.Fatalf("source restoration: %+v %v", restored, err)
	}
	if _, err := f.session.Resolve("build"); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("restoration reused previous approval: %v", err)
	}
	artifact, err := f.session.Inspect(first.ArtifactID)
	if err != nil || artifact.HeadRevision != "" || artifact.Lifecycle != "active" {
		t.Fatalf("restored source not pending approval: %+v %v", artifact, err)
	}
	approve(t, owner, restored, "")
	if delivered, err := f.session.Resolve("build"); err != nil || delivered.Revision.ID != restored.RevisionID {
		t.Fatalf("fresh restoration approval unavailable: %+v %v", delivered, err)
	}
}

func TestSourceRestoreInvalidatesPreRetirementCandidates(t *testing.T) {
	for _, approved := range []bool{false, true} {
		t.Run(map[bool]string{false: "unapproved", true: "approved"}[approved], func(t *testing.T) {
			f := setup(t)
			first := gitCandidate(t, f, "first", "", "", "old source")
			owner, _ := registered(t, f, first, "source", "build", false)
			old := gitCandidate(t, f, "competing", first.ArtifactID, "", "ancient candidate")
			expected := ""
			if approved {
				approve(t, owner, first, "")
				expected = first.RevisionID
			}
			if _, err := f.session.Retire(first.ArtifactID, expected, "retire"); err != nil {
				t.Fatal(err)
			}
			restored, err := owner.Restore(first.ArtifactID, expected, "restore")
			if err != nil {
				t.Fatal(err)
			}
			if approved && (restored.RevisionID == "" || restored.RevisionID == old.RevisionID || restored.RevisionID == first.RevisionID || restored.State != "candidate") {
				t.Fatalf("approved restoration did not create fresh candidate: %+v", restored)
			}
			if !approved && (restored.RevisionID != "" || restored.State != "restored-awaiting-candidate") {
				t.Fatalf("unapproved restoration chose content implicitly: %+v", restored)
			}
			if _, err := owner.Approve(Approval{OperationID: "old-approval", ArtifactID: first.ArtifactID, RevisionID: old.RevisionID}); !errors.Is(err, ErrConflict) {
				t.Fatalf("old approval accepted after restoration: %v", err)
			}
			if _, err := owner.Approve(Approval{OperationID: "old-resolution", ArtifactID: first.ArtifactID, RevisionID: old.RevisionID, ResolveConflict: true}); !errors.Is(err, ErrConflict) {
				t.Fatalf("old candidate resolved after restoration: %v", err)
			}
			if !approved {
				restored = gitCandidate(t, f, "fresh", first.ArtifactID, "", "freshly proposed source")
			}
			approve(t, owner, restored, "")
		})
	}
}

func competingMemory(t *testing.T, f *fixture) (Receipt, Receipt, Receipt) {
	t.Helper()
	first := capture(t, f.session, memory("first", "original"))
	current := memory("current", "accepted")
	current.ArtifactID, current.ExpectedRevision = first.ArtifactID, first.RevisionID
	second := capture(t, f.session, current)
	stale := memory("stale", "competing")
	stale.ArtifactID, stale.ExpectedRevision = first.ArtifactID, first.RevisionID
	stale.Associations = Associations{Identifiers: []string{"candidate-identifier"}}
	return first, second, capture(t, f.session, stale)
}

func TestResolutionChecksCurrentHeadAndDurablyCopiesUnverifiedEvidence(t *testing.T) {
	f := setup(t)
	first, current, candidate := competingMemory(t, f)
	a := Resolution{OperationID: "resolve", ArtifactID: first.ArtifactID, RevisionID: candidate.RevisionID, ExpectedRevision: first.RevisionID}
	if r, err := f.session.ResolveConflict(a); !errors.Is(err, ErrConflict) || r != (Receipt{}) {
		t.Fatalf("stale resolution acknowledged: %+v %v", r, err)
	}
	a.ExpectedRevision = current.RevisionID
	r, err := f.session.ResolveConflict(a)
	if err != nil || r.State != "pending-local" || r.RevisionID == candidate.RevisionID {
		t.Fatalf("explicit resolution: %+v %v", r, err)
	}
	if retry, err := f.session.ResolveConflict(a); err != nil || retry != r {
		t.Fatalf("resolution retry changed receipt: %+v %v", retry, err)
	}
	foreign := capture(t, f.session, memory("foreign", "another artifact"))
	if _, err := f.session.ResolveConflict(Resolution{OperationID: "foreign-selection", ArtifactID: first.ArtifactID, RevisionID: foreign.RevisionID, ExpectedRevision: r.RevisionID}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("selected revision from another artifact: %v", err)
	}
	changed := a
	changed.RevisionID = current.RevisionID
	if _, err := f.session.ResolveConflict(changed); !errors.Is(err, ErrOperation) {
		t.Fatalf("changed resolution retry: %v", err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := f.session.Inspect(first.ArtifactID)
	if err != nil || artifact.HeadRevision != r.RevisionID || len(artifact.Revisions) != 4 || artifact.Revisions[2].State != "resolved" {
		t.Fatalf("resolved history lost: %+v %v", artifact, err)
	}
	selected := artifact.Revisions[3]
	if selected.Content != "competing" || selected.Base != current.RevisionID || selected.Verification != "unverified" || len(selected.Associations.Identifiers) != 1 {
		t.Fatalf("resolution changed evidence or trust: %+v", selected)
	}
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	result, err := f.session.Search(SearchRequest{Query: "candidate-identifier"})
	if err != nil || len(result.Results) != 1 || result.Results[0].RevisionID != r.RevisionID {
		t.Fatalf("resolved revision not indexed: %+v %v", result, err)
	}
	pending, err := f.session.Pending()
	if err != nil || len(pending) != 5 || pending[3].Action != "resolve-conflict" || pending[3].RevisionID != r.RevisionID {
		t.Fatalf("resolved pending work: %+v %v", pending, err)
	}
	kept, err := f.session.ResolveConflict(Resolution{OperationID: "keep", ArtifactID: first.ArtifactID, RevisionID: r.RevisionID, ExpectedRevision: r.RevisionID})
	if err != nil || kept.RevisionID == r.RevisionID {
		t.Fatalf("keep accepted version: %+v %v", kept, err)
	}
	if _, err := f.session.Retire(first.ArtifactID, kept.RevisionID, "retire-resolved"); err != nil {
		t.Fatal(err)
	}
	if retry, err := f.session.ResolveConflict(a); err != nil || retry != r {
		t.Fatalf("historical resolution receipt lost: %+v %v", retry, err)
	}
	if artifact, err := f.session.Inspect(first.ArtifactID); err != nil || artifact.Lifecycle != "retired" || artifact.HeadRevision != kept.RevisionID {
		t.Fatalf("retry revived resolved artifact: %+v %v", artifact, err)
	}
	if err := f.auth.RevokeSession(f.token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.session.ResolveConflict(a); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("revoked retry disclosed resolution receipt: %v", err)
	}
}

func TestLegacyApprovalReceiptRemainsRetryable(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "legacy", "", "", "legacy approved snapshot")
	owner, _ := registered(t, f, r, "source", "build", false)
	a := Approval{OperationID: "legacy-approval", ArtifactID: r.ArtifactID, RevisionID: r.RevisionID}
	approved, err := owner.Approve(a)
	if err != nil {
		t.Fatal(err)
	}
	type legacyApproval struct {
		OperationID      string `json:"operation_id"`
		ArtifactID       string `json:"artifact_id"`
		RevisionID       string `json:"revision_id"`
		ExpectedRevision string `json:"expected_revision"`
		Overrides        string `json:"overrides,omitempty"`
		OverrideRevision string `json:"override_revision,omitempty"`
	}
	hash := fingerprint(struct {
		Action string
		legacyApproval
	}{"approve", legacyApproval{OperationID: a.OperationID, ArtifactID: a.ArtifactID, RevisionID: a.RevisionID}})
	if _, err := f.store.db.Exec("UPDATE contributions SET fingerprint=? WHERE operation_id=?", hash, a.OperationID); err != nil {
		t.Fatal(err)
	}
	if retry, err := owner.Approve(a); err != nil || retry != approved {
		t.Fatalf("legacy receipt no longer retryable: %+v %v", retry, err)
	}
	changed := a
	changed.ResolveConflict = true
	if _, err := owner.Approve(changed); !errors.Is(err, ErrOperation) {
		t.Fatalf("changed conflict-review operation reused receipt: %v", err)
	}
}

func TestResolutionAndRetirementRacePreservesOneTransition(t *testing.T) {
	f := setup(t)
	first, current, candidate := competingMemory(t, f)
	other, err := Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	session, _ := other.Session(f.token, f.checkout)
	var resolved, retired Receipt
	var resolveErr, retireErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		resolved, resolveErr = f.session.ResolveConflict(Resolution{OperationID: "resolve-race", ArtifactID: first.ArtifactID, RevisionID: candidate.RevisionID, ExpectedRevision: current.RevisionID})
	})
	wg.Go(func() {
		retired, retireErr = session.Retire(first.ArtifactID, current.RevisionID, "retire-race")
	})
	wg.Wait()
	a, err := f.session.Inspect(first.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if resolveErr == nil {
		if !errors.Is(retireErr, ErrConflict) || retired != (Receipt{}) || a.Lifecycle != "active" || a.HeadRevision != resolved.RevisionID || len(a.Revisions) != 4 {
			t.Fatalf("resolution race lost: %+v %+v %v %v", resolved, a, resolveErr, retireErr)
		}
	} else if retireErr != nil || !errors.Is(resolveErr, ErrConflict) || resolved != (Receipt{}) || a.Lifecycle != "retired" || a.HeadRevision != current.RevisionID || len(a.Revisions) != 3 {
		t.Fatalf("retirement race lost: %+v %+v %v %v", retired, a, resolveErr, retireErr)
	}
}

func TestReconciliationFailuresRollBackReceiptHistoryAndQueue(t *testing.T) {
	for _, action := range []string{"resolve-conflict", "restore"} {
		t.Run(action, func(t *testing.T) {
			f := setup(t)
			first, current, candidate := competingMemory(t, f)
			owner, err := f.store.Owner(f.checkout)
			if err != nil {
				t.Fatal(err)
			}
			if action == "restore" {
				if _, err := f.session.Retire(first.ArtifactID, current.RevisionID, "retire"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.store.IndexBatch(100); err != nil {
				t.Fatal(err)
			}
			before, _ := f.session.Inspect(first.ArtifactID)
			work, _ := f.session.Pending()
			if _, err := f.store.db.Exec(`CREATE TABLE commit_failure (artifact_id TEXT REFERENCES artifacts(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_commit AFTER INSERT ON pending WHEN NEW.action='` + action + `' BEGIN INSERT INTO commit_failure VALUES('missing'); END;`); err != nil {
				t.Fatal(err)
			}
			var receipt Receipt
			if action == "restore" {
				receipt, err = owner.Restore(first.ArtifactID, current.RevisionID, "failure")
			} else {
				receipt, err = f.session.ResolveConflict(Resolution{OperationID: "failure", ArtifactID: first.ArtifactID, RevisionID: candidate.RevisionID, ExpectedRevision: current.RevisionID})
			}
			if !errors.Is(err, ErrUnavailable) || receipt != (Receipt{}) {
				t.Fatalf("failed commit acknowledged: %+v %v", receipt, err)
			}
			after, err := f.session.Inspect(first.ArtifactID)
			if err != nil || after.HeadRevision != before.HeadRevision || after.Lifecycle != before.Lifecycle || len(after.Revisions) != len(before.Revisions) || after.Revisions[2].State != before.Revisions[2].State {
				t.Fatalf("partial lifecycle persisted: %+v %v", after, err)
			}
			pending, err := f.session.Pending()
			if err != nil || len(pending) != len(work) {
				t.Fatalf("partial pending persisted: %+v %v", pending, err)
			}
			var count int
			if err := f.store.db.QueryRow("SELECT count(*) FROM index_queue").Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial index work persisted: %d %v", count, err)
			}
		})
	}
}

func TestRestorationRejectsOldEditsAndReplayedTransitions(t *testing.T) {
	f := setup(t)
	first, current, candidate := competingMemory(t, f)
	owner, _ := f.store.Owner(f.checkout)
	if _, err := owner.Restore(first.ArtifactID, current.RevisionID, "not-retired"); !errors.Is(err, ErrConflict) {
		t.Fatalf("active artifact restored: %v", err)
	}
	if _, err := f.session.Retire(first.ArtifactID, current.RevisionID, "retire"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.session.ResolveConflict(Resolution{OperationID: "retired-resolution", ArtifactID: first.ArtifactID, RevisionID: candidate.RevisionID, ExpectedRevision: current.RevisionID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("resolution undid retirement: %v", err)
	}
	if _, err := owner.Restore(first.ArtifactID, first.RevisionID, "stale-restore"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale restore accepted: %v", err)
	}
	r, err := owner.Restore(first.ArtifactID, current.RevisionID, "restore")
	if err != nil || r.State != "pending-local" || r.RevisionID == current.RevisionID {
		t.Fatalf("restore: %+v %v", r, err)
	}
	if retry, err := owner.Restore(first.ArtifactID, current.RevisionID, "restore"); err != nil || retry != r {
		t.Fatalf("restore retry: %+v %v", retry, err)
	}
	if _, err := owner.Restore(first.ArtifactID, r.RevisionID, "restore"); !errors.Is(err, ErrOperation) {
		t.Fatalf("changed restore retry: %v", err)
	}
	old := memory("old-edit", "old edit")
	old.ArtifactID, old.ExpectedRevision = first.ArtifactID, current.RevisionID
	if got := capture(t, f.session, old); got.State != "conflict" {
		t.Fatalf("old edit replaced restoration: %+v", got)
	}
	if _, err := f.session.Retire(first.ArtifactID, r.RevisionID, "retire-again"); err != nil {
		t.Fatal(err)
	}
	if retry, err := owner.Restore(first.ArtifactID, current.RevisionID, "restore"); err != nil || retry != r {
		t.Fatalf("restore lost original receipt: %+v %v", retry, err)
	}
	if artifact, err := f.session.Inspect(first.ArtifactID); err != nil || artifact.Lifecycle != "retired" {
		t.Fatalf("restore retry undid later retirement: %+v %v", artifact, err)
	}
}

func TestResolutionAndRestoreUseCurrentRepositoryAuthority(t *testing.T) {
	f := setup(t)
	first, current, candidate := competingMemory(t, f)
	other := filepath.Join(filepath.Dir(f.checkout), "other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, other, "init", "-q")
	if _, err := f.auth.Register(other, "Personal"); err != nil {
		t.Fatal(err)
	}
	token, _ := f.auth.CreateSession(other, "Personal")
	session, _ := f.store.Session(token, other)
	owner, _ := f.store.Owner(other)
	for _, id := range []string{first.ArtifactID, "missing"} {
		if r, err := session.ResolveConflict(Resolution{OperationID: "denied", ArtifactID: id, RevisionID: candidate.RevisionID, ExpectedRevision: current.RevisionID}); !errors.Is(err, authority.ErrDenied) || r != (Receipt{}) {
			t.Fatalf("resolution disclosed restricted object: %+v %v", r, err)
		}
		if r, err := owner.Restore(id, current.RevisionID, "denied"); !errors.Is(err, authority.ErrDenied) || r != (Receipt{}) {
			t.Fatalf("restoration disclosed restricted object: %+v %v", r, err)
		}
	}
	if err := f.auth.RevokeSession(f.token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.session.ResolveConflict(Resolution{OperationID: "revoked", ArtifactID: first.ArtifactID, RevisionID: candidate.RevisionID, ExpectedRevision: current.RevisionID}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("revoked session resolved: %v", err)
	}
}

func TestConcurrentReplacementAndIndependentObservations(t *testing.T) {
	f := setup(t)
	first := capture(t, f.session, memory("first", "original"))
	other, err := Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	second, _ := other.Session(f.token, f.checkout)
	sessions := []*Session{f.session, second}
	results := make([]Receipt, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, session := range sessions {
		wg.Go(func() {
			c := memory([]string{"one", "two"}[i], []string{"replacement one", "replacement two"}[i])
			c.ArtifactID, c.ExpectedRevision = first.ArtifactID, first.RevisionID
			results[i], errs[i] = session.Contribute(c)
		})
	}
	wg.Wait()
	accepted, conflicts := 0, 0
	for i, r := range results {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if r.State == "pending-local" {
			accepted++
		} else if r.State == "conflict" {
			conflicts++
		}
	}
	a, err := f.session.Inspect(first.ArtifactID)
	if err != nil || len(a.Revisions) != 3 || accepted != 1 || conflicts != 1 {
		t.Fatalf("concurrent replacements lost: %+v %+v %v", results, a, err)
	}
	one := capture(t, f.session, memory("observation-one", "Contradictory observation"))
	two := capture(t, second, memory("observation-two", "Contradictory observation"))
	if one.ArtifactID == two.ArtifactID || capture(t, second, memory("observation-one", "Contradictory observation")) != one {
		t.Fatal("independent observations collapsed or retry duplicated")
	}
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	found, err := f.session.Search(SearchRequest{Query: "contradictory"})
	if err != nil || len(found.Results) != 2 {
		t.Fatalf("independent observations unavailable: %+v %v", found, err)
	}
}
