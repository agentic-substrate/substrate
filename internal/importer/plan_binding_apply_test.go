package importer

import (
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/identity"
)

// #95, end of the chain: the refusal has to happen inside Apply, before the
// transaction that writes, on both machine paths. The unit tests pin
// CheckPlanWitness; these pin that Apply actually consults it and that nothing
// reached the store when it refused.

// plantedPlan is the issue's own reproduction: the scanned bullet plus a second
// body the scan never saw, under a brand-new heading (route B), which the slot
// guard admits.
func plantedPlan(t *testing.T) (Plan, InventoryWitness) {
	t.Helper()
	p, w := mustPlan(t, scannedInventory())
	p.Blocks = append(p.Blocks, Block{
		Hash: sha256Hex([]byte(plantedBody)), Body: plantedBody,
		Rel: ".claude/CLAUDE.md", Heading: "Deployment", Ordinal: 0,
		Kind: "instruction", Sources: []Source{{Hostname: "mac"}},
	})
	return *p, w
}

// The trusted machine is where a planted block becomes active — a directive
// rendered to agents (Gotcha 5) with no review_item and no conflict. Mutation
// that turns this red again: moving the CheckPlanWitness call out of Apply, or
// past the commit.
func TestApplyTrustedMachineRefusesAPlantedBlock(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	plan, witness := plantedPlan(t)
	_, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Witness: witness,
		Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})
	if err == nil {
		t.Fatal("the trusted machine committed a block the scan never saw")
	}
	if !strings.Contains(err.Error(), "Deployment") {
		t.Fatalf("the refusal does not name the planted block: %v", err)
	}
	// Nothing from this plan may have landed -- not the planted body and not
	// the two real bullets it travelled with, since the refusal precedes the
	// transaction entirely. (The world seeds an unrelated ci.required row.)
	got := countByBodyStatus(t, conn, "instruction", w.project)
	for _, body := range []string{plantedBody, "Run tests before committing.", "Keep the docs current."} {
		if got[body] != "" {
			t.Fatalf("the refusal was not before the write; %q landed as %q", body, got[body])
		}
	}
}

// The untrusted path files rows as proposed, which is still a body an operator
// is one approval away from activating. Mutation that turns this red again:
// gating the witness check on req.Machine == req.TrustedMachine.
func TestApplyUntrustedMachineRefusesAPlantedBlock(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	// "mac" must be imported first, or the untrusted path refuses for an
	// unrelated reason and the test proves nothing about the witness.
	seedPlan, seedWitness := mustPlan(t, scannedInventory())
	if _, err := Apply(ctx, st, ApplyRequest{
		Plan: *seedPlan, Witness: seedWitness,
		Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	}); err != nil {
		t.Fatalf("seed the trusted machine: %v", err)
	}

	plan, witness := plantedPlan(t)
	for i := range plan.Blocks {
		plan.Blocks[i].Sources = []Source{{Hostname: "wsl"}}
	}
	_, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Witness: witness,
		Machine: "wsl", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})
	if err == nil {
		t.Fatal("the untrusted machine committed a block the scan never saw")
	}
	if !strings.Contains(err.Error(), "Deployment") {
		t.Fatalf("the refusal does not name the planted block: %v", err)
	}
	got := countByBodyStatus(t, conn, "instruction", w.project)
	if got[plantedBody] != "" {
		t.Fatalf("the planted body reached the store as %q", got[plantedBody])
	}
}

// The positive control for the whole binding at the Apply layer: the real,
// unedited plan for a multi-bullet file still commits. Mutation that turns this
// red: any over-strict identity (adding Kind, or the plan's own hostname list)
// to the witness comparison.
func TestApplyAdmitsTheScannedPlan(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	plan, witness := mustPlan(t, scannedInventory())
	if _, err := Apply(ctx, st, ApplyRequest{
		Plan: *plan, Witness: witness,
		Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	}); err != nil {
		t.Fatalf("the scanned plan was refused: %v", err)
	}
	got := countByBodyStatus(t, conn, "instruction", w.project)
	for _, body := range []string{"Run tests before committing.", "Keep the docs current."} {
		if got[body] != "active" {
			t.Fatalf("bullet %q status %q, want active; %#v", body, got[body], got)
		}
	}
}
