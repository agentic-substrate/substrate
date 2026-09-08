package memory

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/mcpx"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/store"
)

type world struct {
	alice, bob, agent                   string
	orgID, teamAID, teamBID             string
	global, org, teamA, teamB, projectA string
	aliceP, bobP, agentP                *identity.Principal
	path                                string
}

func seedWorld(t *testing.T, conn *pgx.Conn) world {
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
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	w := world{
		alice:    id(),
		bob:      id(),
		agent:    id(),
		orgID:    id(),
		teamAID:  id(),
		teamBID:  id(),
		org:      id(),
		teamA:    id(),
		teamB:    id(),
		projectA: id(),
	}
	orgName := "acme-" + w.orgID[:8]
	w.path = "global:/org:" + orgName + "/team:alpha/project:secret"
	w.global = ensureGlobal(t, conn)
	exec(`INSERT INTO principal (id, kind, display_name, trust) VALUES
		($1, 'user', 'alice', 'human'),
		($2, 'user', 'bob', 'human')`, w.alice, w.bob)
	exec(`INSERT INTO principal (id, kind, display_name, trust, minted_by) VALUES
		($1, 'agent', 'bot', 'agent_interactive', $2)`, w.agent, w.alice)
	exec(`INSERT INTO org (id, name) VALUES ($1, $2)`, w.orgID, orgName)
	exec(`INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'alpha'), ($3, $2, 'beta')`,
		w.teamAID, w.orgID, w.teamBID)
	exec(`INSERT INTO membership (principal_id, team_id, role) VALUES
		($1, $2, 'member'), ($3, $4, 'member'), ($5, $2, 'member')`,
		w.alice, w.teamAID, w.bob, w.teamBID, w.agent)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'org', $2, $3, 0, 'placeholder')`, w.org, w.global, orgName)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'team', $2, 'alpha', 0, 'placeholder', $3),
		($4, 'team', $2, 'beta', 0, 'placeholder', $5)`,
		w.teamA, w.org, w.teamAID, w.teamB, w.teamBID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'project', $2, 'secret', 0, 'placeholder', $3)`, w.projectA, w.teamA, w.teamAID)

	parse := func(s string) uuid.UUID {
		t.Helper()
		u, err := uuid.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	aliceID, bobID, agentID := parse(w.alice), parse(w.bob), parse(w.agent)
	orgID, teamA, teamB := parse(w.orgID), parse(w.teamAID), parse(w.teamBID)
	w.aliceP = &identity.Principal{
		ID: aliceID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamA}, Capabilities: []string{"memory:write"},
	}
	w.bobP = &identity.Principal{
		ID: bobID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamB}, Capabilities: []string{"memory:write"},
	}
	w.agentP = &identity.Principal{
		ID: agentID, Kind: identity.KindAgent, Trust: identity.TrustAgentInteractive,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamA}, Capabilities: []string{"memory:write"},
	}
	return w
}

func openService(t *testing.T, dsn string) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	return New(func() *store.Store { return st }), st
}

func aliceWrite(w world) WriteIn {
	in := WriteIn{
		Kind:       "fact",
		Title:      "validator",
		Body:       "the harness loads validation.py before scoring",
		Scope:      w.path,
		Tier:       "semantic",
		Status:     "confirmed",
		Visibility: "team",
	}
	in.Verification.Type = "human"
	in.Source = &SourceIn{Machine: "wsl"}
	in.Identifiers = []string{"validation.py"}
	return in
}

func TestAgentWriteForcedUnverified(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)

	in := aliceWrite(w)
	in.Status = "confirmed"
	in.Verification.Type = "agent_inference"
	ctx := identity.WithPrincipal(t.Context(), w.agentP)
	out, err := svc.Write(ctx, in)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if out.Status != "unverified" {
		t.Fatalf("agent requested confirmed, stored %q; want unverified (Gotcha 4)", out.Status)
	}
}

func TestHumanWriteMayConfirm(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)

	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	out, err := svc.Write(ctx, aliceWrite(w))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if out.Status != "confirmed" {
		t.Fatalf("human confirmed write stored %q", out.Status)
	}
}

func TestSupersedeRetainsOldRowAndEdge(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, st := openService(t, dsn)

	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	written, err := svc.Write(ctx, aliceWrite(w))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	replaced, err := svc.Supersede(ctx, SupersedeIn{
		OldID:  written.ID,
		Body:   "validation.py now uses ruff",
		Reason: "linter migration",
	})
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if replaced.ID == "" || replaced.ID == written.ID {
		t.Fatalf("supersede id %q, want a new row distinct from %s", replaced.ID, written.ID)
	}

	var oldStatus, supersededBy string
	var oldCount int
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*), status::text, superseded_by::text FROM memory WHERE id = $1 GROUP BY status, superseded_by`, written.ID).
			Scan(&oldCount, &oldStatus, &supersededBy)
	}); err != nil {
		t.Fatalf("old row: %v", err)
	}
	if oldCount != 1 {
		t.Fatal("supersede deleted the old row (Gotcha 6)")
	}
	if supersededBy != replaced.ID {
		t.Fatalf("superseded_by=%q, want %s", supersededBy, replaced.ID)
	}
	if oldStatus != "superseded" {
		t.Fatalf("old status=%q, want superseded so the default-read index no longer matches", oldStatus)
	}

	var statusOnly int
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM memory WHERE id = $1 AND status IN ('confirmed','probable')`, written.ID).
			Scan(&statusOnly)
	}); err != nil {
		t.Fatal(err)
	}
	if statusOnly != 0 {
		t.Fatal("superseded row still matches a status-only confirmed/probable filter")
	}

	var oldBody string
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT body FROM memory WHERE id = $1`, written.ID).Scan(&oldBody)
	}); err != nil {
		t.Fatal(err)
	}
	if oldBody != "the harness loads validation.py before scoring" {
		t.Fatalf("old body was rewritten or deleted: %q", oldBody)
	}

	var edges int
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM memory_edge WHERE relation = 'supersedes' AND (from_id = $1 OR to_id = $1)`, replaced.ID).Scan(&edges)
	}); err != nil {
		t.Fatal(err)
	}
	if edges != 1 {
		t.Fatalf("supersedes edges=%d, want 1", edges)
	}

	var reasonCount int
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit WHERE reason = 'linter migration'`).Scan(&reasonCount)
	}); err != nil {
		t.Fatal(err)
	}
	if reasonCount < 1 {
		t.Fatal("supersede did not write an audit reason")
	}

	def, err := svc.Search(ctx, SearchIn{Query: "validation.py"})
	if err != nil {
		t.Fatalf("default search: %v", err)
	}
	if containsID(def.Results, written.ID) {
		t.Fatal("superseded row appeared in a default read")
	}

	explicit, err := svc.Search(ctx, SearchIn{Query: "validation.py", Status: []string{"superseded"}})
	if err != nil {
		t.Fatalf("explicit search: %v", err)
	}
	if !containsID(explicit.Results, written.ID) {
		t.Fatal("superseded row missing from explicit request")
	}
}

// MEM-4 / Gotcha 4: a human correcting a confirmed fact must publish a
// replacement that default memory.search can see. Passing "" into
// decideStatus made the replacement unverified and invisible — the
// correction removed the wrong answer and put nothing in its place.
func TestHumanSupersedeConfirmedVisibleInDefaultSearch(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	written, err := svc.Write(ctx, aliceWrite(w))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	replaced, err := svc.Supersede(ctx, SupersedeIn{
		OldID:  written.ID,
		Body:   "staging DB is at 10.0.0.9",
		Reason: "corrected wrong host",
	})
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}

	def, err := svc.Search(ctx, SearchIn{Query: "staging DB"})
	if err != nil {
		t.Fatalf("default search: %v", err)
	}
	if !containsID(def.Results, replaced.ID) {
		t.Fatalf("human supersede of confirmed memory produced replacement %s invisible to default memory.search; status likely unverified", replaced.ID)
	}
	hit := hitByID(def.Results, replaced.ID)
	if hit.Status != "confirmed" {
		t.Fatalf("replacement status=%q, want confirmed (inherit superseded row for human actor)", hit.Status)
	}
	if containsID(def.Results, written.ID) {
		t.Fatal("superseded row still in default search")
	}
}

// Gotcha 4 guard: an agent superseding a confirmed row must still land
// unverified. If inheritance is applied without the agent check, this fails.
// The agent must own the row — RLS content_update requires owner_id = actor.
func TestAgentSupersedeForcedUnverified(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, st := openService(t, dsn)

	agentCtx := identity.WithPrincipal(t.Context(), w.agentP)
	in := aliceWrite(w)
	in.Status = "confirmed"
	in.Verification.Type = "agent_inference"
	in.Body = "agent-owned staging host is 10.0.0.5"
	in.Identifiers = []string{"agent-staging-host"}
	written, err := svc.Write(agentCtx, in)
	if err != nil {
		t.Fatalf("agent write: %v", err)
	}
	if written.Status != "unverified" {
		t.Fatalf("agent write status=%q, want unverified before promotion", written.Status)
	}

	// Promote outside the agent write path so inheritance has something above
	// unverified to wrongly copy. FORCE RLS still applies to the table owner,
	// so is_admin must be set for the UPDATE to match.
	if _, err := conn.Exec(t.Context(), `SELECT set_config('substrate.is_admin', 'true', false)`); err != nil {
		t.Fatalf("set is_admin: %v", err)
	}
	tag, err := conn.Exec(t.Context(), `UPDATE memory SET status = 'confirmed' WHERE id = $1`, written.ID)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("promote rows=%d, want 1", tag.RowsAffected())
	}

	replaced, err := svc.Supersede(agentCtx, SupersedeIn{
		OldID:  written.ID,
		Body:   "agent thinks staging is at 10.0.0.9",
		Reason: "agent correction attempt",
	})
	if err != nil {
		t.Fatalf("agent supersede: %v", err)
	}

	var status string
	if err := st.Tx(agentCtx, func(tx pgx.Tx) error {
		return tx.QueryRow(agentCtx, `SELECT status::text FROM memory WHERE id = $1`, replaced.ID).Scan(&status)
	}); err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if status != "unverified" {
		t.Fatalf("agent supersede stored status=%q, want unverified (Gotcha 4)", status)
	}

	aliceCtx := identity.WithPrincipal(t.Context(), w.aliceP)
	def, err := svc.Search(aliceCtx, SearchIn{Query: "agent-staging-host"})
	if err != nil {
		t.Fatalf("default search: %v", err)
	}
	if containsID(def.Results, replaced.ID) {
		t.Fatal("agent supersede replacement appeared in default search; agents must not publish above unverified")
	}
}

func hitByID(hits []SearchHit, id string) SearchHit {
	for _, h := range hits {
		if h.ID == id {
			return h
		}
	}
	return SearchHit{}
}

func TestIdentifierHitOutranksKeyword(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	target := aliceWrite(w)
	written, err := svc.Write(ctx, target)
	if err != nil {
		t.Fatalf("target write: %v", err)
	}
	for i, body := range []string{
		"we validated the python scoring path last week",
		"python tests run after the module loads",
		"a validation helper lives beside the harness",
	} {
		in := aliceWrite(w)
		in.Title = "distractor"
		in.Body = body
		in.Identifiers = nil
		if _, err := svc.Write(ctx, in); err != nil {
			t.Fatalf("distractor %d: %v", i, err)
		}
	}

	out, err := svc.Search(ctx, SearchIn{Query: "validation.py", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out.Results) < 1 {
		t.Fatal("search returned no hits")
	}
	if out.Results[0].ID != written.ID {
		t.Fatalf("rank 1 is %s score=%v snippet=%q; identifier hit %s must outrank keyword hits (MEM-3)",
			out.Results[0].ID, out.Results[0].Score, out.Results[0].Snippet, written.ID)
	}
}

// TestSearchScopeRemainsExactMatch locks the MCP memory.search wire contract:
// Scope filters to one scope_id. Chain filtering is ScopeIDs only (#82).
func TestSearchScopeRemainsExactMatch(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	admin := *w.aliceP
	admin.Trust = identity.TrustHumanAdmin
	adminCtx := identity.WithPrincipal(t.Context(), &admin)

	globalIn := aliceWrite(w)
	globalIn.Scope = "global:"
	globalIn.Visibility = "global"
	globalIn.Title = "exact-scope-ancestor"
	globalIn.Identifiers = []string{"exact-scope-token"}
	if _, err := svc.Write(adminCtx, globalIn); err != nil {
		t.Fatalf("global write: %v", err)
	}
	leaf := aliceWrite(w)
	leaf.Title = "exact-scope-leaf"
	leaf.Identifiers = []string{"exact-scope-token"}
	leaf.Visibility = "global"
	if _, err := svc.Write(ctx, leaf); err != nil {
		t.Fatalf("leaf write: %v", err)
	}

	out, err := svc.Search(ctx, SearchIn{Query: "exact-scope-token", Scope: w.path, Limit: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	foundLeaf, foundAncestor := false, false
	for _, h := range out.Results {
		switch h.Title {
		case "exact-scope-leaf":
			foundLeaf = true
		case "exact-scope-ancestor":
			foundAncestor = true
		}
	}
	if !foundLeaf {
		t.Fatal("exact Scope search missing the leaf memory")
	}
	if foundAncestor {
		t.Fatal("exact Scope search returned an ancestor; MCP single-scope filter must stay exact (#82)")
	}
}

// TestSearchEmptyScopeIDsDoesNotFallThroughToExactScope pins the compiler
// empty-chain case: ScopeIDs is a non-nil empty slice from chainIDs, and must
// not fall through to resolving Scope (which hard-errors on unknown paths).
func TestSearchEmptyScopeIDsDoesNotFallThroughToExactScope(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	missing := "global:/org:missing-" + w.orgID[:8] + "/team:alpha/project:ghost"
	out, err := svc.Search(ctx, SearchIn{
		Query: "any-query", Scope: missing, ScopeIDs: []uuid.UUID{}, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search with empty ScopeIDs: %v; want empty results, not a Scope resolve error", err)
	}
	if len(out.Results) != 0 {
		t.Fatalf("empty ScopeIDs returned %d hits; want 0 (fail-closed, not ELSE true)", len(out.Results))
	}
}

// TestSearchBothEmptyScopeIsUnfilteredByDesign pins the MCP default: no Scope
// and nil ScopeIDs means RLS-only filtering (ELSE true). Deliberate, not a fallthrough.
func TestSearchBothEmptyScopeIsUnfilteredByDesign(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	in := aliceWrite(w)
	in.Title = "unfiltered-scope-sentinel"
	in.Identifiers = []string{"unfiltered-scope-token"}
	in.Visibility = "global"
	if _, err := svc.Write(ctx, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := svc.Search(ctx, SearchIn{Query: "unfiltered-scope-token", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, h := range out.Results {
		if h.Title == "unfiltered-scope-sentinel" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("both-empty Search missed a visible memory; nil ScopeIDs + empty Scope is RLS-only by contract")
	}
}

func TestSearchReturnsWithinTwoSecondsWithoutEmbeddings(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	if _, err := svc.Write(ctx, aliceWrite(w)); err != nil {
		t.Fatalf("write: %v", err)
	}
	start := time.Now()
	out, err := svc.Search(ctx, SearchIn{Query: "validation.py"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("memory.search took %s with no embedding backend; want <= 2s (MEM-5)", d)
	}
	if len(out.Results) < 1 {
		t.Fatal("search returned no hits; a duration-only assertion would pass on an empty result")
	}
}

func TestTeamVisibleMemoryRequiresSession(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, st := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	written, err := svc.Write(ctx, aliceWrite(w))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	bare, err := st.Pool().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bare.Rollback(t.Context()) })
	var n int
	if err := bare.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE id = $1`, written.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("team-visible memory leaked without substrate.* session settings")
	}

	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM memory WHERE id = $1`, written.ID).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("team-visible memory missing after ApplySession")
	}
}

func TestTeamVisibleInvisibleToNonMemberThroughToolPath(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)

	aliceCtx := identity.WithPrincipal(t.Context(), w.aliceP)
	written, err := svc.Write(aliceCtx, aliceWrite(w))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	global := aliceWrite(w)
	global.Visibility = "global"
	global.Title = "public"
	global.Body = "shared global note"
	global.Identifiers = []string{"shared.txt"}
	pub, err := svc.Write(aliceCtx, global)
	if err != nil {
		t.Fatalf("global write: %v", err)
	}

	bobCtx := identity.WithPrincipal(t.Context(), w.bobP)
	out, err := svc.Search(bobCtx, SearchIn{Query: "validation.py", Status: []string{"confirmed", "probable", "unverified"}})
	if err != nil {
		t.Fatalf("bob search: %v", err)
	}
	if containsID(out.Results, written.ID) {
		t.Fatal("team-visible memory leaked to a non-member through the tool path (SCOPE-3); service-layer visibility filter must not be what hides it")
	}

	pubOut, err := svc.Search(bobCtx, SearchIn{Query: "shared.txt"})
	if err != nil {
		t.Fatalf("bob global search: %v", err)
	}
	if !containsID(pubOut.Results, pub.ID) {
		t.Fatal("bob could not see a global-visible memory; search is not reading through RLS")
	}
}

func TestWriteRejectsSecretThroughService(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, st := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	in := aliceWrite(w)
	key := generatedAWSAccessKey(t)
	in.Body = "key " + key
	_, err := svc.Write(ctx, in)
	if !errors.Is(err, policy.ErrSecretDetected) {
		t.Fatalf("err=%v, want %s", err, policy.CodeSecretDetected)
	}
	var n int
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM memory WHERE body LIKE $1`, "%"+key+"%").Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("secret body was stored")
	}
}

func TestDefaultSearchOmitsUnverifiedAndEpisodic(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	confirmed := aliceWrite(w)
	conf, err := svc.Write(ctx, confirmed)
	if err != nil {
		t.Fatal(err)
	}
	unverified := aliceWrite(w)
	unverified.Status = "unverified"
	unverified.Title = "guess"
	unv, err := svc.Write(ctx, unverified)
	if err != nil {
		t.Fatal(err)
	}
	episodic := aliceWrite(w)
	episodic.Tier = "episodic"
	episodic.Title = "session note"
	epi, err := svc.Write(ctx, episodic)
	if err != nil {
		t.Fatal(err)
	}

	def, err := svc.Search(ctx, SearchIn{Query: "validation.py"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsID(def.Results, conf.ID) {
		t.Fatal("confirmed semantic missing from default read")
	}
	if containsID(def.Results, unv.ID) {
		t.Fatal("unverified appeared in default read (MEM-4)")
	}
	if containsID(def.Results, epi.ID) {
		t.Fatal("episodic appeared in default read (MEM-4)")
	}

	expl, err := svc.Search(ctx, SearchIn{Query: "validation.py", Tier: "episodic", Status: []string{"confirmed"}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsID(expl.Results, epi.ID) {
		t.Fatal("episodic missing from explicit request")
	}
}

func TestStripAndCapStored(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, st := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	in := aliceWrite(w)
	in.Body = "hello\x00world"
	out, err := svc.Write(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT body FROM memory WHERE id = $1`, out.ID).Scan(&body)
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "\x00") {
		t.Fatalf("control character stored: %q", body)
	}
	if body != "helloworld" {
		t.Fatalf("stored body = %q, want helloworld after stripControls through Write", body)
	}

	in.Body = strings.Repeat("a", 5000)
	capped, err := svc.Write(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT body FROM memory WHERE id = $1`, capped.ID).Scan(&stored)
	}); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 4096 {
		t.Fatalf("stored body length %d, want 4096 after cap through Write", len(stored))
	}
}

func TestWriteRejectsControlByteSplitSecretsStored(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	awsRest := generatedAWSAccessKey(t)[4:]
	jwt := generatedJWT(t)
	dot := strings.IndexByte(jwt, '.')
	cases := []struct {
		name string
		mut  func(*WriteIn)
	}{
		{"aws", func(in *WriteIn) { in.Body = "key AKIA\x00" + awsRest }},
		{"pem", func(in *WriteIn) {
			in.Body = "-----BEGIN RSA PRIVATE\x00 KEY-----\n" + strings.Repeat("A", 64) + "\n-----END RSA PRIVATE KEY-----"
		}},
		{"jwt", func(in *WriteIn) { in.Title = jwt[:dot] + ".\x00" + jwt[dot+1:] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := aliceWrite(w)
			tc.mut(&in)
			out, err := svc.Write(ctx, in)
			if !errors.Is(err, policy.ErrSecretDetected) {
				t.Fatalf("control-byte %s accepted (id=%s err=%v); scanner ran on the raw input instead of the stored bytes", tc.name, out.ID, err)
			}
		})
	}
}

func TestRegisterExposesMemoryTools(t *testing.T) {
	srv := mcpx.New("substrate-test", "v0")
	Register(srv, func() *store.Store { return nil }, nil)
	sess := connectMCP(t, srv)
	listed, err := sess.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"memory.write", "memory.search", "memory.supersede"} {
		if !names[want] {
			t.Fatalf("missing tool %s; got %v", want, names)
		}
	}
}

func containsID(hits []SearchHit, id string) bool {
	for _, h := range hits {
		if h.ID == id {
			return true
		}
	}
	return false
}

func connectMCP(t *testing.T, srv *mcpx.Server) *mcp.ClientSession {
	t.Helper()
	// Reuse the same streamable-HTTP helper pattern as internal/mcpx tests
	// without importing the mcpx test helpers (different package).
	p := &identity.Principal{ID: uuid.Must(uuid.NewV7()), DisplayName: "test", Trust: identity.TrustHuman}
	return connectMCPLookup(t, srv, p)
}

func connectMCPLookup(t *testing.T, srv *mcpx.Server, p *identity.Principal) *mcp.ClientSession {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/mcp", srv.Handler())
	h := identity.Middleware(func(context.Context, string) (*identity.Principal, error) {
		return p, nil
	})(mux)
	httpSrv := httptest.NewServer(h)
	t.Cleanup(httpSrv.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "memory-test", Version: "v0"}, nil)
	sess, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             httpSrv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: "t"}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

type bearerRT struct{ token string }

func (b bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

// MEM-3 with vectors in play: a PERFECT semantic match must still lose to an
// exact identifier hit. This is the regression that adding embeddings is most
// likely to cause, and it is invisible without a stored vector — the keyword
// version of this test passes either way.
func TestIdentifierStillOutranksAPerfectSemanticMatch(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)

	// The distractor embeds onto the query's axis (cosine distance 0, the full
	// 25 points); the identifier row embeds orthogonally and gets none. A fake
	// returning one fixed vector would boost BOTH rows equally and the test
	// would pass with the semantic weight set arbitrarily high.
	svc = svc.WithEmbedder(&axisEmbedder{query: "validation.py", document: "nothing textually similar"})
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	target := aliceWrite(w)
	target.Title = "the identifier row"
	written, err := svc.Write(ctx, target)
	if err != nil {
		t.Fatalf("target write: %v", err)
	}

	distractor := aliceWrite(w)
	distractor.Title = "the semantic row"
	distractor.Body = "nothing textually similar whatsoever"
	distractor.Identifiers = nil
	other, err := svc.Write(ctx, distractor)
	if err != nil {
		t.Fatalf("distractor write: %v", err)
	}

	out, err := svc.Search(ctx, SearchIn{Query: "validation.py", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("search returned no hits")
	}
	if out.Results[0].ID != written.ID {
		t.Fatalf("rank 1 is %s (%q), want the exact identifier hit %s; a semantic neighbour outranked a typed filename (MEM-3)",
			out.Results[0].ID, out.Results[0].Title, written.ID)
	}
	_ = other
}

// MEM-5 end to end: with the embedder failing on every call, search still
// returns real keyword hits, and does so well inside the 2s bound.
func TestSearchDegradesToKeywordWhenEmbedderIsDown(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	svc = svc.WithEmbedder(&fakeEmbedder{err: errors.New("dial tcp 127.0.0.1:11434: connect: connection refused")})
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	if _, err := svc.Write(ctx, aliceWrite(w)); err != nil {
		t.Fatalf("write with a dead embedder must still succeed: %v", err)
	}

	start := time.Now()
	out, err := svc.Search(ctx, SearchIn{Query: "validation.py", Limit: 10})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("search must not fail because embeddings failed (MEM-5): %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("search returned no hits; degradation dropped the keyword path too")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("search took %v with a dead embedder, want under 2s (MEM-5)", elapsed)
	}
}

// The write path must actually persist a vector. Without this, every semantic
// assertion in this file is vacuous: the ranking term is multiplied by an
// embedding that is NULL.
func TestWriteStoresAnEmbedding(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	svc = svc.WithEmbedder(&fakeEmbedder{vec: unitVec(768)})
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	written, err := svc.Write(ctx, aliceWrite(w))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	var present bool
	if err := conn.QueryRow(t.Context(),
		`SELECT embedding IS NOT NULL FROM memory WHERE id = $1`, written.ID).Scan(&present); err != nil {
		t.Fatalf("query embedding: %v", err)
	}
	if !present {
		t.Fatal("embedding is NULL after a write with a working embedder; the vector never reached the row")
	}
}

// A write whose embed call failed leaves embedding NULL and still succeeds;
// backfill fills it later and is idempotent (EDD §8.4).
func TestBackfillFillsNullEmbeddingsAndIsIdempotent(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	// The GPU is busy: the write must still land.
	down := &fakeEmbedder{err: errors.New("connection refused")}
	written, err := svc.WithEmbedder(down).Write(ctx, aliceWrite(w))
	if err != nil {
		t.Fatalf("write must succeed when embedding fails: %v", err)
	}
	var present bool
	if err := conn.QueryRow(t.Context(),
		`SELECT embedding IS NOT NULL FROM memory WHERE id = $1`, written.ID).Scan(&present); err != nil {
		t.Fatalf("query: %v", err)
	}
	if present {
		t.Fatal("embedding is set after a failed embed; the failure was not tolerated as NULL")
	}

	up := &fakeEmbedder{vec: unitVec(768)}
	svc = svc.WithEmbedder(up)
	n, err := svc.Backfill(ctx, 64)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if n == 0 {
		t.Fatal("backfill filled 0 rows, want at least the row left NULL")
	}
	if err := conn.QueryRow(t.Context(),
		`SELECT embedding IS NOT NULL FROM memory WHERE id = $1`, written.ID).Scan(&present); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !present {
		t.Fatal("backfill reported success but the embedding is still NULL")
	}

	// Idempotence is about this row, not about the call count: the suite shares
	// one database, so a second pass legitimately picks up rows other tests
	// left NULL. What must never happen is an existing vector being rewritten —
	// the UPDATE is guarded on embedding IS NULL precisely so a concurrent or
	// repeated run cannot clobber one.
	var before string
	if err := conn.QueryRow(t.Context(),
		`SELECT embedding::text FROM memory WHERE id = $1`, written.ID).Scan(&before); err != nil {
		t.Fatalf("read embedding: %v", err)
	}
	if _, err := svc.WithEmbedder(&fakeEmbedder{vec: orthoVec(768)}).Backfill(ctx, 64); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	var after string
	if err := conn.QueryRow(t.Context(),
		`SELECT embedding::text FROM memory WHERE id = $1`, written.ID).Scan(&after); err != nil {
		t.Fatalf("re-read embedding: %v", err)
	}
	if after != before {
		t.Fatal("backfill rewrote an embedding that was already set; the IS NULL guard is not holding")
	}
}

// The semantic term must actually contribute. Neither row shares a word with
// the query, so keyword scoring ties them at zero and the tie breaks on
// created_at DESC — which favours the row written SECOND. The aligned row is
// written FIRST, so it can only come back rank 1 if similarity moved it there.
// Delete the vector term from searchSQL and this test fails.
func TestSemanticSimilarityActuallyRanks(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	const query = "zzqq unrelated lexeme"
	svc = svc.WithEmbedder(&axisEmbedder{query: query, document: "aligned marker"})
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	aligned := aliceWrite(w)
	aligned.Title = "aligned marker"
	aligned.Body = "carries no query words at all"
	aligned.Identifiers = nil
	first, err := svc.Write(ctx, aligned)
	if err != nil {
		t.Fatalf("aligned write: %v", err)
	}

	inert := aliceWrite(w)
	inert.Title = "inert row"
	inert.Body = "also carries no query words at all"
	inert.Identifiers = nil
	if _, err := svc.Write(ctx, inert); err != nil {
		t.Fatalf("inert write: %v", err)
	}

	out, err := svc.Search(ctx, SearchIn{Query: query, Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("search returned no hits")
	}
	if out.Results[0].ID != first.ID {
		t.Fatalf("rank 1 is %q, want the semantically aligned row; with no keyword overlap the "+
			"only thing that can promote it is the vector term, so that term is not contributing",
			out.Results[0].Title)
	}
}
