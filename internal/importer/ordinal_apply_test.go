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
		if _, err := Apply(ctx, st, witnessed(req)); err != nil {
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
		if _, err := Apply(ctx, st, witnessed(req)); err != nil {
			t.Fatalf("apply %q: %v", body, err)
		}
	}
	edit := ApplyRequest{Plan: ordinalPlan("wsl", "beta rule, reworded.", 1), Machine: "wsl", TrustedMachine: "mac", Scope: w.pathStr, Commit: true}
	if _, err := Apply(ctx, st, witnessed(edit)); err != nil {
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
		if _, err := Apply(ctx, st, witnessed(req)); err != nil {
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

// #93 broke the CLI on the shape no test carried: ONE plan holding N bullets
// from ONE machine. The other cases here apply single-block plans in sequence,
// and the guard's own unit tests never reach Apply, so the whole real path —
// BuildPlan → validatePlanSlots → planWrites → commit — went unexercised.
func TestApplyMultiBulletSingleMachineFileLandsEveryBullet(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	content := "## Gotchas\n\n- alpha rule.\n- beta rule.\n- gamma rule.\n"
	plan, err := BuildPlan([]Inventory{{
		Hostname: "mac",
		Files: []File{{
			Rel: ".claude/CLAUDE.md", Path: "/work/x/.claude/CLAUDE.md",
			DetectedType: "claude", Content: content,
		}},
	}}, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Blocks) != 3 {
		t.Fatalf("BuildPlan produced %d blocks, want 3", len(plan.Blocks))
	}

	res, err := Apply(ctx, st, witnessed(ApplyRequest{
		Plan: *plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	}))
	if err != nil {
		t.Fatalf("apply a single-machine multi-bullet plan: %v", err)
	}
	if len(res.Active) != 3 {
		t.Fatalf("apply reported %d active rows, want 3: %#v", len(res.Active), res.Active)
	}
	if len(res.Conflict) != 0 {
		t.Fatalf("single-machine bullets produced %d conflicts, want 0: %#v", len(res.Conflict), res.Conflict)
	}

	got := countByBodyStatus(t, conn, "instruction", w.project)
	for _, body := range []string{"alpha rule.", "beta rule.", "gamma rule."} {
		if got[body] != "active" {
			t.Fatalf("bullet %q status %q, want active; %#v", body, got[body], got)
		}
	}
	keys := map[string]string{}
	rows, err := conn.Query(t.Context(),
		`SELECT key, body FROM instruction WHERE scope_id = $1 AND key LIKE 'import.%'`, w.project)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, body string
		if err := rows.Scan(&key, &body); err != nil {
			t.Fatal(err)
		}
		if prev, dup := keys[key]; dup {
			t.Fatalf("bullets %q and %q collided on key %q", prev, body, key)
		}
		keys[key] = body
	}
	if len(keys) != 3 {
		t.Fatalf("stored %d imported rows, want 3: %#v", len(keys), keys)
	}

	var n int
	if err := conn.QueryRow(t.Context(),
		`SELECT count(*) FROM review_item WHERE kind = 'import_conflict' AND scope_id = $1`, w.project).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a multi-bullet single-machine import opened %d conflict reviews, want 0", n)
	}
}
