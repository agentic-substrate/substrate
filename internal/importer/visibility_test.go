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

// printedRow is one row as an operator reads it out of ApplyResult.Format.
type printedRow struct{ scope, visibility string }

// parsePlanRows re-reads Format's own output rather than the structs behind
// it. Reading PlannedRow.Visibility here would leave Format free to print
// anything at all — a hardcoded "visibility=owner" on every line kept the
// earlier version of this test green.
func parsePlanRows(t *testing.T, out string) map[string]printedRow {
	t.Helper()
	rows := map[string]printedRow{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "  skipped ") {
			continue
		}
		f := strings.Fields(line)
		// hostname status kind [scope=..] [visibility=..] key body...
		if len(f) < 4 {
			continue
		}
		var pr printedRow
		i := 3
		for ; i < len(f); i++ {
			switch {
			case strings.HasPrefix(f[i], "scope="):
				pr.scope = strings.TrimPrefix(f[i], "scope=")
			case strings.HasPrefix(f[i], "visibility="):
				pr.visibility = strings.TrimPrefix(f[i], "visibility=")
			default:
				goto key
			}
		}
	key:
		if i >= len(f) {
			continue
		}
		key := f[i]
		if !strings.HasPrefix(key, "import.") {
			continue
		}
		rows[key] = pr
	}
	return rows
}

// storedRows reads (key -> scope_id, visibility) as the migration role, so an
// owner-visible row is not hidden from the comparison.
func storedRows(t *testing.T, conn *pgx.Conn, table string, scopeIDs ...string) map[string]printedRow {
	t.Helper()
	var q string
	switch table {
	case "instruction":
		q = `SELECT key, scope_id::text, visibility::text FROM instruction WHERE scope_id = ANY($1)`
	case "preference":
		q = `SELECT key, scope_id::text, visibility::text FROM preference WHERE scope_id = ANY($1)`
	default:
		t.Fatalf("unknown table %s", table)
	}
	rows, err := conn.Query(t.Context(), q, scopeIDs)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]printedRow{}
	for rows.Next() {
		var key, scopeID, vis string
		if err := rows.Scan(&key, &scopeID, &vis); err != nil {
			t.Fatal(err)
		}
		out[key] = printedRow{scope: scopeID, visibility: vis}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPlanPreviewMatchesCommittedVisibility pins the printed plan to the
// write: showing "visibility=owner" and then inserting team is the failure
// this guards against, and AC3 of #86 is about what the operator reads before
// --commit, so the assertion has to run over Format's output.
//
// Hardcoding either half of Format's `scope=`/`visibility=` labels is the
// one-line production change that makes this red.
func TestPlanPreviewMatchesCommittedVisibility(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	dry, err := Apply(ctx, st, applyReq(w, "mac", false))
	if err != nil {
		t.Fatal(err)
	}
	printed := parsePlanRows(t, dry.Format())
	if len(printed) == 0 {
		t.Fatalf("plan output has no printed rows; the comparison would be vacuous:\n%s", dry.Format())
	}
	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	// scope_id -> the path Format prints for it, so a per-row scope swap is
	// caught and not just a per-row visibility swap.
	pathOf := map[string]string{
		w.team:    teamPathOf(t, w.pathStr),
		w.project: w.pathStr,
	}
	stored := map[string]printedRow{}
	for k, v := range storedRows(t, conn, "preference", w.team, w.project) {
		stored[k] = v
	}
	for k, v := range storedRows(t, conn, "instruction", w.team, w.project) {
		stored[k] = v
	}
	compared := 0
	for key, want := range printed {
		got, ok := stored[key]
		if !ok {
			continue // conflict rows carry no key and are never written as-is
		}
		compared++
		if got.visibility != want.visibility {
			t.Fatalf("row %s previewed visibility=%s but was written %s", key, want.visibility, got.visibility)
		}
		wantPath, ok := pathOf[got.scope]
		if !ok {
			t.Fatalf("row %s was written to unexpected scope %s", key, got.scope)
		}
		if want.scope != wantPath {
			t.Fatalf("row %s previewed scope=%s but was written to %s", key, want.scope, wantPath)
		}
	}
	if compared == 0 {
		t.Fatalf("no printed row matched a committed row; the comparison is vacuous\nprinted %#v\nstored %#v", printed, stored)
	}
}

// teamPathOf is the test's own reading of "the team scope of this path", kept
// independent of teamPath so a bug there cannot make the assertion agree with
// itself.
func teamPathOf(t *testing.T, pathStr string) string {
	t.Helper()
	i := strings.Index(pathStr, "/team:")
	if i < 0 {
		t.Fatalf("path %q has no team segment", pathStr)
	}
	rest := pathStr[i+len("/team:"):]
	if j := strings.Index(rest, "/"); j >= 0 {
		rest = rest[:j]
	}
	return pathStr[:i] + "/team:" + rest
}
