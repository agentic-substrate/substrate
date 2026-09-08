package adapter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/scope"
)

// agentPrincipal is a normal adapter token: kind=agent, a team membership, and
// no admin trust. It can never be human_admin, so every scope this test feeds
// to policy.Check is judged the way the live server judges it.
func agentPrincipal() identity.Principal {
	return identity.Principal{
		ID:      uuid.Must(uuid.NewV7()),
		Kind:    identity.KindAgent,
		Trust:   identity.TrustAgentInteractive,
		TeamIDs: []uuid.UUID{uuid.Must(uuid.NewV7())},
	}
}

// TestDriftProposalScopeAgreesWithPolicy runs the adapter and the policy layer
// in one test: the scope the adapter actually PUTS on a drift proposal is fed
// to the real policy.Check for review.create as an agent principal. Both halves
// were individually correct and disagreed in production (#58) precisely because
// the adapter's tests stub the review endpoint and the policy's tests never
// look at what the adapter sends.
func TestDriftProposalScopeAgreesWithPolicy(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "acme", "known")
	initRepo(t, repo, "git@github.com:acme/known.git")

	home := t.TempDir()
	homeDest := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(homeDest), 0o750); err != nil {
		t.Fatal(err)
	}
	// Hand edits in both a repo file and a home file, so the proposal for each
	// kind of target is checked.
	if err := os.WriteFile(homeDest, []byte("hand-edited home file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("hand-edited repo file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := newFake(t)
	srv.bound["github.com/acme/known"] = true
	srv.setTargets(pairAt(t, "2026-09-01T00:00:00Z", "3.12").targets...)
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	posted := srv.reviews()
	if len(posted) != 2 {
		t.Fatalf("got %d drift proposals, want one per hand-edited target: %+v", len(posted), posted)
	}
	p := agentPrincipal()
	for _, r := range posted {
		sc, err := scope.Parse(r.Scope)
		if err != nil {
			t.Fatalf("adapter sent scope %q for %s, which scope.Parse rejects: %v", r.Scope, r.Path, err)
		}
		if err := policy.Check("review.create", sc, p); err != nil {
			t.Fatalf("adapter sent scope %q for %s, which an agent token may not write: %v", r.Scope, r.Path, err)
		}
	}

	// The check has teeth: the old default is rejected by the same call.
	old, err := scope.Parse("global:")
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.Check("review.create", old, p); !errors.Is(err, policy.ErrNeedsReview) {
		t.Fatalf("policy.Check on global: = %v, want SUBSTRATE_NEEDS_REVIEW; the assertion above would be vacuous", err)
	}
}

// TestRepoProposalCarriesTheRepoScope pins the derivation itself: the proposal
// for a file inside a checkout is filed at that checkout's repo scope, keyed by
// the git remote, not at whatever -scope the daemon was started with.
func TestRepoProposalCarriesTheRepoScope(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "known")
	initRepo(t, repo, "https://github.com/acme/known.git")
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("hand-edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	srv := newFake(t)
	srv.bound["github.com/acme/known"] = true
	srv.setTargets(agentsTarget(t))
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	posted := srv.reviews()
	if len(posted) != 1 {
		t.Fatalf("got %d proposals, want 1: %+v", len(posted), posted)
	}
	sc, err := scope.Parse(posted[0].Scope)
	if err != nil {
		t.Fatalf("scope %q: %v", posted[0].Scope, err)
	}
	leaf := sc.Leaf()
	if leaf.Kind != scope.Repo || leaf.Name != "github.com/acme/known" {
		t.Fatalf("proposal leaf = %+v, want repo github.com/acme/known", leaf)
	}
	if posted[0].Scope == cfg.Scope {
		t.Fatal("repo proposal was filed at the daemon's configured scope, not the checkout's")
	}
}
