//go:build phase1exit

// Package phase1exit is the Phase 1 exit test (issue #29, EDD §14 last row):
// two substrate-adapter processes and one substrate-server, all real binaries,
// against a real Postgres 16 with pgvector in a container.
//
// It is excluded from `go test ./...` by the phase1exit build tag because it
// takes minutes, not seconds. `make phase1-exit` runs it; CI runs it on main
// and on the milestone, never on every push.
//
// Nothing here touches the operator's $HOME or /work. Every adapter is given
// an explicit -home under t.TempDir(); the binary refuses to guess $HOME, and
// that guard is load-bearing, not incidental.
package phase1exit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/store"
)

const pgImage = "pgvector/pgvector:pg16"

// optOutVars are the environment variables that must NOT be set when this
// suite runs. A Phase 1 exit test with an escape hatch is not a gate.
var optOutVars = []string{
	"SUBSTRATE_SKIP_E2E",
	"SUBSTRATE_E2E_SKIP",
	"SKIP_PHASE1_EXIT",
	"TESTCONTAINERS_RYUK_DISABLED_REASON",
}

func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

// ---------------------------------------------------------------------------
// latency ledger
// ---------------------------------------------------------------------------

// bounds are the product requirement (PRD §8), not a tuning knob. They are
// declared once, here, so raising one is a visible diff and not a quiet edit
// inside an assertion.
const (
	boundMemoryVisible   = 60 * time.Second  // SYNC-2 / MEM-1..5, scenario 1
	boundInstructionSync = 5 * time.Minute   // SYNC-1 / INST-4, scenario 2
	boundOutboxReconnect = 2 * time.Minute   // SYNC-2, scenario 4
	boundSearchNoOllama  = 2 * time.Second   // MEM-5, scenario 5
)

type ledger struct {
	mu   sync.Mutex
	rows []ledgerRow
}

type ledgerRow struct {
	scenario string
	what     string
	observed time.Duration
	bound    time.Duration
}

var observed = &ledger{}

func (l *ledger) record(scenario, what string, got, bound time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rows = append(l.rows, ledgerRow{scenario, what, got, bound})
}

func (l *ledger) report(t *testing.T) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	b.WriteString("\n=== Phase 1 exit: observed latencies ===\n")
	for _, r := range l.rows {
		pct := float64(r.observed) / float64(r.bound) * 100
		b.WriteString(fmt.Sprintf("  %-14s %-46s %10s  bound %8s  (%.1f%% of bound)\n",
			r.scenario, r.what, r.observed.Round(time.Millisecond), r.bound, pct))
	}
	b.WriteString("========================================\n")
	t.Log(b.String())
}

// ---------------------------------------------------------------------------
// Postgres
// ---------------------------------------------------------------------------

func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, pgImage,
		postgres.WithDatabase("substrate"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("phase1 exit: Postgres 16 + pgvector container failed to start: %v\n"+
			"Fix: start the Docker daemon and `docker pull %s`. This suite never skips: "+
			"a Phase 1 exit test that passes without a database asserts nothing.", err, pgImage)
	}
	t.Cleanup(func() {
		if err := ctr.Terminate(context.Background()); err != nil {
			t.Errorf("terminate postgres: %v", err)
		}
	})
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("phase1 exit: postgres connection string: %v", err)
	}
	if err := store.Migrate(ctx, dsn); err != nil {
		t.Fatalf("phase1 exit: migrate: %v", err)
	}
	return dsn
}

// ---------------------------------------------------------------------------
// world
// ---------------------------------------------------------------------------

type world struct {
	orgName     string
	teamAID     string
	aliceID     string
	globalScope string
	orgScope    string
	teamScope   string
	projScope   string
	repoScope   string
	repoKey     string
	// scopePath is the chain the adapters are configured with (-scope) and the
	// scope observations and home-file drift proposals are filed at.
	scopePath string
}

// seedWorld builds the minimum real world: one org, one team, one project, one
// repo, one human, and the INST-2 instruction pair (3.13 at global, 3.12 at
// project) that the ancestor-ordering mutation must break.
//
// The project instruction is inserted `team`-visible, never inserted-then-
// updated: content_freeze_cols cancels a visibility UPDATE, and a `global`
// fixture would pass whether or not RLS session settings were applied.
func seedWorld(t *testing.T, conn *pgx.Conn) world {
	t.Helper()
	ctx := t.Context()
	id := func() string {
		var s string
		if err := conn.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&s); err != nil {
			t.Fatalf("gen_random_uuid: %v", err)
		}
		return s
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed: %v\nquery: %s", err, q)
		}
	}
	w := world{
		orgName: "acme",
		teamAID: id(),
		aliceID: id(),
		orgScope:  id(),
		teamScope: id(),
		projScope: id(),
		repoScope: id(),
		globalScope: id(),
	}
	orgID := id()
	w.repoKey = "github.com/acme/api"
	w.scopePath = "global:/org:acme/team:alpha/project:phase1"

	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1,'global',NULL,'',0,'placeholder')`, w.globalScope)
	exec(`INSERT INTO principal (id, kind, display_name, trust) VALUES ($1,'user','alice','human')`, w.aliceID)
	exec(`INSERT INTO org (id, name) VALUES ($1,$2)`, orgID, w.orgName)
	exec(`INSERT INTO team (id, org_id, name) VALUES ($1,$2,'alpha')`, w.teamAID, orgID)
	exec(`INSERT INTO membership (principal_id, team_id, role) VALUES ($1,$2,'member')`, w.aliceID, w.teamAID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1,'org',$2,$3,0,'placeholder')`, w.orgScope, w.globalScope, w.orgName)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES ($1,'team',$2,'alpha',0,'placeholder',$3)`, w.teamScope, w.orgScope, w.teamAID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES ($1,'project',$2,'phase1',0,'placeholder',$3)`, w.projScope, w.teamScope, w.teamAID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES ($1,'repo',$2,$3,0,'placeholder',$4)`, w.repoScope, w.projScope, w.repoKey, w.teamAID)

	// INST-2: most specific active instruction wins. The global row is the
	// decoy the ancestor ordering must not pick.
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'constraint', 'python.version', '3.13', 'active', $2)`, w.globalScope, w.aliceID)
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'constraint', 'python.version', '3.12', 'active', $2)`, w.projScope, w.aliceID)
	// A second, team-visible key so the render exercises RLS on a row that a
	// missing session setting would hide.
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'rule', 'ci.required', 'true', 'active', $2)`, w.projScope, w.aliceID)
	return w
}

func mintAgent(t *testing.T, dsn string, w world, machine string) string {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	res, err := identity.Mint(t.Context(), st.Pool(), identity.MintInput{
		For:         "agent",
		Parent:      w.aliceID,
		Machine:     machine,
		Scopes:      []string{"memory:write"},
		DisplayName: "adapter-" + machine,
	})
	if err != nil {
		t.Fatalf("mint token for %s: %v", machine, err)
	}
	return res.Token
}

// ---------------------------------------------------------------------------
// binaries
// ---------------------------------------------------------------------------

func buildBinaries(t *testing.T) (server, adapter string) {
	t.Helper()
	dir := t.TempDir()
	build := func(pkg, name string) string {
		out := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", out, pkg) //nolint:gosec // literal argv
		cmd.Dir = repoRoot()
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("phase1 exit: go build %s failed: %v\n%s", pkg, err, b)
		}
		return out
	}
	return build("./cmd/substrate-server", "substrate-server"), build("./cmd/substrate-adapter", "substrate-adapter")
}

// ---------------------------------------------------------------------------
// black-hole Ollama
// ---------------------------------------------------------------------------

// blackHoleOllama accepts TCP connections and never answers. This is the
// hostile shape of "Ollama unavailable": a refused connection fails fast and
// would prove nothing about the embed budget (MEM-5).
func blackHoleOllama(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("black-hole listener: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		var held []net.Conn
		defer func() {
			for _, c := range held {
				_ = c.Close()
			}
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held = append(held, c)
		}
	}()
	return "http://" + ln.Addr().String()
}

// ---------------------------------------------------------------------------
// server process
// ---------------------------------------------------------------------------

type serverProc struct {
	url  string
	logs *lineBuffer
}

type lineBuffer struct {
	mu   sync.Mutex
	data []string
}

func (b *lineBuffer) add(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, s)
}

func (b *lineBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.data, "\n")
}

func startServer(t *testing.T, bin, dsn, ollama string) *serverProc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "-addr", "127.0.0.1:0", "-dsn", dsn, "-ollama", ollama) //nolint:gosec // literal argv
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("server stdout: %v", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start substrate-server: %v", err)
	}
	logs := &lineBuffer{}
	addrCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			logs.add(line)
			var rec struct {
				Msg  string `json:"msg"`
				Addr string `json:"addr"`
			}
			if json.Unmarshal([]byte(line), &rec) == nil && rec.Msg == "listening" && rec.Addr != "" {
				select {
				case addrCh <- rec.Addr:
				default:
				}
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
	var addr string
	select {
	case addr = <-addrCh:
	case <-time.After(30 * time.Second):
		t.Fatalf("phase1 exit: substrate-server never logged its listen address in 30s.\nlogs:\n%s", logs.String())
	}
	sp := &serverProc{url: "http://" + addr, logs: logs}
	waitReady(t, sp.url)
	return sp
}

func waitReady(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/readyz") //nolint:gosec,noctx // test-local URL
		if err == nil {
			code := res.StatusCode
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if code == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("phase1 exit: substrate-server /readyz never went 200. Fix: check the Postgres container and the DSN.")
}

// ---------------------------------------------------------------------------
// controllable proxy (adapter B's link to the server)
// ---------------------------------------------------------------------------

type linkMode int32

const (
	linkUp linkMode = iota
	// linkDown refuses everything: the hour-long outage.
	linkDown
	// linkHollow answers POST /v1/memory/batch with 200 and an EMPTY receipt
	// list without forwarding it. This is the shape a half-wired load balancer
	// produces on reconnection, and it is what makes the outbox receipt check
	// load-bearing rather than decorative.
	linkHollow
)

type link struct {
	mode atomic.Int32
	srv  *httptest.Server
	// batchPosts counts POST /v1/memory/batch requests actually forwarded.
	batchPosts atomic.Int64
}

func newLink(t *testing.T, upstream string) *link {
	t.Helper()
	u, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	rp.FlushInterval = -1 // stream SSE through immediately
	l := &link{}
	l.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch linkMode(l.mode.Load()) {
		case linkDown:
			http.Error(w, "link down", http.StatusServiceUnavailable)
			return
		case linkHollow:
			if r.Method == http.MethodPost && r.URL.Path == "/v1/memory/batch" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
				return
			}
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/memory/batch" {
			l.batchPosts.Add(1)
		}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(l.srv.Close)
	return l
}

func (l *link) set(m linkMode) { l.mode.Store(int32(m)) }
func (l *link) url() string    { return l.srv.URL }

// ---------------------------------------------------------------------------
// adapter process
// ---------------------------------------------------------------------------

type adapterProc struct {
	name      string
	home      string
	statePath string
	token     string
	server    string
	scope     string
	repoDir   string

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	logs   *lineBuffer
	bin    string
	t      *testing.T
}

func newAdapter(t *testing.T, bin, name, serverURL, token string, w world) *adapterProc {
	t.Helper()
	home := t.TempDir()
	// <home>/work/<project>/<repo> is the depth-2 layout Discover scans.
	repoDir := filepath.Join(home, "work", "acme", "api")
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repoDir, "https://"+w.repoKey+".git")
	return &adapterProc{
		name:      name,
		home:      home,
		statePath: filepath.Join(home, ".substrate", "adapter.sqlite"),
		token:     token,
		server:    serverURL,
		scope:     w.scopePath,
		repoDir:   repoDir,
		bin:       bin,
		logs:      &lineBuffer{},
		t:         t,
	}
}

func gitInit(t *testing.T, dir, remote string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...) //nolint:gosec // literal argv
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	run("init", "-q", "-b", "main")
	run("remote", "add", "origin", remote)
}

// start launches the daemon. -home is always explicit: substrate-adapter
// refuses to guess $HOME, which is the guard that keeps this suite off the
// operator's real files.
func (a *adapterProc) start() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, a.bin, //nolint:gosec // literal argv
		"-server", a.server,
		"-token", a.token,
		"-machine", a.name,
		"-home", a.home,
		"-state", a.statePath,
		"-roots", filepath.Join(a.home, "work"),
		"-scope", a.scope,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		a.t.Fatalf("adapter %s stdout: %v", a.name, err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		a.t.Fatalf("start adapter %s: %v", a.name, err)
	}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			a.logs.add(a.name + " | " + sc.Text())
		}
	}()
	a.cmd, a.cancel = cmd, cancel
	a.t.Cleanup(a.stop)
}

func (a *adapterProc) stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd == nil {
		return
	}
	a.cancel()
	_ = a.cmd.Wait()
	a.cmd, a.cancel = nil, nil
}

// managedFiles is every path this adapter renders, in EDD §6 order.
func (a *adapterProc) managedFiles() []string {
	return []string{
		filepath.Join(a.repoDir, "AGENTS.md"),
		filepath.Join(a.home, ".codex", "AGENTS.md"),
		filepath.Join(a.repoDir, "CLAUDE.md"),
		filepath.Join(a.home, ".claude", "CLAUDE.md"),
		filepath.Join(a.home, ".cursor", "rules", "substrate.mdc"),
	}
}

// ---------------------------------------------------------------------------
// MCP client
// ---------------------------------------------------------------------------

type bearerRT struct{ token string }

func (b bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

func connectMCP(t *testing.T, base, token string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "phase1-exit", Version: "v0"}, nil)
	sess, err := c.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             base + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("phase1 exit: MCP initialize against %s failed: %v", base, err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

type searchHit struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type searchOut struct {
	Results []searchHit `json:"results"`
}

// memorySearch calls the real memory.search MCP tool as the given session.
func memorySearch(t *testing.T, sess *mcp.ClientSession, args map[string]any) (searchOut, error) {
	t.Helper()
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{Name: "memory.search", Arguments: args})
	if err != nil {
		return searchOut{}, err
	}
	if res.IsError {
		return searchOut{}, fmt.Errorf("memory.search returned an error result: %+v", res.Content)
	}
	var out searchOut
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return searchOut{}, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return searchOut{}, fmt.Errorf("decode memory.search output: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// REST helpers (as an adapter's own token)
// ---------------------------------------------------------------------------

type reviewItem struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Status  string          `json:"status"`
	Payload json.RawMessage `json:"payload"`
}

func listReviews(t *testing.T, base, token string) []reviewItem {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/v1/review?status=open", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/review: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("GET /v1/review = %d: %s", res.StatusCode, b)
	}
	var out struct {
		Items []reviewItem `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Items
}

func serverRender(t *testing.T, base, token, repoKey string) map[string]string {
	t.Helper()
	u := base + "/v1/render?machine=probe&repos=" + url.QueryEscape(repoKey)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/render: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("GET /v1/render = %d: %s", res.StatusCode, b)
	}
	var out struct {
		Targets []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"targets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, tg := range out.Targets {
		m[tg.Path] = tg.Content
	}
	return m
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
