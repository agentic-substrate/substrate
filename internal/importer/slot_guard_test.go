package importer

import (
	"strings"
	"testing"
)

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

// The PR's claim is that the refusal names the ordinal, so an operator can find
// the offending bullet. Nothing enforced that: reverting the message to the old
// slot-only form left the package green.
func TestValidatePlanSlotsErrorNamesSlotAndOrdinal(t *testing.T) {
	plan := Plan{Blocks: []Block{
		{Hash: sha256Hex([]byte("legit")), Rel: "AGENTS.md", Heading: "Alpha", Ordinal: 3, Body: "legit"},
		{Hash: sha256Hex([]byte("forged")), Rel: "AGENTS.md", Heading: "Alpha", Ordinal: 3, Body: "forged"},
	}}
	err := validatePlanSlots(plan)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "ordinal 3") {
		t.Fatalf("error does not name the ordinal: %v", err)
	}
	if !strings.Contains(err.Error(), "AGENTS.md#Alpha") {
		t.Fatalf("error does not name the slot: %v", err)
	}
}

// Keying on the ordinal alone passes every other test in this file, so the
// wrong-but-plausible fix would ship green. Two different files at the same
// ordinal are different identities and must both be admitted.
func TestValidatePlanSlotsDoesNotConflateDifferentSlotsAtSameOrdinal(t *testing.T) {
	plan := Plan{Blocks: []Block{
		{Hash: sha256Hex([]byte("a")), Rel: "AGENTS.md", Heading: "Gotchas", Ordinal: 0, Body: "a"},
		{Hash: sha256Hex([]byte("b")), Rel: "docs/TESTING.md", Heading: "Gotchas", Ordinal: 0, Body: "b"},
	}}
	if err := validatePlanSlots(plan); err != nil {
		t.Fatalf("distinct files at the same ordinal wrongly refused: %v", err)
	}
}

// #96: the guard compared the raw Rel/Heading strings while rowKey slugs both
// before joining them. "CLAUDE.md" and "CLAUDE!md" are two distinct slots to
// this guard but slug() maps both to "claude-md", so rowKey computes one key
// for both — the guard has to agree with rowKey about what "the same slot"
// means, or it admits exactly the collision it exists to catch. Mutating
// either block's Rel back to an identical string (removing the "!") turns
// this red for the wrong reason (same-hash dedup) rather than the right one.
func TestValidatePlanSlotsRefusesSlugCollidingSlots(t *testing.T) {
	plan := Plan{Blocks: []Block{
		{Hash: sha256Hex([]byte("legit")), Rel: "CLAUDE.md", Heading: "Alpha", Ordinal: 0, Body: "legit"},
		{Hash: sha256Hex([]byte("forged")), Rel: "CLAUDE!md", Heading: "Alpha", Ordinal: 0, Body: "forged"},
	}}
	err := validatePlanSlots(plan)
	if err == nil {
		t.Fatal("two blocks whose Rel differs only in a character slug() strips were accepted as distinct slots")
	}
	if !strings.Contains(err.Error(), "CLAUDE.md#Alpha") || !strings.Contains(err.Error(), "CLAUDE!md#Alpha") {
		t.Fatalf("error does not name both raw colliding slots: %v", err)
	}
}
