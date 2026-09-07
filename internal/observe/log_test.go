package observe_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/compiler"
	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/mcpx"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/observe"
	"github.com/agentic-substrate/substrate/internal/store"
)

const (
	sentinelToken = "tok_SENTINEL_TOKEN_9e2b1d"
	sentinelBody  = "MEMBODY_SENTINEL_7f3a9c_do_not_log"
	sentinelInst  = "INSTBODY_SENTINEL_c4e8_do_not_log"
)

// syncBuffer is a concurrency-safe sink. slog.SetDefault is process-global, so
// every other test in this package writes through the handler installed here
// while this test reads it. bytes.Buffer is not safe for that.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func testLogger(w *syncBuffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil))
}

// Not parallel: it replaces the process-global default logger.
//
// Goes red if any production path logs the memory body, the bearer token, or
// the token's SHA-256 — including wrapping them in an error that slog then
// prints. Logging nothing also fails: the request line must carry principal_id,
// tool, scope_path, and latency.
func TestLogsNeverContainTokenOrMemoryBody(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	if _, err := conn.Exec(t.Context(), `INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'rule', 'obs.sentinel', $3, 'active', $2)`,
		w.projectA, w.alice, sentinelInst); err != nil {
		t.Fatalf("seed instruction: %v", err)
	}

	logBuf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	rt, err := observe.Setup(t.Context(), observe.Config{Endpoint: "", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = rt.Shutdown(context.Background()) })

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
		return w.aliceP, nil
	})(mux))
	httpSrv := httptest.NewServer(h)
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "observe-log-test", Version: "v0"}, nil)
	sess, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             httpSrv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: sentinelToken}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	writeIn := memory.WriteIn{
		Kind: "fact", Title: "sentinel-title-ok",
		Body: sentinelBody, Scope: w.pathStr, Visibility: "team",
		Identifiers: []string{"sentinel.py"}, Status: "confirmed", Tier: "semantic",
	}
	writeIn.Verification.Type = "human"
	writeIn.Source = &memory.SourceIn{Machine: "wsl"}
	wres, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "memory.write",
		Arguments: writeIn,
	})
	if err != nil {
		t.Fatalf("memory.write: %v", err)
	}
	if wres.IsError {
		t.Fatalf("memory.write isError: %+v", wres.Content)
	}

	sres, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "memory.search",
		Arguments: memory.SearchIn{Query: "sentinel-title-ok", Status: []string{"confirmed"}},
	})
	if err != nil {
		t.Fatalf("memory.search: %v", err)
	}
	if sres.IsError {
		t.Fatalf("memory.search isError: %+v", sres.Content)
	}

	cres, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "context.get",
		Arguments: compiler.GetIn{Repo: w.repoKey, Files: []string{"sentinel.py"}},
	})
	if err != nil {
		t.Fatalf("context.get: %v", err)
	}
	if cres.IsError {
		t.Fatalf("context.get isError: %+v", cres.Content)
	}

	logs := logBuf.String()
	tokenHash := sha256.Sum256([]byte(sentinelToken))
	hashHex := hex.EncodeToString(tokenHash[:])
	for _, s := range []struct{ name, val string }{
		{"token", sentinelToken},
		{"token hash", hashHex},
		{"memory body", sentinelBody},
		{"instruction body", sentinelInst},
	} {
		if strings.Contains(logs, s.val) {
			t.Errorf("%s %q appeared in a log line:\n%s", s.name, s.val, logs)
		}
	}

	mustField := []string{"principal_id", "tool", "scope_path", "latency_ms", "request_id"}
	for _, f := range mustField {
		if !strings.Contains(logs, `"`+f+`"`) {
			t.Errorf("log stream missing %q (EDD §12); logging nothing would also hide a leaked body:\n%s", f, logs)
		}
	}
	if !strings.Contains(logs, w.aliceP.ID.String()) {
		t.Errorf("principal_id value %s never logged", w.aliceP.ID)
	}
	if !strings.Contains(logs, "context.get") {
		t.Errorf("tool=context.get never logged")
	}
}
