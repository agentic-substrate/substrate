package importer

import "testing"

// #93: validatePlanSlots keyed on (rel, heading) alone, but splitBlocks emits
// one block per bullet, so a single machine's two bullets under one heading
// looked like an unlisted conflict and the whole import was refused. Siblings
// are told apart by Ordinal since #80; the guard has to use it.

func TestValidatePlanSlotsAllowsSingleMachineSiblings(t *testing.T) {
	p, err := BuildPlan([]Inventory{{
		Hostname: "wsl",
		Files:    []File{{Rel: "AGENTS.md", Path: "/work/x/AGENTS.md", Content: "## Alpha\n\n- first rule\n- second rule\n"}},
	}}, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(p.Blocks) != 2 || len(p.Conflicts) != 0 {
		t.Fatalf("want 2 blocks and 0 conflicts, got %d and %d", len(p.Blocks), len(p.Conflicts))
	}
	if err := validatePlanSlots(*p); err != nil {
		t.Fatalf("two bullets under one heading refused: %v", err)
	}
}

// The guard's real job: two different bodies claiming one identity. A forged
// plan.json that reuses an ordinal must still be refused, or a colliding key
// could promote content the operator never wrote.
func TestValidatePlanSlotsStillRefusesSameOrdinalCollision(t *testing.T) {
	plan := Plan{Blocks: []Block{
		{Hash: sha256Hex([]byte("legit")), Rel: "AGENTS.md", Heading: "Alpha", Ordinal: 0, Body: "legit"},
		{Hash: sha256Hex([]byte("forged")), Rel: "AGENTS.md", Heading: "Alpha", Ordinal: 0, Body: "forged"},
	}}
	err := validatePlanSlots(plan)
	if err == nil {
		t.Fatal("two distinct hashes at the same ordinal were accepted")
	}
	t.Logf("refused as expected: %v", err)
}
