package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "embed"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/mcpx"
)

//go:embed testdata/tool_schemas.json
var schemaSnapshot []byte

func TestHealthzWithoutStore(t *testing.T) {
	h := newHandler(nil, nil)
	healthz := get(t, h, "/healthz")
	defer func() { _ = healthz.Body.Close() }()
	if healthz.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200 with no store", healthz.StatusCode)
	}
	readyz := get(t, h, "/readyz")
	defer func() { _ = readyz.Body.Close() }()
	if readyz.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503 with no store", readyz.StatusCode)
	}
}

func TestServeKeepsListeningWhenPostgresDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		errc <- serve(ctx, ln, "postgres://postgres:x@127.0.0.1:1/none?sslmode=disable&connect_timeout=1", nil)
	}()

	url := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	var code int
	for time.Now().Before(deadline) {
		res, err := client.Get(url + "/healthz")
		if err == nil {
			code = res.StatusCode
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if code == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code != http.StatusOK {
		t.Fatal("/healthz never answered 200; the process exited instead of staying up (EDD §16)")
	}
	res, err := client.Get(url + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503 while Postgres is down", res.StatusCode)
	}
	select {
	case err := <-errc:
		t.Fatalf("serve returned while Postgres was unreachable: %v", err)
	default:
	}
}

func TestProtectedRouteRequiresBearer(t *testing.T) {
	h := newHandler(nil, nil)
	res := get(t, h, "/v1/review")
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/v1/review without bearer = %d, want 401", res.StatusCode)
	}
}

func TestMCPRequiresBearer(t *testing.T) {
	var lookedUp bool
	p := &identity.Principal{ID: uuid.Must(uuid.NewV7()), DisplayName: "test", Trust: identity.TrustHuman}
	h := newHandlerLookup(nil, nil, func(context.Context, string) (*identity.Principal, error) {
		lookedUp = true
		return p, nil
	})
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		lookedUp = false
		req := httptest.NewRequest(method, "/mcp", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s /mcp without bearer = %d, want 401", method, rec.Code)
		}
		if lookedUp {
			t.Fatalf("%s /mcp without bearer ran lookup", method)
		}
	}

	lookedUp = false
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Authorization", "Bearer test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("valid bearer still 401; auth ran after the handler or lookup failed")
	}
	if rec.Code == http.StatusNotFound {
		t.Fatal("/mcp is not mounted")
	}
	if !lookedUp {
		t.Fatal("valid bearer never reached lookup; middleware did not wrap /mcp")
	}
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("valid bearer did not reach the SDK: %d", rec.Code)
	}
}

func TestProductionSchemaSnapshot(t *testing.T) {
	p := &identity.Principal{ID: uuid.Must(uuid.NewV7()), DisplayName: "test", Trust: identity.TrustHuman}
	h := newHandlerLookup(nil, nil, func(context.Context, string) (*identity.Principal, error) {
		return p, nil
	})
	httpSrv := httptest.NewServer(h)
	defer httpSrv.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "cmd-test", Version: "v0"}, nil)
	sess, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             httpSrv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: "test"}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer func() { _ = sess.Close() }()
	listed, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := mcpx.CheckToolNames(listed.Tools); err != nil {
		t.Fatal(err)
	}
	got, err := mcpx.ToolSchemas(listed.Tools)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]json.RawMessage
	if err := json.Unmarshal(schemaSnapshot, &want); err != nil {
		t.Fatal(err)
	}
	if err := mcpx.DiffToolSchemas(got, want); err != nil {
		b, mErr := json.MarshalIndent(got, "", "  ")
		if mErr != nil {
			t.Fatal(err)
		}
		t.Fatalf("%v\ncurrent schemas:\n%s", err, b)
	}
}

func TestMCPInitializeAndListTools(t *testing.T) {
	p := &identity.Principal{ID: uuid.Must(uuid.NewV7()), DisplayName: "test", Trust: identity.TrustHuman}
	h := newHandlerLookup(nil, nil, func(context.Context, string) (*identity.Principal, error) {
		return p, nil
	})
	httpSrv := httptest.NewServer(h)
	defer httpSrv.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "cmd-test", Version: "v0"}, nil)
	sess, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             httpSrv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: "test"}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer func() { _ = sess.Close() }()
	listed, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if listed == nil {
		t.Fatal("tools/list returned nil")
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"context.get", "memory.write", "memory.search", "memory.supersede"} {
		if !names[want] {
			t.Fatalf("production MCP missing %s; got %v", want, names)
		}
	}
}

type bearerRT struct{ token string }

func (b bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

func get(t *testing.T, h http.Handler, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}
