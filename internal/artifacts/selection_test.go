package artifacts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func gitCandidate(t *testing.T, f *fixture, op, artifact, base, content string) Receipt {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.checkout, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", "SKILL.md")
	git(t, f.checkout, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", op)
	return capture(t, f.session, Contribution{OperationID: op, ArtifactID: artifact, ExpectedRevision: base, Kind: "skill", Source: &Source{Commit: git(t, f.checkout, "rev-parse", "HEAD"), Path: "SKILL.md"}})
}
func registered(t *testing.T, f *fixture, r Receipt, source, alias string, overridable bool) (*Owner, Choice) {
	t.Helper()
	owner, err := f.store.Owner(f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	c, err := owner.Register(Registration{ArtifactID: r.ArtifactID, Source: source, Name: "build", Alias: alias, Overridable: overridable})
	if err != nil {
		t.Fatalf("register immutable candidate: %v", err)
	}
	return owner, c
}
func approve(t *testing.T, o *Owner, r Receipt, base string) Receipt {
	t.Helper()
	result, err := o.Approve(Approval{OperationID: "approve-" + r.OperationID, ArtifactID: r.ArtifactID, RevisionID: r.RevisionID, ExpectedRevision: base})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestApprovedSnapshotSurvivesGitChangesAndReopen(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "first", "", "", "approved bytes")
	owner, choice := registered(t, f, r, "repository", "build", false)
	if _, err := f.session.Resolve(choice.Qualified); err == nil {
		t.Fatal("candidate delivered before approval")
	}
	approved := approve(t, owner, r, "")
	if approved.State != "approved" {
		t.Fatalf("not approved: %+v", approved)
	}
	second := gitCandidate(t, f, "changed", r.ArtifactID, r.RevisionID, "unapproved replacement")
	if err := os.Remove(filepath.Join(f.checkout, "SKILL.md")); err != nil {
		t.Fatal(err)
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
	delivered, err := f.session.Resolve("build")
	if err != nil || delivered.Revision.Content != "approved bytes" || delivered.Revision.ID != r.RevisionID || delivered.Revision.Source == nil {
		t.Fatalf("source movement replaced approval: %+v %v", delivered, err)
	}
	inspected, err := f.session.Inspect(r.ArtifactID)
	if err != nil || inspected.HeadRevision != r.RevisionID || inspected.Revisions[1].ID != second.RevisionID || inspected.Revisions[1].State != "candidate" {
		t.Fatalf("candidate not inspectable: %+v %v", inspected, err)
	}
	pending, err := f.session.Pending()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range pending {
		if w.Action == "approve" && w.RevisionID == r.RevisionID {
			found = true
		}
	}
	if !found {
		t.Fatal("approval absent from durable pending work")
	}
}
func TestQualifiedChoicesAndAmbiguousAlias(t *testing.T) {
	f := setup(t)
	first := gitCandidate(t, f, "one", "", "", "first choice")
	owner, a := registered(t, f, first, "one", "build", false)
	approve(t, owner, first, "")
	second := gitCandidate(t, f, "two", "", "", "second choice")
	_, b := registered(t, f, second, "two", "build", false)
	approve(t, owner, second, "")
	if a.Qualified == b.Qualified || !strings.Contains(a.Qualified, "/one/build") {
		t.Fatalf("identity collision: %+v %+v", a, b)
	}
	for _, c := range []Choice{a, b} {
		d, err := f.session.Resolve(c.Qualified)
		if err != nil || d.Choice.ArtifactID != c.ArtifactID {
			t.Fatalf("qualified choice unavailable: %+v %v", d, err)
		}
	}
	if _, err := f.session.Resolve("build"); !errors.Is(err, ErrConflict) {
		t.Fatalf("ambiguous alias delivered: %v", err)
	}
	choices, err := f.session.Choices("build")
	if err != nil || len(choices) != 2 || choices[0].State != "conflict" || choices[1].State != "conflict" {
		t.Fatalf("alias conflict not explained: %+v %v", choices, err)
	}
}
func TestStaleApprovalAndRetirementCannotRestoreCandidate(t *testing.T) {
	f := setup(t)
	first := gitCandidate(t, f, "one", "", "", "initial")
	owner, choice := registered(t, f, first, "source", "build", false)
	approved := approve(t, owner, first, "")
	next := gitCandidate(t, f, "two", first.ArtifactID, first.RevisionID, "next")
	a := Approval{OperationID: "choose-next", ArtifactID: first.ArtifactID, RevisionID: next.RevisionID, ExpectedRevision: ""}
	if _, err := owner.Approve(a); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale head approval accepted: %v", err)
	}
	if _, err := f.session.Retire(first.ArtifactID, first.RevisionID, "retire"); err != nil {
		t.Fatal(err)
	}
	a.ExpectedRevision = first.RevisionID
	if _, err := owner.Approve(a); !errors.Is(err, ErrConflict) {
		t.Fatalf("retired artifact restored: %v", err)
	}
	if _, err := f.session.Resolve(choice.Qualified); err == nil {
		t.Fatal("retired approved snapshot delivered")
	}
	choices, err := f.session.Choices("build")
	if err != nil || len(choices) != 1 || choices[0].State != "retired" {
		t.Fatalf("retirement not inspectable: %+v %v", choices, err)
	}
	if retry, err := owner.Approve(Approval{OperationID: "approve-one", ArtifactID: first.ArtifactID, RevisionID: first.RevisionID}); err != nil || retry != approved {
		t.Fatalf("approval receipt retry: %+v %v", retry, err)
	}
}
func TestChoicesFilterBeforeConflictAndReauthenticate(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "personal", "", "", "permitted content")
	owner, choice := registered(t, f, r, "source", "build", false)
	approve(t, owner, r, "")
	other := filepath.Join(filepath.Dir(f.checkout), "other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, other, "init", "-q")
	if err := f.auth.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Register(other, "Work"); err != nil {
		t.Fatal(err)
	}
	token, err := f.auth.CreateSession(other, "Work")
	if err != nil {
		t.Fatal(err)
	}
	work, err := f.store.Session(token, other)
	if err != nil {
		t.Fatal(err)
	}
	hidden := &fixture{store: f.store, auth: f.auth, checkout: other, session: work}
	private := gitCandidate(t, hidden, "work", "", "", "hidden source")
	wo, _ := registered(t, hidden, private, "source", "build", false)
	approve(t, wo, private, "")
	choices, err := f.session.Choices("build")
	if err != nil || len(choices) != 1 || choices[0].State != "effective" {
		t.Fatalf("unauthorized source affected conflict: %+v %v", choices, err)
	}
	if _, err := work.Resolve(choice.Qualified); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("qualified cross-space leak: %v", err)
	}
	if err := f.auth.RevokeSession(f.token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.session.Choices(""); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("revoked listing: %v", err)
	}
	if _, err := f.session.Resolve(choice.Qualified); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("revoked delivery: %v", err)
	}
}
func TestOverrideRequiresExplicitApprovedOverridableDefault(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "default", "", "", "default")
	owner, defaultChoice := registered(t, f, r, "default", "build", true)
	approve(t, owner, r, "")
	spec := gitCandidate(t, f, "specific", "", "", "specialization")
	_, specificChoice := registered(t, f, spec, "project", "build", false)
	approved := Approval{OperationID: "specialize", ArtifactID: spec.ArtifactID, RevisionID: spec.RevisionID, Overrides: r.ArtifactID, OverrideRevision: r.RevisionID}
	if _, err := owner.Approve(approved); err != nil {
		t.Fatal(err)
	}
	choices, err := f.session.Choices(specificChoice.Qualified)
	if err != nil || len(choices) != 1 || choices[0].Overrides != r.ArtifactID || choices[0].OverrideRevision != r.RevisionID {
		t.Fatalf("approved relationship not inspectable: %+v %v", choices, err)
	}
	defaults, err := f.session.Choices(defaultChoice.Qualified)
	if err != nil || len(defaults) != 1 || !defaults[0].Overridable {
		t.Fatalf("default registration not inspectable: %+v %v", defaults, err)
	}
	d, err := f.session.Resolve("build")
	if err != nil || d.Revision.Content != "specialization" {
		t.Fatalf("explicit override not effective: %+v %v", d, err)
	}
	d, err = f.session.Resolve(defaultChoice.Qualified)
	if err != nil || d.Revision.Content != "default" {
		t.Fatalf("qualified default lost: %+v %v", d, err)
	}
	changed := gitCandidate(t, f, "default-changed", r.ArtifactID, r.RevisionID, "changed default")
	approve(t, owner, changed, r.RevisionID)
	if _, err := f.session.Resolve("build"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale override silently fell back: %v", err)
	}
	if _, err := f.session.Resolve(specificChoice.Qualified); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale relationship delivered: %v", err)
	}
	immutable := gitCandidate(t, f, "nonoverride", "", "", "fixed default")
	registered(t, f, immutable, "fixed", "fixed", false)
	approve(t, owner, immutable, "")
	invalid := gitCandidate(t, f, "invalid", "", "", "invalid override")
	registered(t, f, invalid, "invalid", "fixed", false)
	if _, err := owner.Approve(Approval{OperationID: "bad", ArtifactID: invalid.ArtifactID, RevisionID: invalid.RevisionID, Overrides: immutable.ArtifactID, OverrideRevision: immutable.RevisionID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("nonoverridable target accepted: %v", err)
	}
}
func TestRegistrationAndApprovalFailuresCommitNothing(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "one", "", "", "one")
	owner, c := registered(t, f, r, "source", "build", false)
	duplicate := gitCandidate(t, f, "two", "", "", "two")
	if _, err := owner.Register(Registration{ArtifactID: duplicate.ArtifactID, Source: "source", Name: "build"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("qualified registration reused: %v", err)
	}
	if _, err := f.store.db.Exec(`CREATE TRIGGER fail_approval BEFORE INSERT ON pending WHEN NEW.action='approve' BEGIN SELECT RAISE(ABORT,'failure'); END`); err != nil {
		t.Fatal(err)
	}
	if receipt, err := owner.Approve(Approval{OperationID: "approve", ArtifactID: r.ArtifactID, RevisionID: r.RevisionID}); !errors.Is(err, ErrUnavailable) || receipt != (Receipt{}) {
		t.Fatalf("failed approval acknowledged: %+v %v", receipt, err)
	}
	if _, err := f.session.Resolve(c.Qualified); err == nil {
		t.Fatal("failed approval made content effective")
	}
	inspected, err := f.session.Inspect(r.ArtifactID)
	if err != nil || inspected.HeadRevision != "" || inspected.Revisions[0].State != "candidate" {
		t.Fatalf("partial approval persisted: %+v %v", inspected, err)
	}
	if _, err := f.store.Owner(filepath.Dir(f.checkout)); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("unregistered owner context: %v", err)
	}
}

func TestApprovalMarksCompetingCandidatesAndRejectsChangedRetry(t *testing.T) {
	f := setup(t)
	first := gitCandidate(t, f, "first", "", "", "first")
	owner, _ := registered(t, f, first, "source", "build", false)
	competing := gitCandidate(t, f, "competing", first.ArtifactID, "", "competing")
	a := Approval{OperationID: "choose", ArtifactID: first.ArtifactID, RevisionID: first.RevisionID}
	if _, err := owner.Approve(a); err != nil {
		t.Fatal(err)
	}
	inspected, err := f.session.Inspect(first.ArtifactID)
	if err != nil || inspected.Revisions[1].State != "conflict" {
		t.Fatalf("competing candidate not marked conflict: %+v %v", inspected, err)
	}
	a.RevisionID = competing.RevisionID
	if _, err := owner.Approve(a); !errors.Is(err, ErrOperation) {
		t.Fatalf("changed approval retry: %v", err)
	}
	a.OperationID = "stale-candidate"
	a.ExpectedRevision = first.RevisionID
	if _, err := owner.Approve(a); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale candidate promoted: %v", err)
	}
	replacement := gitCandidate(t, f, "resolved", first.ArtifactID, first.RevisionID, "explicit resolution")
	approve(t, owner, replacement, first.RevisionID)
	d, err := f.session.Resolve("build")
	if err != nil || d.Revision.Content != "explicit resolution" {
		t.Fatalf("current resolution unavailable: %+v %v", d, err)
	}
}

func TestIncomparableOverridesAndCyclesBlockSelection(t *testing.T) {
	f := setup(t)
	def := gitCandidate(t, f, "default", "", "", "default")
	owner, dc := registered(t, f, def, "default", "build", true)
	approve(t, owner, def, "")
	for _, name := range []string{"one", "two"} {
		r := gitCandidate(t, f, name, "", "", name)
		registered(t, f, r, name, "build", false)
		if _, err := owner.Approve(Approval{OperationID: "approve-" + name, ArtifactID: r.ArtifactID, RevisionID: r.RevisionID, Overrides: def.ArtifactID, OverrideRevision: def.RevisionID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.session.Resolve("build"); !errors.Is(err, ErrConflict) {
		t.Fatalf("multiple overrides silently ranked: %v", err)
	}
	d, err := f.session.Resolve(dc.Qualified)
	if err != nil || d.Revision.Content != "default" {
		t.Fatalf("qualified default blocked: %+v %v", d, err)
	}
	proposed := gitCandidate(t, f, "cycle", def.ArtifactID, def.RevisionID, "cycle")
	if _, err := owner.Approve(Approval{OperationID: "self-cycle", ArtifactID: def.ArtifactID, RevisionID: proposed.RevisionID, ExpectedRevision: def.RevisionID, Overrides: def.ArtifactID, OverrideRevision: def.RevisionID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("self override accepted: %v", err)
	}
}

func TestSameSpaceRepositoryAlternativesDoNotInfluenceAlias(t *testing.T) {
	f := setup(t)
	first := gitCandidate(t, f, "here", "", "", "here")
	owner, choice := registered(t, f, first, "source", "build", false)
	approve(t, owner, first, "")
	other := filepath.Join(filepath.Dir(f.checkout), "other-personal")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, other, "init", "-q")
	if _, err := f.auth.Register(other, "Personal"); err != nil {
		t.Fatal(err)
	}
	token, err := f.auth.CreateSession(other, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.store.Session(token, other)
	if err != nil {
		t.Fatal(err)
	}
	hidden := &fixture{auth: f.auth, store: f.store, checkout: other, session: session}
	private := gitCandidate(t, hidden, "there", "", "", "private repository")
	otherOwner, privateChoice := registered(t, hidden, private, "source", "build", false)
	approve(t, otherOwner, private, "")
	choices, err := f.session.Choices("build")
	if err != nil || len(choices) != 1 || choices[0].ArtifactID != choice.ArtifactID || choices[0].State != "effective" {
		t.Fatalf("same-space repository altered alias: %+v %v", choices, err)
	}
	if _, err := f.session.Resolve(privateChoice.Qualified); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("same-space qualified source leaked: %v", err)
	}
	if _, err := owner.Approve(Approval{OperationID: "cross", ArtifactID: private.ArtifactID, RevisionID: private.RevisionID}); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("owner context approved another repository: %v", err)
	}
}

func TestVersionOneStoreMigratesWithoutLosingCandidatesOrReceipts(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "candidate", "", "", "survives migration")
	if _, err := f.store.db.Exec("DROP TABLE maintenance; DROP TABLE review_grants; DROP TABLE publication_revisions; DROP TABLE publication_proposals; DROP TABLE publication_policy; DROP TABLE tokens; DROP TABLE indexed; DROP TABLE index_queue; DROP TABLE revision_associations; DROP TABLE approvals; DROP TABLE registrations; PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
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
	inspected, err := f.session.Inspect(r.ArtifactID)
	if err != nil || inspected.Revisions[0].Content != "survives migration" {
		t.Fatalf("migration lost revision: %+v %v", inspected, err)
	}
	owner, _ := registered(t, f, r, "source", "build", false)
	approve(t, owner, r, "")
	d, err := f.session.Resolve("build")
	if err != nil || d.Revision.ID != r.RevisionID {
		t.Fatalf("migrated snapshot unavailable: %+v %v", d, err)
	}
}

func TestInvalidUTF8ApprovalOperationIsDeniedBeforeReceipt(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "first", "", "", "first")
	owner, _ := registered(t, f, r, "source", "build", false)
	a := Approval{OperationID: string([]byte{'a', 0xff}), ArtifactID: r.ArtifactID, RevisionID: r.RevisionID}
	if _, err := owner.Approve(a); err == nil {
		t.Fatal("invalid UTF-8 approval operation acknowledged")
	}
}

func TestApprovalValidatesAllTextBeforeRetryFingerprint(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "first", "", "", "first")
	owner, _ := registered(t, f, r, "source", "build", false)
	approved := Approval{OperationID: "approved", ArtifactID: r.ArtifactID, RevisionID: r.RevisionID}
	if _, err := owner.Approve(approved); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"artifact", "revision", "expected", "overrides", "override-revision"} {
		invalid := approved
		value := string([]byte{'x', 0xff})
		switch field {
		case "artifact":
			invalid.ArtifactID = value
		case "revision":
			invalid.RevisionID = value
		case "expected":
			invalid.ExpectedRevision = value
		case "overrides":
			invalid.Overrides = value
			invalid.OverrideRevision = r.RevisionID
		case "override-revision":
			invalid.Overrides = r.ArtifactID
			invalid.OverrideRevision = value
		}
		if _, err := owner.Approve(invalid); !errors.Is(err, ErrInvalidText) {
			t.Fatalf("%s validated after receipt fingerprint: %v", field, err)
		}
	}
}
