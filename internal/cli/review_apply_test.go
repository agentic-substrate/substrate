package cli

import (
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
	"github.com/agentic-substrate/substrate/internal/instruction"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/pgtest"
	"github.com/agentic-substrate/substrate/internal/preference"
	"github.com/agentic-substrate/substrate/internal/rest"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

// reviewWorld is two teams under one org, each with a member, plus a lead on
// team A. Two teams is the point: an invariant about tenancy cannot be stated
// against a single-team fixture.
type reviewWorld struct {
	alice, bob, lead                              string
	orgID, teamAID, teamBID                       string
	global, org, teamA, teamB, projectA, projectB string
	repo, repoKey                                 string
	pathStr, pathStrB                             string
	aliceP, bobP, leadP                           *identity.Principal
}

type nopGit struct{}

func (nopGit) Check(context.Context) error { return nil }

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
		repo:     id(),
	}
	w.global = pgtest.EnsureGlobal(t, conn)
	orgName := "rev-" + w.orgID[:8]
	w.pathStr = "global:/org:" + orgName + "/team:core/project:plotlens"
	w.pathStrB = "global:/org:" + orgName + "/team:other/project:otherapp"
	w.repoKey = "github.com/acme/plotlens-" + w.repo[:8]
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
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'repo', $2, $3, 0, 'placeholder', $4)`, w.repo, w.projectA, w.repoKey, w.teamAID)
	// Team-visible seed: a global-only fixture would pass with or without session GUCs.
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'constraint', 'ci.required', 'true', 'active', $2)`, w.projectA, w.alice)

	parse := func(s string) uuid.UUID {
		t.Helper()
		u, err := uuid.Parse(s)
		if err != nil {
			t.Fatalf("parse uuid %q: %v", s, err)
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

// serveReview mounts the real REST handler, so the CLI under test talks to
// the same code the control plane runs rather than to a fixture.
func serveReview(t *testing.T, dsn string, w reviewWorld) *httptest.Server {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
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

// importBothMachines scans two machines' harness files, plans them, and
// applies the plan twice through the real CLI, which is what queues the
// conflicts the review tests then decide.
func importBothMachines(t *testing.T, srv *httptest.Server, token, pathStr string) {
	t.Helper()
	tmp := t.TempDir()
	macHome := filepath.Join(tmp, "mac")
	wslHome := filepath.Join(tmp, "wsl")
	mustWriteCLI(t, filepath.Join(macHome, ".claude", "CLAUDE.md"), macCLAUDE)
	mustWriteCLI(t, filepath.Join(wslHome, ".claude", "CLAUDE.md"), wslCLAUDE)
	invMac := filepath.Join(tmp, "inv-mac.json")
	invWsl := filepath.Join(tmp, "inv-wsl.json")
	planPath := filepath.Join(tmp, "plan.json")
	d := Deps{HTTP: srv.Client()}
	if _, _, err := run(t, d, "import", "scan", "--root", macHome, "--hostname", "mac", "--out", invMac); err != nil {
		t.Fatalf("scan mac: %v", err)
	}
	if _, _, err := run(t, d, "import", "scan", "--root", wslHome, "--hostname", "wsl", "--out", invWsl); err != nil {
		t.Fatalf("scan wsl: %v", err)
	}
	if _, _, err := run(t, d, "import", "plan", "--out", planPath, invMac, invWsl); err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, machine := range []string{"mac", "wsl"} {
		if _, _, err := run(t, d, "import", "apply", planPath,
			"--machine", machine, "--trusted", "mac",
			"--server", srv.URL, "--token", token, "--scope", pathStr, "--yes"); err != nil {
			t.Fatalf("apply %s as %s: %v", machine, token, err)
		}
	}
}

func listReview(t *testing.T, srv *httptest.Server, token string) string {
	t.Helper()
	out, _, err := run(t, Deps{HTTP: srv.Client()}, "review", "list", "--server", srv.URL, "--token", token)
	if err != nil {
		t.Fatalf("review list: %v", err)
	}
	return out
}

// approveReview drives `review approve` as the team-A lead, which is the verb
// that replaced `decide -decision approved -commit`.
func approveReview(t *testing.T, srv *httptest.Server, id string, extra ...string) string {
	t.Helper()
	args := append([]string{"review", "approve", id, "--server", srv.URL, "--token", "lead", "--yes"}, extra...)
	out, _, err := run(t, Deps{HTTP: srv.Client()}, args...)
	if err != nil {
		t.Fatalf("review approve %s: %v\n%s", id, err, out)
	}
	return out
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

// countBodyStatus reads as the migration role, which sees every row: it
// distinguishes "the row is not active" from "RLS hid it".
func countBodyStatus(t *testing.T, conn *pgx.Conn, table, body, scopeID string) map[string]int {
	t.Helper()
	out := map[string]int{}
	rows, err := conn.Query(t.Context(), bodyStatusQuery(t, table), body, scopeID)
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatalf("scan %s count: %v", table, err)
		}
		out[status] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return out
}

// countBodyStatusAs reads under one principal's session settings, which is
// where owner visibility either hides a row or does not.
func countBodyStatusAs(t *testing.T, dsn string, p *identity.Principal, table, body, scopeID string) map[string]int {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(st.Close)
	out := map[string]int{}
	q := bodyStatusQuery(t, table)
	ctx := identity.WithPrincipal(t.Context(), p)
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, q, body, scopeID)
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

func bodyStatusQuery(t *testing.T, table string) string {
	t.Helper()
	switch table {
	case "instruction":
		return `SELECT status::text, count(*) FROM instruction WHERE body = $1 AND scope_id = $2 GROUP BY status`
	case "preference":
		return `SELECT status::text, count(*) FROM preference WHERE body = $1 AND scope_id = $2 GROUP BY status`
	default:
		t.Fatalf("unknown table %s", table)
		return ""
	}
}

func rowColumnForBody(t *testing.T, conn *pgx.Conn, table, column, body, scopeID string) string {
	t.Helper()
	var q string
	switch table {
	case "instruction":
		q = `SELECT ` + column + `::text FROM instruction WHERE body = $1 AND scope_id = $2`
	case "preference":
		q = `SELECT ` + column + `::text FROM preference WHERE body = $1 AND scope_id = $2`
	default:
		t.Fatalf("unknown table %s", table)
	}
	var got string
	if err := conn.QueryRow(t.Context(), q, body, scopeID).Scan(&got); err != nil {
		t.Fatalf("%s.%s for %q: %v", table, column, body, err)
	}
	return got
}

type resolvedRows struct {
	ins   []instruction.Record
	prefs []preference.Record
}

// resolveAs runs the real resolution path under one principal's session
// settings.
func resolveAs(t *testing.T, dsn string, p *identity.Principal, pathStr string, overlay []string) resolvedRows {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(st.Close)
	path, err := scope.Parse(pathStr)
	if err != nil {
		t.Fatalf("scope.Parse: %v", err)
	}
	ids := make([]uuid.UUID, len(overlay))
	for i, s := range overlay {
		u, err := uuid.Parse(s)
		if err != nil {
			t.Fatalf("parse overlay uuid: %v", err)
		}
		ids[i] = u
	}
	var out resolvedRows
	ctx := identity.WithPrincipal(t.Context(), p)
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		out.ins, err = instruction.Resolve(ctx, tx, path)
		if err != nil {
			return err
		}
		out.prefs, _, err = preference.Resolve(ctx, tx, ids, instruction.Keys(out.ins))
		return err
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return out
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

// Team B imported the identical "Prefer spaces." block. Team A's lead
// approving their own conflict must not activate team B's row: a tenancy leak
// here is invisible in every single-team fixture.
// Mutation that turns this red: looking instruction/preference rows up by
// body alone in the decide commit, so last-wins activates B while A's item is
// merely marked approved.
func TestReviewApproveDoesNotActivateAnotherTeamsIdenticalBody(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
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

	approveReview(t, srv, indentID, "--reason", "mac indent", "--hostname", "mac")

	// Read back as alice, not as the deciding lead: an imported preference is
	// owner-visible (#86), so the lead's own session cannot see the row it
	// just activated.
	aliceSpaces := countBodyStatusAs(t, dsn, w.aliceP, "preference", "Prefer spaces.", w.teamA)
	if aliceSpaces["active"] != 1 {
		t.Fatalf("team-A approve did not activate Prefer spaces. under the owning principal; %#v", aliceSpaces)
	}
	bobSpaces := countBodyStatusAs(t, dsn, w.bobP, "preference", "Prefer spaces.", w.teamB)
	if bobSpaces["proposed"] != 1 {
		t.Fatalf("team-B identical body was not still proposed under bob; %#v", bobSpaces)
	}
	if bobSpaces["active"] != 0 {
		t.Fatalf("team-A approve activated team-B's Prefer spaces.; %#v", bobSpaces)
	}
	bSuper := countBodyStatus(t, conn, "preference", "Prefer spaces.", w.teamB)
	if bSuper["proposed"] != 1 || bSuper["active"] != 0 {
		t.Fatalf("team-B Prefer spaces. status counts %#v, want proposed=1 active=0", bSuper)
	}
}

// #80 restated for the review path: the lead flipping alice's imported
// preference cannot see it under RLS, so the key had to come from somewhere
// other than the blocked lookup. A content hash is the wrong answer -- a
// hash-keyed row can never be superseded, corrected, or matched by a future
// import.
// Mutation that turns this red: restoring decideKey's
// `"import." + asKind + ".review." + hash` fallback.
func TestReviewKindFlipKeepsTheImportedSlotKey(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	indentID := itemsBySlot(listed)[".claude/CLAUDE.md#Indent"]
	if indentID == "" {
		t.Fatalf("no Indent conflict in listing:\n%s", listed)
	}

	approveReview(t, srv, indentID,
		"--reason", "tabs, but it is an instruction",
		"--hostname", "wsl", "--as-kind", "instruction")

	prefKey := rowColumnForBody(t, conn, "preference", "key", "Prefer tabs.", w.teamA)
	insKey := rowColumnForBody(t, conn, "instruction", "key", "Prefer tabs.", w.projectA)
	if !strings.HasPrefix(prefKey, "import.preference.") {
		t.Fatalf("imported preference key %q is not a slot key; the comparison below would be vacuous", prefKey)
	}
	want := "import.instruction." + strings.TrimPrefix(prefKey, "import.preference.")
	if insKey != want {
		t.Fatalf("kind flip keyed the instruction %q, want the imported slot key %q", insKey, want)
	}
	if strings.Contains(insKey, ".review.") {
		t.Fatalf("kind flip minted a content-hash key %q (#80)", insKey)
	}
}

// The other half of the flip: an instruction flipped to a preference is a
// personal row and must not be filed team-readable, which is the invariant
// #86 exists to establish.
// Mutation that turns this red: restoring store.VisibilityTeam on the
// InsertPreference in the review decide commit.
func TestReviewConflictPreferenceInsertIsOwnerVisible(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	deployID := itemsBySlot(listed)[".claude/CLAUDE.md#Deploy"]
	if deployID == "" {
		t.Fatalf("no Deploy conflict in listing:\n%s", listed)
	}

	approveReview(t, srv, deployID,
		"--reason", "staging, and it is a preference",
		"--hostname", "mac", "--as-kind", "preference")

	body := "Use the staging cluster."
	if got := countBodyStatus(t, conn, "preference", body, w.teamA); got["active"] != 1 {
		t.Fatalf("flip to preference did not activate %q; %#v", body, got)
	}
	if vis := rowColumnForBody(t, conn, "preference", "visibility", body, w.teamA); vis != "owner" {
		t.Fatalf("review conflict wrote preference %q %s-visible, want owner", body, vis)
	}
}

// The end-to-end statement of #86 through review: a personal preference
// imported from ~/.claude/CLAUDE.md is approvable, resolves for its owner, and
// is not readable by another member of the same team. internal/importer keeps
// only the importer half of this; the "another team member cannot read it"
// half lives here.
// Mutation that turns this red: restoring store.VisibilityTeam on the
// InsertPreference in commitWrites, which makes the lead-cannot-see assertion
// fail.
func TestImportedPreferenceStaysOwnerVisibleThroughReview(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	indentID := itemsBySlot(listed)[".claude/CLAUDE.md#Indent"]
	if indentID == "" {
		t.Fatalf("no Indent conflict in listing:\n%s", listed)
	}
	// The review path is the acceptance criterion, not a detail: an
	// owner-visible row must still be reachable by the decide apply.
	approveReview(t, srv, indentID, "--reason", "mac indent", "--hostname", "mac")

	if vis := rowColumnForBody(t, conn, "preference", "visibility", "Prefer spaces.", w.teamA); vis != "owner" {
		t.Fatalf("imported preference is %s-visible, want owner", vis)
	}
	if status := rowColumnForBody(t, conn, "preference", "status", "Prefer spaces.", w.teamA); status != "active" {
		t.Fatalf("review approve left the owner-visible preference %s, want active", status)
	}

	overlay := []string{w.teamA, w.org}
	owner := resolveAs(t, dsn, w.aliceP, w.pathStr, overlay)
	if countPrefBodies(owner.prefs, "Prefer spaces.") != 1 {
		t.Fatalf("owner cannot resolve their own imported preference; %#v", owner.prefs)
	}

	other := resolveAs(t, dsn, w.leadP, w.pathStr, overlay)
	if countPrefBodies(other.prefs, "Prefer spaces.") != 0 {
		t.Fatalf("another principal on the same team resolved alice's personal preference; %#v", other.prefs)
	}
	// Distinguishes "RLS restricted the read" from "the row was never there":
	// the same session must still see the team-visible rows.
	if countBodies(other.ins, "Always run gofmt.") != 1 {
		t.Fatalf("lead lost the team-visible imported instruction too, so the test proves nothing about RLS; %#v", other.ins)
	}
	if countBodies(other.ins, "true") != 1 {
		t.Fatalf("lead lost the seeded team-visible ci.required row; %#v", other.ins)
	}
}

// The CLI's repo key and the merged POST /v1/import handler agree end to end:
// a checkout with no --scope sends `repo`, the server binds it to the chain
// itself (R18), the rows land at the repo scope, and the response never
// discloses the chain. Asserting the wire body alone could not see any of
// that; this is the first time both halves exist in one tree.
// Mutation that turns this red: sending `scope` instead of `repo` from
// import.go, or renaming either field on either side.
func TestImportApplyRepoKeyReachesTheMergedHandler(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)

	checkout := gitCheckout(t, "https://"+w.repoKey+".git")
	tmp := t.TempDir()
	home := filepath.Join(tmp, "mac")
	mustWriteCLI(t, filepath.Join(home, ".claude", "CLAUDE.md"), macCLAUDE)
	inv := filepath.Join(tmp, "inv.json")
	planPath := filepath.Join(tmp, "plan.json")
	d := Deps{HTTP: srv.Client(), Getwd: func() (string, error) { return checkout, nil }}
	if _, _, err := run(t, d, "import", "scan", "--root", home, "--hostname", "mac", "--out", inv); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if _, _, err := run(t, d, "import", "plan", "--out", planPath, inv); err != nil {
		t.Fatalf("plan: %v", err)
	}
	out, _, err := run(t, d, "import", "apply", planPath,
		"--machine", "mac", "--trusted", "mac",
		"--server", srv.URL, "--token", "alice", "--yes")
	if err != nil {
		t.Fatalf("repo-keyed import apply: %v", err)
	}
	if !strings.Contains(out, "repo: "+w.repoKey) {
		t.Fatalf("the echo must name the repo key it sent, got:\n%s", out)
	}
	// The chain is exactly what a repo-keyed caller must never be handed back.
	for _, secret := range []string{w.pathStr, "team:core", "project:plotlens"} {
		if strings.Contains(out, secret) {
			t.Fatalf("the response leaked the resolved chain %q:\n%s", secret, out)
		}
	}

	var scopeID string
	if err := conn.QueryRow(t.Context(),
		`SELECT scope_id::text FROM instruction WHERE body = 'Always run gofmt.' AND created_by = $1`,
		w.alice).Scan(&scopeID); err != nil {
		t.Fatalf("read back the imported instruction: %v", err)
	}
	if scopeID != w.repo {
		t.Fatalf("import landed at scope %s, want the repo scope %s", scopeID, w.repo)
	}
}
