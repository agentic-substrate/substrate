package importer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/store"
)

type importWorld struct {
	actor, bob, orgID, teamID, teamBID string
	global, org, team, teamB, project  string
	userScope, pathStr                 string
	aliceP, bobP                       *identity.Principal
}

func seedImportWorld(t *testing.T, conn *pgx.Conn) importWorld {
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
	w := importWorld{
		actor:     id(),
		bob:       id(),
		orgID:     id(),
		teamID:    id(),
		teamBID:   id(),
		org:       id(),
		team:      id(),
		teamB:     id(),
		project:   id(),
		userScope: id(),
	}
	w.global = ensureGlobal(t, conn)
	orgName := "imp-" + w.orgID[:8]
	w.pathStr = "global:/org:" + orgName + "/team:core/project:plotlens"
	exec(`INSERT INTO principal (id, kind, display_name, trust) VALUES
		($1, 'user', 'alice', 'human'), ($2, 'user', 'bob', 'human')`, w.actor, w.bob)
	exec(`INSERT INTO org (id, name) VALUES ($1, $2)`, w.orgID, orgName)
	exec(`INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'core'), ($3, $2, 'other')`,
		w.teamID, w.orgID, w.teamBID)
	exec(`INSERT INTO membership (principal_id, team_id, role) VALUES
		($1, $2, 'member'), ($3, $4, 'member')`, w.actor, w.teamID, w.bob, w.teamBID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'org', $2, $3, 0, 'placeholder')`, w.org, w.global, orgName)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'team', $2, 'core', 0, 'placeholder', $3),
		($4, 'team', $2, 'other', 0, 'placeholder', $5)`,
		w.team, w.org, w.teamID, w.teamB, w.teamBID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'project', $2, 'plotlens', 0, 'placeholder', $3)`, w.project, w.team, w.teamID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'user', NULL, $2, 0, 'placeholder')`, w.userScope, w.actor)
	// Team-visible seed: a global-only fixture would pass with or without session GUCs.
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'constraint', 'ci.required', 'true', 'active', $2)`, w.project, w.actor)

	parse := func(s string) uuid.UUID {
		t.Helper()
		u, err := uuid.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	w.aliceP = &identity.Principal{
		ID: parse(w.actor), Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: parse(w.orgID), TeamIDs: []uuid.UUID{parse(w.teamID)},
	}
	w.bobP = &identity.Principal{
		ID: parse(w.bob), Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: parse(w.orgID), TeamIDs: []uuid.UUID{parse(w.teamBID)},
	}
	return w
}

func twoMachinePlan() Plan {
	shared := "Always run gofmt."
	tabs := "Prefer tabs."
	spaces := "Prefer spaces."
	wslOnly := "Use modules."
	return Plan{
		Blocks: []Block{
			{
				Hash: sha256Hex([]byte(shared)), Heading: "Shared", Body: shared,
				Kind: "instruction", ImpliedScope: "user", Rel: ".claude/CLAUDE.md",
				Sources: []Source{{Hostname: "mac"}, {Hostname: "wsl"}},
			},
			{
				Hash: sha256Hex([]byte(wslOnly)), Heading: "Modules", Body: wslOnly,
				Kind: "instruction", ImpliedScope: "user", Rel: ".claude/CLAUDE.md",
				Sources: []Source{{Hostname: "wsl"}},
			},
		},
		Conflicts: []Conflict{{
			Slot: ".claude/CLAUDE.md#Indent",
			Pair: []ConflictSide{
				{Hash: sha256Hex([]byte(tabs)), Hostnames: []string{"wsl"}, Body: tabs},
				{Hash: sha256Hex([]byte(spaces)), Hostnames: []string{"mac"}, Body: spaces},
			},
		}},
		Memories: []MemoryItem{
			{
				Hash: sha256Hex([]byte("the cluster is up")), Title: "cluster",
				Body: "the cluster is up", Kind: "fact", Hostname: "mac", SourceStatus: "confirmed",
			},
			{
				Hash: sha256Hex([]byte("wsl saw a flake")), Title: "flake",
				Body: "wsl saw a flake", Kind: "incident", Hostname: "wsl", SourceStatus: "confirmed",
			},
		},
	}
}

func applyReq(w importWorld, machine string, commit bool) ApplyRequest {
	return ApplyRequest{
		Plan:           twoMachinePlan(),
		Machine:        machine,
		TrustedMachine: "mac",
		Scope:          w.pathStr,
		Commit:         commit,
	}
}

func openStore(t *testing.T, dsn string) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func countByBodyStatus(t *testing.T, conn *pgx.Conn, table, scopeID string) map[string]string {
	t.Helper()
	var q string
	switch table {
	case "instruction":
		q = `SELECT body, status::text FROM instruction WHERE scope_id = $1`
	case "preference":
		q = `SELECT body, status::text FROM preference WHERE scope_id = $1`
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
		var body, status string
		if err := rows.Scan(&body, &status); err != nil {
			t.Fatal(err)
		}
		out[body] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func tableCounts(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var ins, pref, mem, rev, rec int
	q := func(sql string, dest *int) {
		t.Helper()
		if err := conn.QueryRow(t.Context(), sql).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	q(`SELECT count(*) FROM instruction`, &ins)
	q(`SELECT count(*) FROM preference`, &pref)
	q(`SELECT count(*) FROM memory`, &mem)
	q(`SELECT count(*) FROM review_item`, &rev)
	q(`SELECT count(*) FROM ingest_receipt`, &rec)
	return strings.Join([]string{
		"instruction=" + itoa(ins),
		"preference=" + itoa(pref),
		"memory=" + itoa(mem),
		"review_item=" + itoa(rev),
		"ingest_receipt=" + itoa(rec),
	}, " ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestApplyTrustedMachineNonConflictBecomesActive(t *testing.T) {
	// Treating every imported row as proposed, including the trusted machine's
	// unique blocks, is the one-line change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	res, err := Apply(ctx, st, applyReq(w, "mac", true))
	if err != nil {
		t.Fatal(err)
	}
	if res.DryRun {
		t.Fatal("commit run reported dry_run")
	}
	got := countByBodyStatus(t, conn, "instruction", w.project)
	if got["Always run gofmt."] != "active" {
		t.Fatalf("shared trusted block status %q, want active; row map %#v", got["Always run gofmt."], got)
	}
	var n int
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.bobP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM instruction WHERE body = 'Always run gofmt.' AND scope_id = $1`, w.project).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("bob (other team) saw team-visible imported instruction; RLS session settings were not applied")
	}
	prefs := countByBodyStatus(t, conn, "preference", w.team)
	if prefs["Prefer spaces."] != "proposed" {
		t.Fatalf("trusted conflict side status %q, want proposed; %#v", prefs["Prefer spaces."], prefs)
	}
}

func TestApplySecondMachineCreatesZeroNewActive(t *testing.T) {
	// Dropping `req.Machine == req.TrustedMachine` so the later machine can
	// INSERT status=active is the one-line change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	before := countByBodyStatus(t, conn, "instruction", w.project)
	beforePref := countByBodyStatus(t, conn, "preference", w.team)
	activeBefore := map[string]bool{}
	for body, st := range before {
		if st == "active" {
			activeBefore[body] = true
		}
	}
	for body, st := range beforePref {
		if st == "active" {
			activeBefore[body] = true
		}
	}

	if _, err := Apply(ctx, st, applyReq(w, "wsl", true)); err != nil {
		t.Fatal(err)
	}

	afterIns := countByBodyStatus(t, conn, "instruction", w.project)
	afterPref := countByBodyStatus(t, conn, "preference", w.team)
	var newActive []string
	for body, st := range afterIns {
		if st == "active" && !activeBefore[body] {
			newActive = append(newActive, "instruction:"+body)
		}
	}
	for body, st := range afterPref {
		if st == "active" && !activeBefore[body] {
			newActive = append(newActive, "preference:"+body)
		}
	}
	if len(newActive) != 0 {
		t.Fatalf("second machine created active rows it did not already agree with: %v\nins %#v\npref %#v", newActive, afterIns, afterPref)
	}
	if afterIns["Use modules."] != "proposed" {
		t.Fatalf("wsl-only block status %q, want proposed", afterIns["Use modules."])
	}
	if afterPref["Prefer tabs."] != "proposed" {
		t.Fatalf("wsl conflict side status %q, want proposed", afterPref["Prefer tabs."])
	}
	if afterIns["Always run gofmt."] != "active" {
		t.Fatalf("agreed block lost active status: %q", afterIns["Always run gofmt."])
	}
}

func TestApplyConflictCarriesHostname(t *testing.T) {
	// Omitting hostname from the import_conflict payload is the one-line
	// change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, st, applyReq(w, "wsl", true)); err != nil {
		t.Fatal(err)
	}

	rows, err := conn.Query(t.Context(), `SELECT payload FROM review_item WHERE kind = 'import_conflict' AND team_id = $1`, w.teamID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		n++
		if !strings.Contains(string(raw), `"hostname"`) {
			t.Fatalf("import_conflict payload missing hostname: %s", raw)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		host, _ := payload["hostname"].(string)
		if host == "" {
			t.Fatalf("import_conflict hostname empty: %s", raw)
		}
		if !strings.Contains(string(raw), "wsl") && !strings.Contains(string(raw), "mac") {
			t.Fatalf("import_conflict payload has no machine name: %s", raw)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no import_conflict review items; the queue is empty")
	}
}

func TestApplyImportedMemoryNeverConfirmed(t *testing.T) {
	// Passing SourceStatus through to memory.status is the one-line change
	// that makes this red. The source asserts confirmed; Gotcha 4 forbids it.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}

	var status, tier string
	err := conn.QueryRow(t.Context(), `SELECT status::text, tier::text FROM memory WHERE title = 'cluster' AND scope_id = $1`, w.project).Scan(&status, &tier)
	if err != nil {
		t.Fatal(err)
	}
	if status == "confirmed" || status != "unverified" {
		t.Fatalf("imported memory status %q, want unverified (source claimed confirmed)", status)
	}
	if tier != "episodic" {
		t.Fatalf("imported memory tier %q, want episodic", tier)
	}
}

func TestApplyDryRunWritesNothingAndPrints(t *testing.T) {
	// Setting Commit=true inside Apply regardless of req.Commit, or returning
	// an empty Format(), is the one-line change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	before := tableCounts(t, conn)
	res, err := Apply(ctx, st, applyReq(w, "mac", false))
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun {
		t.Fatal("dry-run result DryRun=false")
	}
	out := res.Format()
	if strings.TrimSpace(out) == "" {
		t.Fatal("dry-run produced empty output; a no-op implementation would also look read-only")
	}
	if !strings.Contains(out, "mac") {
		t.Fatalf("dry-run output missing hostname:\n%s", out)
	}
	after := tableCounts(t, conn)
	if after != before {
		t.Fatalf("dry-run wrote to the database\nbefore %s\nafter  %s", before, after)
	}
}

func TestApplyDryRunMatchesCommit(t *testing.T) {
	// A second Decide/Apply implementation for commit that ignores Preview is
	// the change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	dry, err := Apply(ctx, st, applyReq(w, "mac", false))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Apply(ctx, st, applyReq(w, "mac", true))
	if err != nil {
		t.Fatal(err)
	}
	if drySet(dry) != drySet(got) {
		t.Fatalf("dry-run planned set %q != commit applied set %q", drySet(dry), drySet(got))
	}
}

func TestApplyReplayDoesNotDuplicate(t *testing.T) {
	// Inserting without checking ingest_receipt is the one-line change that
	// makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	before := tableCounts(t, conn)
	res, err := Apply(ctx, st, applyReq(w, "mac", true))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Duplicate {
		t.Fatal("replay Duplicate=false; a re-run must not insert again")
	}
	after := tableCounts(t, conn)
	if after != before {
		t.Fatalf("replay duplicated rows\nbefore %s\nafter  %s", before, after)
	}
}

func TestApplyUntrustedFirstIsRefused(t *testing.T) {
	// Removing the trusted-machine-first gate is the one-line change that
	// makes this red. Dry-run must surface the same error.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	_, err := Apply(ctx, st, applyReq(w, "wsl", false))
	if err == nil || !strings.Contains(err.Error(), "trusted") {
		t.Fatalf("untrusted-first dry-run: %v, want error naming trusted", err)
	}
	_, err = Apply(ctx, st, applyReq(w, "wsl", true))
	if err == nil || !strings.Contains(err.Error(), "trusted") {
		t.Fatalf("untrusted-first commit: %v, want error naming trusted", err)
	}
	got := countByBodyStatus(t, conn, "instruction", w.project)
	if _, ok := got["Use modules."]; ok {
		t.Fatal("untrusted-first still wrote the later machine's block")
	}
}

func TestApplySecondMachineDoesNotMutateTrustedRow(t *testing.T) {
	// An UPDATE of the trusted active row on the later apply is the change
	// that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	var id, body, status string
	if err := conn.QueryRow(t.Context(), `SELECT id::text, body, status::text FROM instruction WHERE body = 'Always run gofmt.' AND scope_id = $1`, w.project).Scan(&id, &body, &status); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, st, applyReq(w, "wsl", true)); err != nil {
		t.Fatal(err)
	}
	var id2, body2, status2 string
	if err := conn.QueryRow(t.Context(), `SELECT id::text, body, status::text FROM instruction WHERE id = $1::uuid`, id).Scan(&id2, &body2, &status2); err != nil {
		t.Fatal(err)
	}
	if id2 != id || body2 != body || status2 != status {
		t.Fatalf("trusted row mutated: id %s→%s body %q→%q status %s→%s", id, id2, body, body2, status, status2)
	}
}

func drySet(r *ApplyResult) string {
	type item struct{ H, K, S, Hash string }
	var items []item
	add := func(rows []PlannedRow) {
		for _, row := range rows {
			items = append(items, item{H: row.Hostname, K: row.Kind, S: row.Status, Hash: row.Hash})
		}
	}
	add(r.Active)
	add(r.Proposed)
	add(r.Conflict)
	add(r.Memory)
	raw, _ := json.Marshal(items)
	return string(raw)
}
