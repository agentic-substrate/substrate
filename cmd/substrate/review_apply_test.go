package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/instruction"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/preference"
	"github.com/agentic-substrate/substrate/internal/rest"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

type reviewWorld struct {
	alice, bob, lead                              string
	orgID, teamAID, teamBID                       string
	global, org, teamA, teamB, projectA, projectB string
	pathStr, pathStrB                             string
	aliceP, bobP, leadP                           *identity.Principal
}

type nopGit struct{}

func (nopGit) Check(context.Context) error { return nil }

func seedReviewWorld(t *testing.T, conn *pgx.Conn) reviewWorld {
	t.Helper()
	id := func() string {
		t.Helper()
		var s string
		if err := conn.QueryRow(t.Context(), "SELECT gen_random_uuid()::text").Scan(&s); err != nil {
			t.Fatalf("gen_random_uuid: %v", err)
		}
		return s
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(t.Context(), q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	w := reviewWorld{
		alice:    id(),
		bob:      id(),
		lead:     id(),
		orgID:    id(),
		teamAID:  id(),
		teamBID:  id(),
		org:      id(),
		teamA:    id(),
		teamB:    id(),
		projectA: id(),
		projectB: id(),
	}
	w.global = ensureGlobal(t, conn)
	orgName := "rev-" + w.orgID[:8]
	w.pathStr = "global:/org:" + orgName + "/team:core/project:plotlens"
	w.pathStrB = "global:/org:" + orgName + "/team:other/project:otherapp"
	exec(`INSERT INTO principal (id, kind, display_name, trust) VALUES
		($1, 'user', 'alice', 'human'), ($2, 'user', 'bob', 'human'), ($3, 'user', 'lead', 'human')`,
		w.alice, w.bob, w.lead)
	exec(`INSERT INTO org (id, name) VALUES ($1, $2)`, w.orgID, orgName)
	exec(`INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'core'), ($3, $2, 'other')`,
		w.teamAID, w.orgID, w.teamBID)
	exec(`INSERT INTO membership (principal_id, team_id, role) VALUES
		($1, $2, 'member'), ($3, $4, 'member'), ($5, $2, 'lead')`,
		w.alice, w.teamAID, w.bob, w.teamBID, w.lead)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'org', $2, $3, 0, 'placeholder')`, w.org, w.global, orgName)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'team', $2, 'core', 0, 'placeholder', $3),
		($4, 'team', $2, 'other', 0, 'placeholder', $5)`,
		w.teamA, w.org, w.teamAID, w.teamB, w.teamBID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'project', $2, 'plotlens', 0, 'placeholder', $3),
		($4, 'project', $5, 'otherapp', 0, 'placeholder', $6)`,
		w.projectA, w.teamA, w.teamAID, w.projectB, w.teamB, w.teamBID)
	// Team-visible seed: a global-only fixture would pass with or without session GUCs.
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'constraint', 'ci.required', 'true', 'active', $2)`, w.projectA, w.alice)

	parse := func(s string) uuid.UUID {
		t.Helper()
		u, err := uuid.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	aliceID, bobID, leadID := parse(w.alice), parse(w.bob), parse(w.lead)
	orgID, teamA, teamB := parse(w.orgID), parse(w.teamAID), parse(w.teamBID)
	w.aliceP = &identity.Principal{
		ID: aliceID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamA},
	}
	w.bobP = &identity.Principal{
		ID: bobID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamB},
	}
	w.leadP = &identity.Principal{
		ID: leadID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamA},
	}
	return w
}

func serveReview(t *testing.T, dsn string, w reviewWorld) *httptest.Server {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	mem := memory.New(func() *store.Store { return st })
	h := rest.New(rest.Options{
		Store:  func() *store.Store { return st },
		Memory: mem,
		Git:    nopGit{},
		Now:    func() time.Time { return time.Date(2026, 9, 7, 18, 0, 0, 0, time.UTC) },
	})
	t.Cleanup(h.Close)
	mux := http.NewServeMux()
	h.Mount(mux)
	wrapped := identity.Middleware(func(_ context.Context, tok string) (*identity.Principal, error) {
		switch tok {
		case "alice":
			return w.aliceP, nil
		case "bob":
			return w.bobP, nil
		case "lead":
			return w.leadP, nil
		default:
			return nil, identity.ErrUnauthorized
		}
	})(mux)
	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)
	return srv
}

const (
	macCLAUDE = "" +
		"# Shared\n" +
		"Always run gofmt.\n" +
		"\n" +
		"# Indent\n" +
		"Prefer spaces.\n" +
		"\n" +
		"# Deploy\n" +
		"Use the staging cluster.\n"
	wslCLAUDE = "" +
		"# Shared\n" +
		"Always run gofmt.\n" +
		"\n" +
		"# Indent\n" +
		"Prefer tabs.\n" +
		"\n" +
		"# Deploy\n" +
		"Use the production cluster.\n"
)

func importBothMachines(t *testing.T, srv *httptest.Server, token, pathStr string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	tmp := t.TempDir()
	macHome := filepath.Join(tmp, "mac")
	wslHome := filepath.Join(tmp, "wsl")
	mustWriteCLI(t, filepath.Join(macHome, ".claude", "CLAUDE.md"), macCLAUDE)
	mustWriteCLI(t, filepath.Join(wslHome, ".claude", "CLAUDE.md"), wslCLAUDE)
	invMac := filepath.Join(tmp, "inv-mac.json")
	invWsl := filepath.Join(tmp, "inv-wsl.json")
	planPath := filepath.Join(tmp, "plan.json")
	if err := importCmd([]string{"scan", "-root", macHome, "-hostname", "mac", "-out", invMac}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("scan mac: %v", err)
	}
	if err := importCmd([]string{"scan", "-root", wslHome, "-hostname", "wsl", "-out", invWsl}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("scan wsl: %v", err)
	}
	if err := importCmd([]string{"plan", "-out", planPath, invMac, invWsl}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	apply := func(machine string) {
		t.Helper()
		if err := importCmd([]string{
			"apply", "-machine", machine, "-trusted", "mac", "-commit",
			"-server", srv.URL, "-token", token, "-scope", pathStr,
			planPath,
		}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Fatalf("apply %s as %s: %v", machine, token, err)
		}
	}
	apply("mac")
	apply("wsl")
}

func listReview(t *testing.T, srv *httptest.Server, token string) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	if err := reviewCmd([]string{"list", "-server", srv.URL, "-token", token}, stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("review list: %v", err)
	}
	return stdout.String()
}

func decideReview(t *testing.T, srv *httptest.Server, id string, extra ...string) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	if err := decideReviewCmd(t, srv, id, stdout, extra...); err != nil {
		t.Fatalf("review decide %s: %v\n%s", id, err, stdout.String())
	}
	return stdout.String()
}

func decideReviewCmd(t *testing.T, srv *httptest.Server, id string, stdout *bytes.Buffer, extra ...string) error {
	t.Helper()
	args := append([]string{
		"decide", id,
		"-server", srv.URL, "-token", "lead",
	}, extra...)
	return reviewCmd(args, stdout, &bytes.Buffer{})
}

func itemsBySlot(out string) map[string]string {
	m := map[string]string{}
	var id, slot string
	flush := func() {
		if id != "" && slot != "" {
			m[slot] = id
		}
		id, slot = "", ""
	}
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "id: "); ok {
			flush()
			id = strings.TrimSpace(v)
			continue
		}
		if v, ok := strings.CutPrefix(line, "slot: "); ok {
			slot = strings.TrimSpace(v)
		}
	}
	flush()
	return m
}

func decidePlanLines(out string) string {
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "activate:") || strings.HasPrefix(line, "retire:") {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

type dbDecideRow struct {
	Kind, Body, Status, ScopeID string
}

func listDecideRows(t *testing.T, conn *pgx.Conn, bodies, scopeIDs []string) []dbDecideRow {
	t.Helper()
	rows, err := conn.Query(t.Context(), `
		SELECT 'instruction', body, status::text, scope_id::text FROM instruction
		WHERE body = ANY($1) AND scope_id = ANY($2)
		UNION ALL
		SELECT 'preference', body, status::text, scope_id::text FROM preference
		WHERE body = ANY($1) AND scope_id = ANY($2)`, bodies, scopeIDs)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []dbDecideRow
	for rows.Next() {
		var r dbDecideRow
		if err := rows.Scan(&r.Kind, &r.Body, &r.Status, &r.ScopeID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func formatAppliedPlan(rows []dbDecideRow) string {
	var act, ret []string
	for _, r := range rows {
		line := r.Kind + " " + strings.ReplaceAll(r.Body, "\n", " ")
		switch r.Status {
		case "active":
			act = append(act, "activate: "+line)
		case "retired":
			ret = append(ret, "retire: "+line)
		}
	}
	sort.Strings(act)
	sort.Strings(ret)
	var b strings.Builder
	for _, line := range act {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range ret {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func filterPlanToExisting(plan string, rows []dbDecideRow) string {
	exist := map[string]bool{}
	for _, r := range rows {
		exist[r.Kind+" "+strings.ReplaceAll(r.Body, "\n", " ")] = true
	}
	var act, ret []string
	for _, line := range strings.Split(plan, "\n") {
		if v, ok := strings.CutPrefix(line, "activate: "); ok && exist[v] {
			act = append(act, line)
		}
		if v, ok := strings.CutPrefix(line, "retire: "); ok && exist[v] {
			ret = append(ret, line)
		}
	}
	sort.Strings(act)
	sort.Strings(ret)
	var b strings.Builder
	for _, line := range act {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range ret {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func countBodyStatus(t *testing.T, conn *pgx.Conn, table, body, scopeID string) map[string]int {
	t.Helper()
	var q string
	switch table {
	case "instruction":
		q = `SELECT status::text, count(*) FROM instruction WHERE body = $1 AND scope_id = $2 GROUP BY status`
	case "preference":
		q = `SELECT status::text, count(*) FROM preference WHERE body = $1 AND scope_id = $2 GROUP BY status`
	default:
		t.Fatalf("unknown table %s", table)
	}
	rows, err := conn.Query(t.Context(), q, body, scopeID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatal(err)
		}
		out[status] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func countBodyStatusAs(t *testing.T, dsn string, p *identity.Principal, table, body, scopeID string) map[string]int {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	var q string
	switch table {
	case "instruction":
		q = `SELECT status::text, count(*) FROM instruction WHERE body = $1 AND scope_id = $2 GROUP BY status`
	case "preference":
		q = `SELECT status::text, count(*) FROM preference WHERE body = $1 AND scope_id = $2 GROUP BY status`
	default:
		t.Fatalf("unknown table %s", table)
	}
	out := map[string]int{}
	ctx := identity.WithPrincipal(t.Context(), p)
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), q, body, scopeID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var status string
			var n int
			if err := rows.Scan(&status, &n); err != nil {
				return err
			}
			out[status] = n
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("count %s as principal: %v", table, err)
	}
	return out
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func countBodies(recs []instruction.Record, body string) int {
	n := 0
	for _, r := range recs {
		if r.Body == body {
			n++
		}
	}
	return n
}

func countPrefBodies(recs []preference.Record, body string) int {
	n := 0
	for _, r := range recs {
		if r.Body == body {
			n++
		}
	}
	return n
}

func TestReviewTwoMachineImportResolvableViaCLI(t *testing.T) {
	// Skipping the retire loop in commitReviewDecision is the one-line
	// production change that makes the loser-retired assertions red.
	// active==0 is already true while the loser is still proposed.
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	if strings.Contains(listed, "0 open review items") || !strings.Contains(listed, "id:") {
		t.Fatalf("lead saw an empty queue after a two-machine import (zero is also the missing-GUC outcome):\n%s", listed)
	}
	if !strings.Contains(listed, "wsl") || !strings.Contains(listed, "mac") {
		t.Fatalf("listing omitted a hostname:\n%s", listed)
	}
	if !strings.Contains(listed, ".claude/CLAUDE.md#Indent") || !strings.Contains(listed, ".claude/CLAUDE.md#Deploy") {
		t.Fatalf("listing omitted a slot:\n%s", listed)
	}
	if !strings.Contains(listed, "Prefer tabs.") || !strings.Contains(listed, "Prefer spaces.") {
		t.Fatalf("listing omitted an Indent body:\n%s", listed)
	}
	if !strings.Contains(listed, "Use the production cluster.") || !strings.Contains(listed, "Use the staging cluster.") {
		t.Fatalf("listing omitted a Deploy body:\n%s", listed)
	}
	bobList := listReview(t, srv, "bob")
	if strings.Contains(bobList, ".claude/CLAUDE.md#Indent") {
		t.Fatalf("bob (other team) saw team-A conflicts:\n%s", bobList)
	}

	bySlot := itemsBySlot(listed)
	indentID := bySlot[".claude/CLAUDE.md#Indent"]
	deployID := bySlot[".claude/CLAUDE.md#Deploy"]
	if indentID == "" || deployID == "" {
		t.Fatalf("missing slot ids: %#v\n%s", bySlot, listed)
	}

	decideReview(t, srv, indentID,
		"-decision", "approved", "-reason", "mac indent", "-hostname", "mac", "-commit")
	decideReview(t, srv, deployID,
		"-decision", "approved", "-reason", "wsl deploy", "-hostname", "wsl", "-commit")

	after := listReview(t, srv, "lead")
	if !strings.Contains(after, "0 open review items") {
		t.Fatalf("queue not empty after CLI decide:\n%s", after)
	}

	spaces := countBodyStatus(t, conn, "preference", "Prefer spaces.", w.teamA)
	tabs := countBodyStatus(t, conn, "preference", "Prefer tabs.", w.teamA)
	if spaces["active"] != 1 {
		t.Fatalf("mac Indent decision did not activate Prefer spaces.; status counts %#v", spaces)
	}
	if tabs["retired"] != 1 {
		t.Fatalf("losing Indent body is not retired; %#v", tabs)
	}
	prod := countBodyStatus(t, conn, "instruction", "Use the production cluster.", w.projectA)
	stage := countBodyStatus(t, conn, "instruction", "Use the staging cluster.", w.projectA)
	if prod["active"] != 1 {
		t.Fatalf("wsl Deploy decision did not activate production cluster; %#v", prod)
	}
	if stage["retired"] != 1 {
		t.Fatalf("losing Deploy body is not retired; %#v", stage)
	}
}

func TestReviewKindFlipChangesStoredRow(t *testing.T) {
	// Skipping the retire loop (leaving Prefer tabs. proposed as a
	// preference) is the one-line production change that makes the
	// retired-status assertion red. Resolve must show the flipped body
	// only as an instruction.
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	indentID := itemsBySlot(listed)[".claude/CLAUDE.md#Indent"]
	if indentID == "" {
		t.Fatalf("no Indent conflict in listing:\n%s", listed)
	}
	if !strings.Contains(listed, "(preference)") {
		t.Fatalf("Indent was not listed as preference before the flip:\n%s", listed)
	}

	decideReview(t, srv, indentID,
		"-decision", "approved", "-reason", "tabs, but it is an instruction",
		"-hostname", "wsl", "-as-kind", "instruction", "-commit")

	ins := countBodyStatus(t, conn, "instruction", "Prefer tabs.", w.projectA)
	pref := countBodyStatus(t, conn, "preference", "Prefer tabs.", w.teamA)
	if ins["active"] != 1 {
		t.Fatalf("kind flip did not store an active instruction; instruction %#v preference %#v", ins, pref)
	}
	if pref["retired"] != 1 {
		t.Fatalf("kind flip did not retire Prefer tabs. as a preference; %#v", pref)
	}

	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	path, err := scope.Parse(w.pathStr)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	ctx := identity.WithPrincipal(t.Context(), w.leadP)
	var recs []instruction.Record
	var prefs []preference.Record
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		recs, err = instruction.Resolve(ctx, tx, path)
		if err != nil {
			return err
		}
		prefs, _, err = preference.Resolve(ctx, tx, []uuid.UUID{mustUUID(t, w.teamA), mustUUID(t, w.org)}, instruction.Keys(recs))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := countBodies(recs, "Prefer tabs."); n != 1 {
		t.Fatalf("instruction.Resolve has %d Prefer tabs. rows, want 1", n)
	}
	if n := countPrefBodies(prefs, "Prefer tabs."); n != 0 {
		t.Fatalf("preference.Resolve still has Prefer tabs.; %#v", prefs)
	}
}

func TestReviewDecideDryRunPlanEqualsCommit(t *testing.T) {
	// Returning from commitReviewDecision after DecideReviewItem without
	// writing instruction/preference status is the one-line production
	// change that makes this red. Comparing two HTTP dumps stays green.
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	deployID := itemsBySlot(listed)[".claude/CLAUDE.md#Deploy"]
	if deployID == "" {
		t.Fatalf("no Deploy conflict in listing:\n%s", listed)
	}

	dry := decideReview(t, srv, deployID,
		"-decision", "approved", "-reason", "wsl deploy", "-hostname", "wsl")
	if !strings.Contains(dry, "dry-run") {
		t.Fatalf("decide without -commit printed no dry-run marker:\n%s", dry)
	}
	still := listReview(t, srv, "lead")
	if !strings.Contains(still, deployID) {
		t.Fatalf("dry-run decide closed the item:\n%s", still)
	}
	prod := countBodyStatus(t, conn, "instruction", "Use the production cluster.", w.projectA)
	if prod["active"] != 0 {
		t.Fatalf("dry-run activated a row; %#v", prod)
	}
	stage := countBodyStatus(t, conn, "instruction", "Use the staging cluster.", w.projectA)
	if stage["active"] != 0 {
		t.Fatalf("dry-run activated the losing Deploy body; %#v", stage)
	}

	committed := decideReview(t, srv, deployID,
		"-decision", "approved", "-reason", "wsl deploy", "-hostname", "wsl", "-commit")
	if strings.Contains(committed, "dry-run") {
		t.Fatalf("-commit still reported dry-run:\n%s", committed)
	}
	bodies := []string{"Use the production cluster.", "Use the staging cluster."}
	scopes := []string{w.projectA, w.teamA}
	rows := listDecideRows(t, conn, bodies, scopes)
	got := formatAppliedPlan(rows)
	want := filterPlanToExisting(decidePlanLines(dry), rows)
	if want == "" || got != want {
		t.Fatalf("post-commit rows %q != dry-run plan %q\ndry:\n%s\ncommit:\n%s\nrows: %#v", got, want, dry, committed, rows)
	}
}

func TestReviewDecideDoesNotActivateAnotherTeamsIdenticalBody(t *testing.T) {
	// Looking up instruction/preference rows by body alone is the one-line
	// production change that makes this red: team B imported the same
	// "Prefer spaces." block, and last-wins can activate B while A's item
	// is marked approved.
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)
	importBothMachines(t, srv, "bob", w.pathStrB)

	listed := listReview(t, srv, "lead")
	indentID := itemsBySlot(listed)[".claude/CLAUDE.md#Indent"]
	if indentID == "" {
		t.Fatalf("no Indent conflict in team-A listing:\n%s", listed)
	}
	bobListed := listReview(t, srv, "bob")
	if !strings.Contains(bobListed, ".claude/CLAUDE.md#Indent") {
		t.Fatalf("bob did not see team-B Indent after import:\n%s", bobListed)
	}

	decideReview(t, srv, indentID,
		"-decision", "approved", "-reason", "mac indent", "-hostname", "mac", "-commit")

	aliceSpaces := countBodyStatusAs(t, dsn, w.leadP, "preference", "Prefer spaces.", w.teamA)
	if aliceSpaces["active"] != 1 {
		t.Fatalf("team-A lead decide did not activate Prefer spaces. under the owning principal; %#v", aliceSpaces)
	}
	bobSpaces := countBodyStatusAs(t, dsn, w.bobP, "preference", "Prefer spaces.", w.teamB)
	if bobSpaces["proposed"] != 1 {
		t.Fatalf("team-B identical body was not still proposed under bob; %#v", bobSpaces)
	}
	if bobSpaces["active"] != 0 {
		t.Fatalf("team-A decide activated team-B's Prefer spaces.; %#v", bobSpaces)
	}
	bSuper := countBodyStatus(t, conn, "preference", "Prefer spaces.", w.teamB)
	if bSuper["proposed"] != 1 || bSuper["active"] != 0 {
		t.Fatalf("team-B Prefer spaces. status counts %#v, want proposed=1 active=0", bSuper)
	}
}

func TestReviewDecideMatchingNoRowFails(t *testing.T) {
	// Inserting a new row when the scoped lookup matches nothing is the
	// one-line production change that makes this red: the API would return
	// 200 and close the item even though no imported row was updated.
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	deployID := itemsBySlot(listed)[".claude/CLAUDE.md#Deploy"]
	if deployID == "" {
		t.Fatalf("no Deploy conflict in listing:\n%s", listed)
	}
	if _, err := conn.Exec(t.Context(),
		`UPDATE instruction SET body = body || ' (gone)' WHERE body IN ('Use the production cluster.', 'Use the staging cluster.')`); err != nil {
		t.Fatalf("rewrite bodies: %v", err)
	}
	if _, err := conn.Exec(t.Context(),
		`UPDATE preference SET body = body || ' (gone)' WHERE body IN ('Use the production cluster.', 'Use the staging cluster.')`); err != nil {
		t.Fatalf("rewrite preference bodies: %v", err)
	}

	err := decideReviewCmd(t, srv, deployID, &bytes.Buffer{},
		"-decision", "approved", "-reason", "wsl deploy", "-hostname", "wsl", "-commit")
	if err == nil {
		t.Fatal("decide matching no row returned success")
	}

	still := listReview(t, srv, "lead")
	if !strings.Contains(still, deployID) {
		t.Fatalf("no-row decide closed the item:\n%s", still)
	}
	prod := countBodyStatus(t, conn, "instruction", "Use the production cluster.", w.projectA)
	if prod["active"] != 0 {
		t.Fatalf("no-row decide inserted an active instruction; %#v", prod)
	}
}
