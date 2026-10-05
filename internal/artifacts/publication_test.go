package artifacts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-substrate/substrate/internal/authority"
)

type publicationFixture struct {
	*fixture
	work                         *Session
	owner, personal              *Owner
	workContext, personalContext authority.Context
	source                       Receipt
}

func publicationSetup(t *testing.T) *publicationFixture {
	t.Helper()
	f := &publicationFixture{fixture: setup(t)}
	root := filepath.Join(filepath.Dir(f.checkout), "work")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	if err := f.auth.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Register(root, "Work"); err != nil {
		t.Fatal(err)
	}
	token, err := f.auth.CreateSession(root, "Work")
	if err != nil {
		t.Fatal(err)
	}
	f.work, err = f.store.Session(token, root)
	if err != nil {
		t.Fatal(err)
	}
	f.owner, err = f.store.Owner(root)
	if err != nil {
		t.Fatal(err)
	}
	f.personal, err = f.store.Owner(f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	f.workContext, err = f.auth.Authenticate(token, root)
	if err != nil {
		t.Fatal(err)
	}
	f.personalContext, err = f.auth.Authenticate(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	f.source = capture(t, f.work, memory("source", "Private Work customer account information."))
	return f
}
func (f *publicationFixture) input() PublicationInput {
	return PublicationInput{OperationID: "derive-1", Content: "Test changes against the documented behavior.", Sources: []PublicationSource{{f.source.ArtifactID, f.source.RevisionID}}, Destination: PublicationDestination{f.personalContext.SpaceID, f.personalContext.RepositoryID}}
}
func (f *publicationFixture) permit(t *testing.T) {
	t.Helper()
	if err := f.owner.SetPublicationPolicy("export", "", true); err != nil {
		t.Fatal(err)
	}
	if err := f.personal.SetPublicationPolicy("publish", "", true); err != nil {
		t.Fatal(err)
	}
}
func (f *publicationFixture) review(t *testing.T) (Publication, string, PublicationReview) {
	t.Helper()
	f.permit(t)
	p, err := f.work.ProposePublication(f.input())
	if err != nil {
		t.Fatal(err)
	}
	token, err := f.owner.IssuePublicationReview(p.ID, p.RevisionID, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	review, err := f.store.ReviewPublication(token)
	if err != nil {
		t.Fatal(err)
	}
	return p, token, review
}
func approval(review PublicationReview) PublicationApproval {
	return PublicationApproval{OperationID: "publish-1", RevisionID: review.Proposal.RevisionID, Snapshot: review.Snapshot}
}

func TestPublicationRequiresExactSeparateReviewAndPreservesPrivateLineage(t *testing.T) {
	f := publicationSetup(t)
	p, token, review := f.review(t)
	if _, err := f.store.Publish(f.work.token, approval(review)); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("ordinary session approved: %v", err)
	}
	if _, err := f.store.Session(token, f.work.checkout); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("review token became a session: %v", err)
	}
	if _, err := f.session.Publication(p.ID); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("Personal saw Work proposal: %v", err)
	}
	if _, err := f.session.Read(ReadRequest{ArtifactID: f.source.ArtifactID}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("Personal read Work: %v", err)
	}
	r, err := f.store.Publish(token, approval(review))
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "published" || r.ArtifactID == f.source.ArtifactID {
		t.Fatalf("unexpected publication: %+v", r)
	}
	d, err := f.session.Read(ReadRequest{ArtifactID: r.ArtifactID})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(d)
	for _, private := range []string{f.source.ArtifactID, f.source.RevisionID, f.workContext.SpaceID, f.workContext.RepositoryID, f.work.checkout, "customer", "Work"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("private lineage disclosed: %s", private)
		}
	}
	if d.Revision.Content != p.Content || d.Revision.Verification != "unverified" || d.Revision.Provenance != "Human-reviewed generalized lesson" || d.Revision.Source != nil || len(d.Revision.Associations.Related) != 0 {
		t.Fatalf("wrong derived artifact: %+v", d)
	}
	if retry, err := f.store.Publish(token, approval(review)); err != nil || retry != r {
		t.Fatalf("retry changed: %+v %v", retry, err)
	}
	changed := approval(review)
	changed.OperationID = "different"
	if _, err := f.store.Publish(token, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("consumed grant reused: %v", err)
	}
}

func TestPublicationReviewInvalidatedByEachChangedBoundary(t *testing.T) {
	for _, change := range []string{"content", "destination", "source", "export-policy", "destination-policy", "deny-then-allow", "artifact-policy", "retirement", "restoration"} {
		t.Run(change, func(t *testing.T) {
			f := publicationSetup(t)
			p, token, review := f.review(t)
			switch change {
			case "content", "destination":
				in := f.input()
				in.ID = p.ID
				in.ExpectedRevision = p.RevisionID
				in.OperationID = "derive-2"
				if change == "content" {
					in.Content = "Changed lesson."
				} else {
					in.Destination.RepositoryID = strings.Repeat("b", 64)
				}
				if _, err := f.work.ProposePublication(in); err != nil {
					t.Fatal(err)
				}
			case "source":
				in := memory("edit-source", "New private content.")
				in.ArtifactID = f.source.ArtifactID
				in.ExpectedRevision = f.source.RevisionID
				capture(t, f.work, in)
			case "export-policy":
				if err := f.owner.SetPublicationPolicy("export", "", true); err != nil {
					t.Fatal(err)
				}
			case "destination-policy":
				if err := f.personal.SetPublicationPolicy("publish", "", true); err != nil {
					t.Fatal(err)
				}
			case "deny-then-allow":
				for _, v := range []bool{false, true} {
					if err := f.owner.SetPublicationPolicy("export", "", v); err != nil {
						t.Fatal(err)
					}
				}
			case "artifact-policy":
				if err := f.owner.SetPublicationPolicy("export", f.source.ArtifactID, true); err != nil {
					t.Fatal(err)
				}
			case "retirement", "restoration":
				if _, err := f.work.Retire(f.source.ArtifactID, f.source.RevisionID, "retire-source"); err != nil {
					t.Fatal(err)
				}
				if change == "restoration" {
					if _, err := f.owner.Restore(f.source.ArtifactID, f.source.RevisionID, "restore-source"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if r, err := f.store.Publish(token, approval(review)); err == nil || r != (Receipt{}) {
				t.Fatalf("stale review published: %+v %v", r, err)
			}
			results, err := f.session.Search(SearchRequest{})
			if err != nil || len(results.Results) != 0 {
				t.Fatalf("failed publish left artifact: %+v %v", results, err)
			}
		})
	}
}

func TestMandatoryPublicationDenialsCannotBecomeApproval(t *testing.T) {
	f := publicationSetup(t)
	if _, err := f.work.ProposePublication(f.input()); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("default export was not denied: %v", err)
	}
	p, token, review := f.review(t)
	_ = p
	for _, scope := range []string{"space", "artifact", "destination"} {
		t.Run(scope, func(t *testing.T) {
			f.permit(t)
			_ = f.owner.SetPublicationPolicy("export", f.source.ArtifactID, true)
			if scope == "space" {
				_ = f.owner.SetPublicationPolicy("export", "", false)
			} else if scope == "artifact" {
				_ = f.owner.SetPublicationPolicy("export", f.source.ArtifactID, false)
			} else {
				_ = f.personal.SetPublicationPolicy("publish", "", false)
			}
			if _, err := f.store.Publish(token, approval(review)); !errors.Is(err, authority.ErrDenied) {
				t.Fatalf("mandatory denial relaxed: %v", err)
			}
		})
	}
}

func TestPublicationAtomicFailureConcurrentConsumptionAndRevocation(t *testing.T) {
	f := publicationSetup(t)
	_, token, review := f.review(t)
	if _, err := f.store.db.Exec(`CREATE TRIGGER fail_publication BEFORE INSERT ON contributions BEGIN SELECT RAISE(ABORT,'failure'); END`); err != nil {
		t.Fatal(err)
	}
	if r, err := f.store.Publish(token, approval(review)); !errors.Is(err, ErrUnavailable) || r != (Receipt{}) {
		t.Fatalf("failed commit acknowledged: %+v %v", r, err)
	}
	if _, err := f.store.db.Exec("DROP TRIGGER fail_publication"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	receipts := make(chan Receipt, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { r, err := f.store.Publish(token, approval(review)); receipts <- r; failures <- err })
	}
	wg.Wait()
	close(receipts)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first Receipt
	for r := range receipts {
		if first == (Receipt{}) {
			first = r
		} else if r != first {
			t.Fatal("concurrent publication duplicated")
		}
	}
	results, err := f.session.Search(SearchRequest{})
	if err != nil || len(results.Results) != 1 {
		t.Fatalf("publication count: %+v %v", results, err)
	}
	if err := f.owner.RevokePublicationReview(token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ReviewPublication(token); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("revoked review disclosed: %v", err)
	}
}

func TestPublicationExpiredForgedAndWrongSnapshotDenied(t *testing.T) {
	f := publicationSetup(t)
	_, token, review := f.review(t)
	wrong := approval(review)
	wrong.Snapshot = "forged"
	if _, err := f.store.Publish(token, wrong); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong snapshot published: %v", err)
	}
	if _, err := f.store.ReviewPublication(strings.Repeat("a", 64)); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("forged grant: %v", err)
	}
	if _, err := f.store.db.Exec("UPDATE review_grants SET expires_at=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Publish(token, approval(review)); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("expired grant: %v", err)
	}
}

func TestPublicationReviewRejectsReplacedCheckoutIdentity(t *testing.T) {
	for _, side := range []string{"source", "destination"} {
		t.Run(side, func(t *testing.T) {
			f := publicationSetup(t)
			root := f.work.checkout
			if side == "destination" {
				root = f.checkout
			}
			git(t, root, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "--allow-empty", "-qm", "initial")
			_, token, review := f.review(t)
			clone := filepath.Join(filepath.Dir(root), "clone")
			git(t, root, "clone", "-q", root, clone)
			if err := os.Rename(filepath.Join(root, ".git"), filepath.Join(filepath.Dir(root), "original-git")); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(clone, ".git"), filepath.Join(root, ".git")); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.ReviewPublication(token); !errors.Is(err, authority.ErrDenied) {
				t.Fatalf("replacement checkout received source review: %v", err)
			}
			if receipt, err := f.store.Publish(token, approval(review)); !errors.Is(err, authority.ErrDenied) || receipt != (Receipt{}) {
				t.Fatalf("replacement checkout published: %+v %v", receipt, err)
			}
		})
	}
}

func TestPublicationDeferredCommitFailureRollsBackEveryWrite(t *testing.T) {
	f := publicationSetup(t)
	p, token, review := f.review(t)
	if _, err := f.store.db.Exec(`CREATE TABLE publication_commit_failure (artifact_id TEXT REFERENCES artifacts(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_publication_commit AFTER INSERT ON pending WHEN NEW.action='publish' BEGIN INSERT INTO publication_commit_failure VALUES('missing'); END;`); err != nil {
		t.Fatal(err)
	}
	if receipt, err := f.store.Publish(token, approval(review)); !errors.Is(err, ErrUnavailable) || receipt != (Receipt{}) {
		t.Fatalf("deferred commit failure acknowledged: %+v %v", receipt, err)
	}
	for _, table := range []string{"artifacts", "revisions", "contributions", "pending"} {
		var count int
		query := "SELECT count(*) FROM " + table + " WHERE "
		if table == "revisions" {
			query += "artifact_id IN (SELECT id FROM artifacts WHERE space_id=?)"
		} else {
			query += "space_id=?"
		}
		if err := f.store.db.QueryRow(query, f.personalContext.SpaceID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed commit left %s: %d %v", table, count, err)
		}
	}
	var queued int
	if err := f.store.db.QueryRow("SELECT count(*) FROM index_queue WHERE artifact_id NOT IN (SELECT id FROM artifacts)").Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("failed commit left index work: %d %v", queued, err)
	}
	var receipt, operation, audit string
	if err := f.store.db.QueryRow("SELECT receipt,operation_id FROM review_grants WHERE token_hash=?", reviewDigest(token)).Scan(&receipt, &operation); err != nil || receipt != "" || operation != "" {
		t.Fatalf("failed commit consumed grant: %q %q %v", receipt, operation, err)
	}
	if err := f.store.db.QueryRow("SELECT receipt,review FROM publication_proposals WHERE id=?", p.ID).Scan(&receipt, &audit); err != nil || receipt != "" || audit != "" {
		t.Fatalf("failed commit completed proposal: %q %q %v", receipt, audit, err)
	}
	if _, err := f.store.db.Exec("DROP TRIGGER fail_publication_commit"); err != nil {
		t.Fatal(err)
	}
	if receipt, err := f.store.Publish(token, approval(review)); err != nil || receipt.State != "published" {
		t.Fatalf("exact retry after repair failed: %+v %v", receipt, err)
	}
	if err := f.owner.RevokePublicationReview(token); err != nil {
		t.Fatal(err)
	}
	if err := f.store.db.QueryRow("SELECT review FROM publication_proposals WHERE id=?", p.ID).Scan(&audit); err != nil || !strings.Contains(audit, review.Snapshot) {
		t.Fatalf("grant cleanup erased private completion audit: %q %v", audit, err)
	}
}

func TestEveryPublicationSourceMustPermitExportWithinCurrentScope(t *testing.T) {
	for _, caseName := range []string{"denied", "foreign-repository", "missing", "retired", "candidate"} {
		t.Run(caseName, func(t *testing.T) {
			f := publicationSetup(t)
			f.permit(t)
			second := capture(t, f.work, memory("second", "Another restricted source."))
			in := f.input()
			ref := PublicationSource{second.ArtifactID, second.RevisionID}
			switch caseName {
			case "denied":
				if err := f.owner.SetPublicationPolicy("export", second.ArtifactID, false); err != nil {
					t.Fatal(err)
				}
			case "foreign-repository":
				root := filepath.Join(filepath.Dir(f.checkout), "foreign")
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				git(t, root, "init", "-q")
				if _, err := f.auth.Register(root, "Work"); err != nil {
					t.Fatal(err)
				}
				token, err := f.auth.CreateSession(root, "Work")
				if err != nil {
					t.Fatal(err)
				}
				session, err := f.store.Session(token, root)
				if err != nil {
					t.Fatal(err)
				}
				other := capture(t, session, memory("other", "Hidden same-space source."))
				ref = PublicationSource{other.ArtifactID, other.RevisionID}
			case "missing":
				ref = PublicationSource{strings.Repeat("f", 64), strings.Repeat("e", 64)}
			case "retired":
				if _, err := f.work.Retire(second.ArtifactID, second.RevisionID, "retire-second"); err != nil {
					t.Fatal(err)
				}
			case "candidate":
				if err := os.WriteFile(filepath.Join(f.work.checkout, "SKILL.md"), []byte("Unapproved source"), 0600); err != nil {
					t.Fatal(err)
				}
				git(t, f.work.checkout, "add", "SKILL.md")
				git(t, f.work.checkout, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "candidate")
				r := capture(t, f.work, Contribution{OperationID: "candidate", Kind: "skill", Source: &Source{Commit: git(t, f.work.checkout, "rev-parse", "HEAD"), Path: "SKILL.md"}})
				ref = PublicationSource{r.ArtifactID, r.RevisionID}
			}
			in.Sources = append(in.Sources, ref)
			if _, err := f.work.ProposePublication(in); !errors.Is(err, authority.ErrDenied) {
				t.Fatalf("second source escaped conjunction: %v", err)
			}
		})
	}
}

func TestDistinctReviewGrantsCannotPublishOneProposalTwice(t *testing.T) {
	f := publicationSetup(t)
	p, token, review := f.review(t)
	second, err := f.owner.IssuePublicationReview(p.ID, p.RevisionID, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	responses := make(chan error, 2)
	for _, credential := range []string{token, second} {
		wg.Go(func() { _, err := f.store.Publish(credential, approval(review)); responses <- err })
	}
	wg.Wait()
	close(responses)
	passed, conflicted := 0, 0
	for err := range responses {
		if err == nil {
			passed++
		} else if errors.Is(err, ErrConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || conflicted != 1 {
		t.Fatalf("distinct grants duplicated publication: %d successes %d conflicts", passed, conflicted)
	}
	results, err := f.session.Search(SearchRequest{})
	if err != nil || len(results.Results) != 1 {
		t.Fatalf("duplicate artifacts: %+v %v", results, err)
	}
}

func TestConsumedReviewRetryKeepsReceiptAfterPolicyAndSourceChanges(t *testing.T) {
	f := publicationSetup(t)
	_, token, review := f.review(t)
	r, err := f.store.Publish(token, approval(review))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.owner.SetPublicationPolicy("export", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.work.Retire(f.source.ArtifactID, f.source.RevisionID, "retire"); err != nil {
		t.Fatal(err)
	}
	if retry, err := f.store.Publish(token, approval(review)); err != nil || retry != r {
		t.Fatalf("historical retry changed receipt: %+v %v", retry, err)
	}
	if _, err := f.store.ReviewPublication(token); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("historical receipt reopened restricted content: %v", err)
	}
}

func TestAcknowledgedPublicationProposalRetrySurvivesSourceAndPolicyChanges(t *testing.T) {
	for _, change := range []string{"source-edit", "retirement", "export-denial"} {
		t.Run(change, func(t *testing.T) {
			f := publicationSetup(t)
			f.permit(t)
			in := f.input()
			p, err := f.work.ProposePublication(in)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "source-edit":
				c := memory("new-source", "changed")
				c.ArtifactID = f.source.ArtifactID
				c.ExpectedRevision = f.source.RevisionID
				capture(t, f.work, c)
			case "retirement":
				if _, err := f.work.Retire(f.source.ArtifactID, f.source.RevisionID, "retire"); err != nil {
					t.Fatal(err)
				}
			case "export-denial":
				if err := f.owner.SetPublicationPolicy("export", "", false); err != nil {
					t.Fatal(err)
				}
			}
			retry, err := f.work.ProposePublication(in)
			if err != nil || retry.ID != p.ID || retry.RevisionID != p.RevisionID || retry.Content != p.Content {
				t.Fatalf("acknowledgment lost: %+v %v", retry, err)
			}
			changed := in
			changed.Content = "different"
			if _, err := f.work.ProposePublication(changed); !errors.Is(err, ErrOperation) {
				t.Fatalf("changed retry accepted: %v", err)
			}
			if _, err := f.owner.IssuePublicationReview(p.ID, p.RevisionID, f.checkout); err == nil {
				t.Fatal("retry receipt became a current grant")
			}
		})
	}
}

func TestPublicationRejectsUnreviewableSourceInventoryBeforeGrantIssuance(t *testing.T) {
	f := publicationSetup(t)
	f.permit(t)
	in := f.input()
	in.Sources = nil
	for i := range 9 {
		r := capture(t, f.work, memory(string(rune('a'+i)), strings.Repeat("x", 900000)))
		in.Sources = append(in.Sources, PublicationSource{r.ArtifactID, r.RevisionID})
	}
	p, err := f.work.ProposePublication(in)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := f.owner.IssuePublicationReview(p.ID, p.RevisionID, f.checkout); !errors.Is(err, ErrUnavailable) || token != "" {
		t.Fatalf("unusable grant issued: %q %v", token, err)
	}
	var count int
	if err := f.store.db.QueryRow("SELECT count(*) FROM review_grants").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed grant persisted: %d %v", count, err)
	}
}

func TestPublicationReviewIncludesImmutableGitBundleAndInvalidatesOverrideChanges(t *testing.T) {
	f := publicationSetup(t)
	f.permit(t)
	gitFixture := &fixture{auth: f.auth, store: f.store, session: f.work, token: f.work.token, checkout: f.work.checkout}
	if err := os.WriteFile(filepath.Join(f.work.checkout, "reference.txt"), []byte("Private dependency text"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.work.checkout, "add", "reference.txt")
	base := gitCandidate(t, gitFixture, "git-default", "", "", "Default skill text")
	owner, choice := registered(t, gitFixture, base, "default", "skill", true)
	approve(t, owner, base, "")
	special := gitCandidate(t, gitFixture, "git-special", "", "", "Specialized skill text")
	if _, err := owner.Register(Registration{ArtifactID: special.ArtifactID, Source: "special", Name: "build", Alias: choice.Alias}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Approve(Approval{OperationID: "approve-special", ArtifactID: special.ArtifactID, RevisionID: special.RevisionID, Overrides: base.ArtifactID, OverrideRevision: base.RevisionID}); err != nil {
		t.Fatal(err)
	}
	in := f.input()
	in.Sources = []PublicationSource{{special.ArtifactID, special.RevisionID}}
	p, err := f.work.ProposePublication(in)
	if err != nil {
		t.Fatal(err)
	}
	token, err := owner.IssuePublicationReview(p.ID, p.RevisionID, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	review, err := f.store.ReviewPublication(token)
	if err != nil {
		t.Fatal(err)
	}
	if review.Sources[0].Choice.Overrides != base.ArtifactID || review.Sources[0].Revision.Content != "Specialized skill text" {
		t.Fatalf("missing exact override: %+v", review.Sources)
	}
	changed := gitCandidate(t, gitFixture, "default-change", base.ArtifactID, base.RevisionID, "New default")
	approve(t, owner, changed, base.RevisionID)
	if _, err := f.store.Publish(token, approval(review)); err == nil {
		t.Fatal("changed override target did not invalidate review")
	}
	// A declared dependency remains in the private review, never the output artifact.
	c := Contribution{OperationID: "bundle", Kind: "agent-definition", Source: &Source{Commit: git(t, f.work.checkout, "rev-parse", "HEAD"), Path: "SKILL.md", Files: []SourceFile{{Path: "reference.txt"}}}}
	r := capture(t, f.work, c)
	if _, err := owner.Register(Registration{ArtifactID: r.ArtifactID, Source: "bundle", Name: "agent"}); err != nil {
		t.Fatal(err)
	}
	approve(t, owner, r, "")
	in = f.input()
	in.OperationID = "derive-bundle"
	in.Sources = []PublicationSource{{r.ArtifactID, r.RevisionID}}
	p, err = f.work.ProposePublication(in)
	if err != nil {
		t.Fatal(err)
	}
	token, err = owner.IssuePublicationReview(p.ID, p.RevisionID, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	review, err = f.store.ReviewPublication(token)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Sources[0].Revision.Source.Files) != 1 || review.Sources[0].Revision.Source.Files[0].Content != "Private dependency text" {
		t.Fatal("private review lost declared dependency bytes")
	}
	if result, err := f.store.Publish(token, approval(review)); err != nil {
		t.Fatal(err)
	} else {
		d, err := f.session.Read(ReadRequest{ArtifactID: result.ArtifactID})
		if err != nil || d.Revision.Source != nil || strings.Contains(d.Revision.Content, "dependency") {
			t.Fatalf("bundle exported: %+v %v", d, err)
		}
	}
}
