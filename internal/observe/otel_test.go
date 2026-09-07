package observe_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/compiler"
	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/mcpx"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/observe"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

type world struct {
	alice, orgID, teamAID              string
	global, org, teamA, projectA, repo string
	repoKey, pathStr                   string
	aliceP                             *identity.Principal
	path                               scope.Path
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
			t.Fatalf("exec: %v", err)
		}
	}
	w := world{
		alice:    id(),
		orgID:    id(),
		teamAID:  id(),
		org:      id(),
		teamA:    id(),
		projectA: id(),
		repo:     id(),
	}
	w.global = ensureGlobal(t, conn)
	orgName := "acme-" + w.orgID[:8]
	w.repoKey = "github.com/acme/obs-" + w.repo[:8]
	w.pathStr = "global:/org:" + orgName + "/team:alpha/project:obs"
	p, err := scope.Parse(w.pathStr)
	if err != nil {
		t.Fatal(err)
	}
	w.path = p

	exec(`INSERT INTO principal (id, kind, display_name, trust) VALUES ($1, 'user', 'alice', 'human')`, w.alice)
	exec(`INSERT INTO org (id, name) VALUES ($1, $2)`, w.orgID, orgName)
	exec(`INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'alpha')`, w.teamAID, w.orgID)
	exec(`INSERT INTO membership (principal_id, team_id, role) VALUES ($1, $2, 'member')`, w.alice, w.teamAID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'org', $2, $3, 0, 'placeholder')`, w.org, w.global, orgName)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'team', $2, 'alpha', 0, 'placeholder', $3)`, w.teamA, w.org, w.teamAID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'project', $2, 'obs', 0, 'placeholder', $3)`, w.projectA, w.teamA, w.teamAID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'repo', $2, $3, 0, 'placeholder', $4)`, w.repo, w.projectA, w.repoKey, w.teamAID)

	// Inserted as team (not updated onto team): content_freeze_cols
	// silently cancels a visibility UPDATE, which would make RLS return
	// zero rows and look like a missing instruction.
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'constraint', 'python.version', '3.12', 'active', $2)`,
		w.projectA, w.alice)

	aliceID := uuid.MustParse(w.alice)
	w.aliceP = &identity.Principal{
		ID: aliceID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: uuid.MustParse(w.orgID), TeamIDs: []uuid.UUID{uuid.MustParse(w.teamAID)},
		Capabilities: []string{"memory:write"},
	}
	return w
}

type bearerRT struct{ token string }

func (b bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

func instrumentedMCP(t *testing.T, dsn string, principal *identity.Principal, rt *observe.Runtime, token string) *mcp.ClientSession {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	mem := memory.New(func() *store.Store { return st })
	srv := mcpx.New("substrate-test", "v0")
	memory.Register(srv, func() *store.Store { return st }, nil)
	compiler.Register(srv, func() *store.Store { return st }, mem)
	mux := http.NewServeMux()
	mux.Handle("/mcp", srv.Handler())
	h := observe.Middleware(rt)(identity.Middleware(func(context.Context, string) (*identity.Principal, error) {
		return principal, nil
	})(mux))
	httpSrv := httptest.NewServer(h)
	t.Cleanup(httpSrv.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "observe-test", Version: "v0"}, nil)
	sess, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             httpSrv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func callContextGet(t *testing.T, sess *mcp.ClientSession, repo string) compiler.GetOut {
	t.Helper()
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "context.get",
		Arguments: compiler.GetIn{Repo: repo},
	})
	if err != nil {
		t.Fatalf("context.get CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("context.get isError: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out compiler.GetOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode GetOut: %v", err)
	}
	if out.PackID == "" {
		t.Fatal("context.get returned an empty pack_id")
	}
	return out
}

// Goes red if context.get records no substrate_tool_latency_seconds{tool="context.get"}
// or if the trace is a single span with no compile/database children.
func TestContextGetEmitsLatencyMetricAndCompileDatabaseSpans(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	rec := newOTLPReceiver(t)
	rt, err := observe.Setup(t.Context(), observe.Config{
		Endpoint:       rec.URL,
		Service:        "substrate-test",
		ExportInterval: time.Hour,
		ExportTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = rt.Shutdown(context.Background()) })

	sess := instrumentedMCP(t, dsn, w.aliceP, rt, "test-token")
	out := callContextGet(t, sess, w.repoKey)
	if !strings.Contains(out.Markdown, "python.version") {
		t.Fatalf("pack missing python.version:\n%s", out.Markdown)
	}

	flushCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := rt.ForceFlush(flushCtx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}

	if !rec.hasMetric(observe.MetricToolLatency, "tool", "context.get") {
		t.Fatalf("collector did not receive %s{tool=context.get}; metrics=%v spans=%v paths=%v lastErr=%q", observe.MetricToolLatency, rec.metricNames(), rec.spanNames(), rec.requestPaths(), rec.errorText())
	}

	compileSpans := rec.spansNamed(observe.SpanCompile)
	if len(compileSpans) == 0 {
		t.Fatalf("no %q span; a request span with no compile child would pass a presence check on the parent only", observe.SpanCompile)
	}
	dbSpans := rec.spansNamed(observe.SpanDatabase)
	if len(dbSpans) == 0 {
		t.Fatalf("no %q span; compile without a database child hides a store round-trip that never ran", observe.SpanDatabase)
	}

	byID := map[string]spanRec{}
	var rootTrace string
	for _, s := range rec.allSpans() {
		byID[s.SpanID] = s
		if strings.Contains(s.Name, "context.get") {
			rootTrace = s.TraceID
		}
	}
	if rootTrace == "" {
		rootTrace = compileSpans[0].TraceID
	}
	compile := compileSpans[0]
	if compile.TraceID != rootTrace {
		t.Fatalf("compile span is on trace %s, want the request trace %s", compile.TraceID, rootTrace)
	}
	db := dbSpans[0]
	if db.TraceID != rootTrace {
		t.Fatalf("database span is on trace %s, want the request trace %s", db.TraceID, rootTrace)
	}
	if db.ParentSpanID == "" {
		t.Fatal("database span has no parent; it must be a child of compile or the request")
	}
	if _, ok := byID[db.ParentSpanID]; !ok {
		t.Fatalf("database parent %s is not in the same export; child spans were not parented", db.ParentSpanID)
	}
}

// Goes red if Setup with an empty endpoint returns an error, or if recording panics.
func TestEmptyOTLPEndpointIsSupported(t *testing.T) {
	rt, err := observe.Setup(t.Context(), observe.Config{Endpoint: ""})
	if err != nil {
		t.Fatalf("empty -otlp must succeed the same way empty -ollama does, got %v", err)
	}
	t.Cleanup(func() { _ = rt.Shutdown(context.Background()) })
	rt.RecordToolLatency(t.Context(), "context.get", time.Millisecond)
	rt.SetOutboxDepth(t.Context(), "wsl", 0)
	if err := rt.ForceFlush(t.Context()); err != nil {
		t.Fatalf("ForceFlush with export disabled: %v", err)
	}
}

// Goes red if export runs on the request path (hanging collector delays the
// call) or if each failed export logs again (Once was not used).
func TestUnreachableCollectorDoesNotFailRequestsAndLogsOnce(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				<-hang
				_ = c.Close()
			}(c)
		}
	}()

	logBuf := &syncBuffer{}
	logger := testLogger(logBuf)
	rt, err := observe.Setup(t.Context(), observe.Config{
		Endpoint:       "http://" + ln.Addr().String(),
		Service:        "substrate-test",
		ExportInterval: time.Hour,
		ExportTimeout:  150 * time.Millisecond,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("Setup against unreachable collector: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = rt.Shutdown(ctx)
	})

	sess := instrumentedMCP(t, dsn, w.aliceP, rt, "test-token")
	start := time.Now()
	for i := 0; i < 3; i++ {
		_ = callContextGet(t, sess, w.repoKey)
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("3 context.get calls took %s against a hanging collector; export is on the request path", elapsed)
	}

	flushCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_ = rt.ForceFlush(flushCtx)
	_ = rt.ForceFlush(flushCtx)

	logs := logBuf.String()
	n := strings.Count(logs, "otlp export failed")
	if n != 1 {
		t.Fatalf("otlp export failed appeared %d times, want 1 (once, not per request):\n%s", n, logs)
	}
}
