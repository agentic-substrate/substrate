package importer

import (
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/identity"
)

// #96: validatePlanSlots is the only refusal standing between an untrusted
// machine's plan and the proposed-row path — that path has no
// instruction_active_key (or any other) unique index behind it, so if the
// guard is fooled the write succeeds and two different bodies quietly share
// one key with no error at all: the resolver order, not the database, decides
// which one an operator ever sees. This proves the guard also stops a
// slug-colliding pair from an untrusted machine, not only a trusted/active
// one. Mutating either Rel back to being byte-identical (so both blocks are
// truly the same slot) turns this red for the wrong reason.
func TestApplyProposedPathRefusesSlugCollidingSlots(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	// "mac" imports first, so it becomes the trusted machine; "wsl" then
	// submits from an untrusted machine, which is what routes its rows to
	// proposed instead of active.
	seed := ApplyRequest{
		Plan:           ordinalPlan("mac", "seed rule.", 0),
		Machine:        "mac",
		TrustedMachine: "mac",
		Scope:          w.pathStr,
		Commit:         true,
	}
	if _, err := Apply(ctx, st, seed); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	plan := Plan{Blocks: []Block{
		{Hash: sha256Hex([]byte("legit")), Rel: "CLAUDE.md", Heading: "Alpha", Ordinal: 0, Body: "legit", Kind: "instruction", Sources: []Source{{Hostname: "wsl"}}},
		{Hash: sha256Hex([]byte("forged")), Rel: "CLAUDE!md", Heading: "Alpha", Ordinal: 0, Body: "forged", Kind: "instruction", Sources: []Source{{Hostname: "wsl"}}},
	}}
	req := ApplyRequest{
		Plan:           plan,
		Machine:        "wsl",
		TrustedMachine: "mac",
		Scope:          w.pathStr,
		Commit:         true,
	}
	_, err := Apply(ctx, st, req)
	if err == nil {
		t.Fatal("proposed-path apply admitted two slug-colliding slots with distinct bodies")
	}
	if got := err.Error(); !strings.Contains(got, "CLAUDE.md#Alpha") || !strings.Contains(got, "CLAUDE!md#Alpha") {
		t.Fatalf("error does not name both raw colliding slots: %v", err)
	}
}
