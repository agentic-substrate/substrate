package importer

import (
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/identity"
)

// One slot legitimately holds several active rows once bullets arrive across
// separate plans (validatePlanSlots only refuses distinct hashes inside ONE
// plan). Under hash keys those rows could never share a key, so pairing a
// proposed row against "any active row in this slot" was harmless. Under
// ordinal keys it is not: the slot is a row-per-bullet namespace, and only the
// row holding the same ordinal is the incoming block's predecessor.

func ordinalPlan(host, body string, ordinal int) Plan {
	return Plan{Blocks: []Block{{
		Hash: sha256Hex([]byte(body)), Heading: "Gotchas", Body: body,
		Kind: "instruction", Rel: ".claude/CLAUDE.md", Ordinal: ordinal,
		Sources: []Source{{Hostname: host}},
	}}}
}

func TestApplyNewOrdinalInSlotIsNotAConflict(t *testing.T) {
	// Prefix-matching the slot instead of the exact ordinal key is the
	// one-line change that makes this red: the third bullet is paired against
	// the first bullet's body and parked as a conflict it has no part in.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	for i, body := range []string{"alpha rule.", "beta rule.", "gamma rule."} {
		req := ApplyRequest{Plan: ordinalPlan("mac", body, i), Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true}
		if _, err := Apply(ctx, st, req); err != nil {
			t.Fatalf("apply %q: %v", body, err)
		}
	}

	got := countByBodyStatus(t, conn, "instruction", w.project)
	for _, body := range []string{"alpha rule.", "beta rule.", "gamma rule."} {
		if got[body] != "active" {
			t.Fatalf("bullet %q status %q, want active; %#v", body, got[body], got)
		}
	}
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM review_item WHERE kind = 'import_conflict' AND scope_id = $1`, w.project).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a fresh ordinal in an occupied slot opened %d conflict reviews, want 0", n)
	}
}

func TestApplyEditPairsAgainstItsOwnOrdinal(t *testing.T) {
	// Prefix-matching the slot is the one-line change that makes this red: the
	// edit to bullet 1 is paired against bullet 0's body, so the supersession
	// edge names the wrong rule (Gotcha 6) and GOV-2 answers with a rule the
	// edit never touched.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	for i, body := range []string{"alpha rule.", "beta rule."} {
		req := ApplyRequest{Plan: ordinalPlan("mac", body, i), Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true}
		if _, err := Apply(ctx, st, req); err != nil {
			t.Fatalf("apply %q: %v", body, err)
		}
	}
	edit := ApplyRequest{Plan: ordinalPlan("wsl", "beta rule, reworded.", 1), Machine: "wsl", TrustedMachine: "mac", Scope: w.pathStr, Commit: true}
	if _, err := Apply(ctx, st, edit); err != nil {
		t.Fatalf("apply edit: %v", err)
	}

	var raw []byte
	if err := conn.QueryRow(t.Context(),
		`SELECT payload FROM review_item WHERE kind = 'import_conflict' AND scope_id = $1`, w.project).Scan(&raw); err != nil {
		t.Fatalf("expected exactly one import_conflict review: %v", err)
	}
	payload := string(raw)
	if !strings.Contains(payload, "beta rule.") {
		t.Fatalf("conflict pair omits the predecessor at the same ordinal: %s", payload)
	}
	if strings.Contains(payload, "alpha rule.") {
		t.Fatalf("conflict pair names an unrelated sibling as the predecessor: %s", payload)
	}
}

func TestApplyStoresOrdinalDerivedKey(t *testing.T) {
	// Nothing else asserts that Ordinal survives planWrites into the stored
	// `key` column; rowKey's own unit test proves only the string builder.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	for i, body := range []string{"alpha rule.", "beta rule."} {
		req := ApplyRequest{Plan: ordinalPlan("mac", body, i), Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true}
		if _, err := Apply(ctx, st, req); err != nil {
			t.Fatalf("apply %q: %v", body, err)
		}
	}

	want := map[string]string{
		"alpha rule.": "import.instruction.claude-claude-md.gotchas.0",
		"beta rule.":  "import.instruction.claude-claude-md.gotchas.1",
	}
	for i, body := range []string{"alpha rule.", "beta rule."} {
		wantKey := want[body]
		if built := rowKey("instruction", ".claude/CLAUDE.md", "Gotchas", i); built != wantKey {
			t.Fatalf("rowKey built %q, want %q", built, wantKey)
		}
		var got string
		if err := conn.QueryRow(t.Context(),
			`SELECT key FROM instruction WHERE scope_id = $1 AND body = $2`, w.project, body).Scan(&got); err != nil {
			t.Fatalf("stored row for %q: %v", body, err)
		}
		if got != wantKey {
			t.Fatalf("stored key for %q is %q, want %q", body, got, wantKey)
		}
	}
}
