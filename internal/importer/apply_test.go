package importer

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/store"
)

type importWorld struct {
	actor, bob, carol, orgID, teamID, teamBID string
	global, org, team, teamB, project         string
	userScope, pathStr                        string
	aliceP, bobP, carolP                      *identity.Principal
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
		carol:     id(),
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
		($1, 'user', 'alice', 'human'), ($2, 'user', 'bob', 'human'), ($3, 'user', 'carol', 'human')`,
		w.actor, w.bob, w.carol)
	exec(`INSERT INTO org (id, name) VALUES ($1, $2)`, w.orgID, orgName)
	exec(`INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'core'), ($3, $2, 'other')`,
		w.teamID, w.orgID, w.teamBID)
	exec(`INSERT INTO membership (principal_id, team_id, role) VALUES
		($1, $2, 'member'), ($3, $4, 'member'), ($5, $2, 'member')`,
		w.actor, w.teamID, w.bob, w.teamBID, w.carol)
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
	w.carolP = &identity.Principal{
		ID: parse(w.carol), Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: parse(w.orgID), TeamIDs: []uuid.UUID{parse(w.teamID)},
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
	return witnessed(ApplyRequest{
		Plan:           twoMachinePlan(),
		Machine:        machine,
		TrustedMachine: "mac",
		Scope:          w.pathStr,
		Commit:         commit,
	})
}

// witnessed stands in for the scan for the tests that are not about #95's
// binding: it attests to exactly the plan it is handed, which is what a real
// inventory would have done for a plan nobody edited. The binding itself is
// exercised against a real Inventory in plan_binding_test.go and must never be
// tested through this helper -- attesting to the plan is precisely the hole #95
// closed.
func witnessed(req ApplyRequest) ApplyRequest {
	if req.Plan.InventoryDigest == "" {
		req.Plan.InventoryDigest = "test-inventory"
	}
	w := InventoryWitness{Digest: req.Plan.InventoryDigest}
	for _, b := range req.Plan.Blocks {
		w.Blocks = append(w.Blocks, BlockRef{Rel: b.Rel, Heading: b.Heading, Ordinal: b.Ordinal, Hash: b.Hash})
	}
	for _, c := range req.Plan.Conflicts {
		for _, side := range c.Pair {
			w.Blocks = append(w.Blocks, BlockRef{Hash: side.Hash})
		}
	}
	for _, m := range req.Plan.Memories {
		w.Memories = append(w.Memories, MemoryRef{Hostname: m.Hostname, Hash: m.Hash})
	}
	req.Witness = w
	return req
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
	// Alice's count(*)=1 on the team-visible seed fails closed if ApplySession
	// is deleted from Tx (constructed red: "alice did not see the team-visible seed").
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	var aliceSeed, bobSeed int
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM instruction WHERE key = 'ci.required' AND body = 'true' AND scope_id = $1`, w.project).Scan(&aliceSeed)
	}); err != nil {
		t.Fatal(err)
	}
	if aliceSeed != 1 {
		t.Fatal("alice did not see the team-visible seed; ApplySession is not reaching the query")
	}
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.bobP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM instruction WHERE key = 'ci.required' AND body = 'true' AND scope_id = $1`, w.project).Scan(&bobSeed)
	}); err != nil {
		t.Fatal(err)
	}
	if bobSeed != 0 {
		t.Fatal("bob (other team) saw the team-visible seed; RLS session settings were not applied")
	}

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
	// Omitting pair[].hostnames from the import_conflict payload, or accepting
	// a payload that names only the applying machine, is the one-line change
	// that makes this red.
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
		var payload struct {
			Hostname  string         `json:"hostname"`
			Hostnames []string       `json:"hostnames"`
			Slot      string         `json:"slot"`
			Pair      []ConflictSide `json:"pair"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		hosts := map[string]bool{}
		for _, side := range payload.Pair {
			if len(side.Hostnames) == 0 {
				t.Fatalf("import_conflict pair side has empty hostnames: %s", raw)
			}
			for _, h := range side.Hostnames {
				hosts[h] = true
			}
		}
		for _, h := range payload.Hostnames {
			hosts[h] = true
		}
		if !hosts["mac"] || !hosts["wsl"] {
			t.Fatalf("import_conflict pair[].hostnames missing mac or wsl: %s", raw)
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
	// Writing a different status in commitWrites than planWrites returned is
	// the one-line change that makes this red. Comparing two ApplyResult
	// values would stay green.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	dry, err := Apply(ctx, st, applyReq(w, "mac", false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	want := plannedWriteSet(dry)
	got := dbWriteSet(t, conn, w)
	if want != got {
		t.Fatalf("dry-run planned set %q != committed rows %q", want, got)
	}
}

func plannedWriteSet(r *ApplyResult) string {
	var lines []string
	add := func(table, body, status string) {
		if table == "review_item" {
			status = "open"
		}
		lines = append(lines, table+"\t"+body+"\t"+status)
	}
	for _, row := range r.Active {
		add(rowTable(row.Kind), row.Body, row.Status)
	}
	for _, row := range r.Proposed {
		add(rowTable(row.Kind), row.Body, row.Status)
	}
	for _, row := range r.Memory {
		add("memory", row.Body, row.Status)
	}
	for _, row := range r.Conflict {
		body := row.Slot
		if body == "" {
			body = row.Body
		}
		add("review_item", body, row.Status)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func dbWriteSet(t *testing.T, conn *pgx.Conn, w importWorld) string {
	t.Helper()
	var lines []string
	q := func(sql string, args ...any) {
		t.Helper()
		rows, err := conn.Query(t.Context(), sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var body, status string
			if err := rows.Scan(&body, &status); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, body+"\t"+status)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	q(`SELECT 'instruction' || E'\t' || body, status::text FROM instruction WHERE scope_id = $1 AND key <> 'ci.required'`, w.project)
	q(`SELECT 'preference' || E'\t' || body, status::text FROM preference WHERE scope_id = $1`, w.team)
	q(`SELECT 'memory' || E'\t' || body, status::text FROM memory WHERE scope_id = $1`, w.project)
	q(`SELECT 'review_item' || E'\t' || COALESCE(payload->>'slot', ''), status::text FROM review_item WHERE kind = 'import_conflict' AND team_id = $1`, w.teamID)
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func rowTable(kind string) string {
	switch kind {
	case "preference":
		return "preference"
	case "import_conflict":
		return "review_item"
	case "fact", "decision", "incident", "lesson", "observation":
		return "memory"
	default:
		return "instruction"
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

func TestApplyFlippedTrustedDoesNotActivateLaterHost(t *testing.T) {
	// canActivate := req.Machine == req.TrustedMachine (ignoring a stored
	// per-scope trusted host) is the one-line change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	if _, err := Apply(ctx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	req := applyReq(w, "wsl", true)
	req.TrustedMachine = "wsl"
	_, err := Apply(ctx, st, witnessed(req))
	got := countByBodyStatus(t, conn, "instruction", w.project)
	if got["Use modules."] == "active" {
		t.Fatalf("flipped -trusted made the later host's block active; row map %#v", got)
	}
	if err == nil {
		t.Fatal("flipped -trusted succeeded; a stored trusted host must reject a later -trusted")
	}
	if !strings.Contains(err.Error(), "trusted") {
		t.Fatalf("flipped -trusted error %v, want it to name trusted", err)
	}
	prefs := countByBodyStatus(t, conn, "preference", w.team)
	if prefs["Prefer tabs."] == "active" {
		t.Fatalf("flipped -trusted activated a later-host conflict side: %#v", prefs)
	}
}

func TestApplyTrustedMarkerVisibleAcrossPrincipals(t *testing.T) {
	// SELECT ingest_receipt only where principal_id = actor is the one-line
	// change that makes this red: Carol's WSL token cannot see Alice's
	// trusted marker and reports "must be imported first".
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	aliceCtx := identity.WithPrincipal(t.Context(), w.aliceP)
	carolCtx := identity.WithPrincipal(t.Context(), w.carolP)

	if _, err := Apply(aliceCtx, st, applyReq(w, "mac", true)); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(carolCtx, st, applyReq(w, "wsl", true))
	if err != nil {
		t.Fatalf("same-team later machine with a different principal: %v", err)
	}
	if res.Duplicate {
		t.Fatal("later machine reported duplicate of the trusted apply")
	}
	got := countByBodyStatus(t, conn, "instruction", w.project)
	if got["Use modules."] != "proposed" {
		t.Fatalf("cross-principal later machine block status %q, want proposed; %#v", got["Use modules."], got)
	}
	if got["Always run gofmt."] != "active" {
		t.Fatalf("trusted block lost active after cross-principal apply: %#v", got)
	}
}

func TestApplyDiscoversSlotConflictAcrossSeparatePlans(t *testing.T) {
	// Copying Conflicts from plan.json and never comparing incoming Blocks
	// against the active row at the same rel#heading is the one-line change
	// that makes this red: two separately-planned machines leave the review
	// queue empty.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	macPlan := Plan{Blocks: []Block{{
		Hash: sha256Hex([]byte("Use vim.")), Heading: "Editor", Body: "Use vim.",
		Kind: "instruction", Rel: ".claude/CLAUDE.md",
		Sources: []Source{{Hostname: "mac"}},
	}}}
	wslPlan := Plan{Blocks: []Block{{
		Hash: sha256Hex([]byte("Use emacs.")), Heading: "Editor", Body: "Use emacs.",
		Kind: "instruction", Rel: ".claude/CLAUDE.md",
		Sources: []Source{{Hostname: "wsl"}},
	}}}
	macReq := ApplyRequest{Plan: macPlan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true}
	wslReq := ApplyRequest{Plan: wslPlan, Machine: "wsl", TrustedMachine: "mac", Scope: w.pathStr, Commit: true}
	if _, err := Apply(ctx, st, witnessed(macReq)); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, st, witnessed(wslReq)); err != nil {
		t.Fatal(err)
	}
	got := countByBodyStatus(t, conn, "instruction", w.project)
	if got["Use vim."] != "active" {
		t.Fatalf("first-machine slot status %q, want active; %#v", got["Use vim."], got)
	}
	if got["Use emacs."] != "proposed" {
		t.Fatalf("conflicting later-machine slot status %q, want proposed; %#v", got["Use emacs."], got)
	}
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM review_item WHERE kind = 'import_conflict' AND scope_id = $1`, w.project).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("separately-planned slot conflict left the review queue empty")
	}
	var raw []byte
	if err := conn.QueryRow(t.Context(), `SELECT payload FROM review_item WHERE kind = 'import_conflict' AND scope_id = $1`, w.project).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "mac") || !strings.Contains(string(raw), "wsl") {
		t.Fatalf("discovered import_conflict missing both hostnames: %s", raw)
	}
}

func TestApplyRefusesDistinctHashesSharingSlotWithoutConflict(t *testing.T) {
	// Accepting two Blocks at the same rel#heading with distinct hashes and
	// no Conflicts entry is the one-line change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	plan := Plan{Blocks: []Block{
		{
			Hash: sha256Hex([]byte("Use vim.")), Heading: "Editor", Body: "Use vim.",
			Kind: "instruction", Rel: ".claude/CLAUDE.md",
			Sources: []Source{{Hostname: "mac"}},
		},
		{
			Hash: sha256Hex([]byte("Use emacs.")), Heading: "Editor", Body: "Use emacs.",
			Kind: "instruction", Rel: ".claude/CLAUDE.md",
			Sources: []Source{{Hostname: "mac"}},
		},
	}}
	_, err := Apply(ctx, st, witnessed(ApplyRequest{Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true}))
	if err == nil {
		t.Fatal("accepted distinct hashes at one slot with no conflict pair")
	}
	if !strings.Contains(err.Error(), "slot") && !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error %v, want it to name the slot or conflict", err)
	}
}

func TestApplyFreshClientIDDoesNotDuplicatePlan(t *testing.T) {
	// Using ClientID as the ingest_receipt primary key instead of
	// (machine, scope, plan-hash) is the one-line change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	first := applyReq(w, "mac", true)
	id1, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	first.ClientID = id1.String()
	if _, err := Apply(ctx, st, witnessed(first)); err != nil {
		t.Fatal(err)
	}
	before := tableCounts(t, conn)
	second := applyReq(w, "mac", true)
	id2, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	second.ClientID = id2.String()
	res, err := Apply(ctx, st, witnessed(second))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Duplicate {
		t.Fatal("retry with a fresh client_id Duplicate=false; the plan-hash receipt must win")
	}
	after := tableCounts(t, conn)
	if after != before {
		t.Fatalf("fresh client_id duplicated rows\nbefore %s\nafter  %s", before, after)
	}
}

func TestApplyReviewItemUsesLeafScopeTeam(t *testing.T) {
	// review_item.team_id = p.TeamIDs[0] is the one-line change that makes
	// this red: a multi-team principal importing --scope .../team:other
	// files the conflict on the wrong team.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	if _, err := conn.Exec(t.Context(), `INSERT INTO membership (principal_id, team_id, role) VALUES ($1, $2, 'member')`, w.actor, w.teamBID); err != nil {
		t.Fatal(err)
	}
	core, err := uuid.Parse(w.teamID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := uuid.Parse(w.teamBID)
	if err != nil {
		t.Fatal(err)
	}
	p := *w.aliceP
	p.TeamIDs = []uuid.UUID{core, other}
	ctx := identity.WithPrincipal(t.Context(), &p)
	scopePath := strings.Replace(w.pathStr, "/team:core/project:plotlens", "/team:other", 1)

	req := applyReq(w, "mac", true)
	req.Scope = scopePath
	if _, err := Apply(ctx, st, witnessed(req)); err != nil {
		t.Fatal(err)
	}
	req = applyReq(w, "wsl", true)
	req.Scope = scopePath
	if _, err := Apply(ctx, st, witnessed(req)); err != nil {
		t.Fatal(err)
	}

	var team string
	if err := conn.QueryRow(t.Context(), `SELECT team_id::text FROM review_item WHERE kind = 'import_conflict' AND scope_id = $1`, w.teamB).Scan(&team); err != nil {
		t.Fatal(err)
	}
	if team != w.teamBID {
		t.Fatalf("review_item.team_id %s, want leaf team %s (principal TeamIDs[0] is %s)", team, w.teamBID, w.teamID)
	}
}

func TestApplySkipsBlocksAlreadyInConflict(t *testing.T) {
	// Inserting Blocks whose hash is already in Conflicts (ignoring
	// conflictHash) is the one-line change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	plan := twoMachinePlan()
	spaces := "Prefer spaces."
	plan.Blocks = append(plan.Blocks, Block{
		Hash: sha256Hex([]byte(spaces)), Heading: "Indent", Body: spaces,
		Kind: "preference", Rel: ".claude/CLAUDE.md",
		Sources: []Source{{Hostname: "mac"}},
	})
	if _, err := Apply(ctx, st, witnessed(ApplyRequest{
		Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM preference WHERE body = $1 AND scope_id = $2`, spaces, w.team).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("conflict-side block written %d times, want 1 (proposed from Conflicts only)", n)
	}
	prefs := countByBodyStatus(t, conn, "preference", w.team)
	if prefs[spaces] != "proposed" {
		t.Fatalf("conflict-side block status %q, want proposed; %#v", prefs[spaces], prefs)
	}
}

func TestApplyRejectsConflictSideWithEmptyHostname(t *testing.T) {
	// Passing conflict sides through with empty Hostnames is the one-line
	// change that makes this red.
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	plan := twoMachinePlan()
	plan.Conflicts[0].Pair[0].Hostnames = nil
	_, err := Apply(ctx, st, witnessed(ApplyRequest{
		Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	}))
	if err == nil {
		t.Fatal("accepted a conflict side with empty hostnames")
	}
	if !strings.Contains(err.Error(), "hostname") {
		t.Fatalf("error %v, want it to name hostname", err)
	}
}
