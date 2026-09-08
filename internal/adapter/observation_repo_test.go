package adapter

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/scope"
)

// A repo-keyed observation puts the repo key on the wire and no scope: the
// server owns the repo-key-to-chain binding (R18). The scope it resolves to
// must be one policy.Check accepts for an *agent* principal, or every
// observation a hook enqueues would 403 in the drain loop and retry forever —
// which is exactly the failure the `global:` default caused (#67).
func TestRepoObservationScopeAgreesWithPolicy(t *testing.T) {
	home := t.TempDir()
	db := openDB(t, filepath.Join(home, "adapter.sqlite"))
	cfg := testConfig(home, filepath.Join(home, "adapter.sqlite"), "http://127.0.0.1:1")
	// Even with a scope configured, a repo-keyed observation must not carry it.
	const remote = "github.com/unrounded/api"

	cid, err := EnqueueObservation(db, cfg, Observation{Tool: "Edit", Body: "ok", Repo: remote})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var payload string
	if err := db.sql.QueryRow(`SELECT payload FROM outbox WHERE client_id = ?`, cid).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(payload), &sent); err != nil {
		t.Fatal(err)
	}
	if sent["repo"] != remote {
		t.Fatalf("repo key on the wire is %v, want %q", sent["repo"], remote)
	}
	if _, ok := sent["scope"]; ok {
		t.Fatalf("a repo-keyed observation also carried a scope (%v); repo and scope are mutually exclusive", sent["scope"])
	}

	// The chain the server resolves that key to, modelled exactly as the
	// review path's fake server models it.
	sc, err := scope.Parse(repoScopeWire(remote))
	if err != nil {
		t.Fatalf("the modelled repo chain does not parse: %v", err)
	}
	agent := identity.Principal{
		ID:           uuid.New(),
		Kind:         "agent",
		Trust:        "agent_autonomous",
		TeamIDs:      []uuid.UUID{uuid.New()},
		Capabilities: []string{"memory:write"},
	}
	if err := policy.Check("memory.write", sc, agent); err != nil {
		t.Fatalf("an agent cannot write an observation at the chain a repo key resolves to (%s): %v", sc, err)
	}

	// Anti-vacuity: the same check must actually refuse `global:`, or the
	// assertion above would pass for any scope at all.
	global, err := scope.Parse("global:")
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.Check("memory.write", global, agent); err == nil {
		t.Fatal("policy.Check accepted memory.write at global: for an agent; this test proves nothing")
	}
}

// A repo-keyed observation must not need -scope at all: the hook shim runs
// without one, and re-introducing a scope requirement there would resurrect the
// global: default the guards were built to refuse.
func TestRepoObservationNeedsNoConfiguredScope(t *testing.T) {
	home := t.TempDir()
	db := openDB(t, filepath.Join(home, "adapter.sqlite"))
	cfg := testConfig(home, filepath.Join(home, "adapter.sqlite"), "http://127.0.0.1:1")
	cfg.Scope = ""

	if _, err := EnqueueObservation(db, cfg, Observation{Tool: "Edit", Repo: "github.com/unrounded/api"}); err != nil {
		t.Fatalf("a repo-keyed observation required -scope: %v", err)
	}
}
