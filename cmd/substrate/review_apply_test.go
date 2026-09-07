package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/rest"
	"github.com/agentic-substrate/substrate/internal/store"
)

type reviewWorld struct {
	alice, bob, lead                    string
	orgID, teamAID, teamBID             string
	global, org, teamA, teamB, projectA string
	pathStr                             string
	aliceP, bobP, leadP                 *identity.Principal
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
	}
	w.global = ensureGlobal(t, conn)
	orgName := "rev-" + w.orgID[:8]
	w.pathStr = "global:/org:" + orgName + "/team:core/project:plotlens"
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
		($1, 'project', $2, 'plotlens', 0, 'placeholder', $3)`, w.projectA, w.teamA, w.teamAID)
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

func importBothMachines(t *testing.T, srv *httptest.Server, w reviewWorld) {
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
			"-server", srv.URL, "-token", "alice", "-scope", w.pathStr,
			planPath,
		}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Fatalf("apply %s: %v", machine, err)
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
	args := append([]string{
		"decide", id,
		"-server", srv.URL, "-token", "lead",
	}, extra...)
	stdout := &bytes.Buffer{}
	if err := reviewCmd(args, stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("review decide %s: %v\n%s", id, err, stdout.String())
	}
	return stdout.String()
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

func TestReviewTwoMachineImportResolvableViaCLI(t *testing.T) {
	// Closing the review_item without activating the chosen body is the
	// one-line change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, w)

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
	if tabs["active"] != 0 {
		t.Fatalf("losing Indent body is still active; %#v", tabs)
	}
	prod := countBodyStatus(t, conn, "instruction", "Use the production cluster.", w.projectA)
	stage := countBodyStatus(t, conn, "instruction", "Use the staging cluster.", w.projectA)
	if prod["active"] != 1 {
		t.Fatalf("wsl Deploy decision did not activate production cluster; %#v", prod)
	}
	if stage["active"] != 0 {
		t.Fatalf("losing Deploy body is still active; %#v", stage)
	}
}

func TestReviewKindFlipChangesStoredRow(t *testing.T) {
	// Ignoring as_kind and writing the classified kind is the one-line
	// change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, w)

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
	if pref["active"] != 0 {
		t.Fatalf("kind flip left Prefer tabs. active as a preference; %#v", pref)
	}
}

func TestReviewDecideDryRunPlanEqualsCommit(t *testing.T) {
	// Implementing dry-run as a second printer that does not share the
	// server apply path is the change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, w)

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

	committed := decideReview(t, srv, deployID,
		"-decision", "approved", "-reason", "wsl deploy", "-hostname", "wsl", "-commit")
	if strings.Contains(committed, "dry-run") {
		t.Fatalf("-commit still reported dry-run:\n%s", committed)
	}
	dryPlan := decidePlanLines(dry)
	commitPlan := decidePlanLines(committed)
	if dryPlan == "" || dryPlan != commitPlan {
		t.Fatalf("dry-run plan %q != commit plan %q\ndry:\n%s\ncommit:\n%s", dryPlan, commitPlan, dry, committed)
	}
}
