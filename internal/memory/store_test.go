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

func TestIdentifierHitOutranksKeyword(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	svc, _ := openService(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	target := aliceWrite(w)
	if _, err := svc.Write(ctx, target); err != nil {
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
		_ = i
	}

	out, err := svc.Search(ctx, SearchIn{Query: "validation.py", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out.Results) < 1 {
		t.Fatal("search returned no hits")
	}
	top := 3
	if len(out.Results) < top {
		top = len(out.Results)
	}
	found := false
	for _, h := range out.Results[:top] {
		if strings.Contains(h.Snippet, "validation.py") || strings.Contains(h.Title, "validator") {
			found = true
		}
	}
	if !found {
		t.Fatalf("memory whose body contains validation.py not in top 3: %+v", out.Results[:top])
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
	if _, err := svc.Search(ctx, SearchIn{Query: "validation.py"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("memory.search took %s with no embedding backend; want <= 2s (MEM-5)", d)
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
	if body != "helloworld" && body != "hello\nworld" && !strings.Contains(body, "hello") {
		t.Fatalf("body after strip = %q", body)
	}
}

func TestRegisterExposesMemoryTools(t *testing.T) {
	srv := mcpx.New("substrate-test", "v0")
	Register(srv, func() *store.Store { return nil })
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
