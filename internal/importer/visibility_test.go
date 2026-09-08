package importer

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
)

// visibilityByTable reads (body, visibility) straight from the table as the
// migration role, so a missing row is distinguishable from a row RLS hid.
// Scoped by scope_id: the package shares one migrated database across tests,
// so a whole-table read is order-dependent.
func visibilityByTable(t *testing.T, conn *pgx.Conn, table, scopeID string) map[string]string {
	t.Helper()
	var q string
	switch table {
	case "instruction":
		q = `SELECT body, visibility::text FROM instruction WHERE scope_id = $1`
	case "preference":
		q = `SELECT body, visibility::text FROM preference WHERE scope_id = $1`
	default:
		t.Fatalf("unknown table %s", table)
	}
	rows, err := conn.Query(t.Context(), q, scopeID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var body, vis string
		if err := rows.Scan(&body, &vis); err != nil {
			t.Fatal(err)
		}
		out[body] = vis
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestImportedPreferenceIsOwnerVisible(t *testing.T) {
	// Hardcoding store.VisibilityTeam on the preference insert is the
	// one-line production change that makes this red (#86): a personal
	// CLAUDE.md became team-readable the moment its owner onboarded.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	prefs := visibilityByTable(t, conn, "preference", w.team)
	if len(prefs) == 0 {
		t.Fatal("import wrote no preference rows; the visibility assertion below would be vacuous")
	}
	for body, vis := range prefs {
		if vis != "owner" {
			t.Fatalf("imported preference %q is %s-visible, want owner", body, vis)
		}
	}
	// Instructions stay team-visible: a project rule is not personal, and
	// asserting it here keeps the fix from being "set everything to owner".
	ins := visibilityByTable(t, conn, "instruction", w.project)
	if len(ins) == 0 {
		t.Fatal("import wrote no instruction rows")
	}
	for body, vis := range ins {
		if vis != "team" {
			t.Fatalf("imported instruction %q is %s-visible, want team", body, vis)
		}
	}
}

func TestImportPlanShowsScopeAndVisibilityBeforeCommit(t *testing.T) {
	// Dropping the scope/visibility columns from ApplyResult.Format is the
	// one-line production change that makes this red: the operator would
	// only learn where a row landed after --commit (#86).
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	res, err := Apply(ctx, st, applyReq(w, "mac", false))
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun {
		t.Fatal("dry-run result DryRun=false")
	}
	out := res.Format()
	teamPathStr := "global:/org:" + strings.Split(w.pathStr, "/org:")[1][:len("imp-")+8] + "/team:core"
	for _, want := range []string{
		"visibility=owner",
		"visibility=team",
		"scope=" + teamPathStr,
		"scope=" + w.pathStr,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry-run plan does not show %q:\n%s", want, out)
		}
	}
	// Every non-conflict row must be labelled: an unlabelled row is one the
	// operator cannot audit before committing it.
	for _, row := range append(append([]PlannedRow{}, res.Active...), res.Proposed...) {
		if row.Scope == "" || row.Visibility == "" {
			t.Fatalf("planned row missing scope/visibility: %#v", row)
		}
	}
}

// TestPlanPreviewMatchesCommittedVisibility pins the preview to the write:
// showing "visibility=owner" and then inserting team is the failure this
// guards against.
func TestPlanPreviewMatchesCommittedVisibility(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	dry, err := Apply(ctx, st, applyReq(w, "mac", false))
	if err != nil {
		t.Fatal(err)
	}
	planned := map[string]string{}
	for _, row := range append(append([]PlannedRow{}, dry.Active...), dry.Proposed...) {
		planned[row.Body] = row.Visibility
	}
	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	stored := map[string]string{}
	for body, vis := range visibilityByTable(t, conn, "preference", w.team) {
		stored[body] = vis
	}
	for body, vis := range visibilityByTable(t, conn, "instruction", w.project) {
		stored[body] = vis
	}
	for body, want := range planned {
		got, ok := stored[body]
		if !ok {
			continue // proposed-only rows are still written; missing means skipped
		}
		if got != want {
			t.Fatalf("row %q previewed as %s but was written %s", body, want, got)
		}
	}
	if len(planned) == 0 {
		t.Fatal("dry-run planned nothing; the comparison is vacuous")
	}
}
