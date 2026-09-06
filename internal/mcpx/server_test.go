package mcpx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
)

type echoIn struct {
	Message string `json:"message" jsonschema:"text to echo"`
}

type echoOut struct {
	Message string `json:"message" jsonschema:"echoed text"`
}

func echoTool(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, echoOut, error) {
	return nil, echoOut(in), nil
}

func denyTool(_ context.Context, _ *mcp.CallToolRequest, _ echoIn) (*mcp.CallToolResult, echoOut, error) {
	return nil, echoOut{}, fmt.Errorf("%w: not a member of a team at this scope", policy.ErrDeniedScope)
}

func TestUnauthenticatedRejectedBeforeHandler(t *testing.T) {
	srv := New("substrate-test", "v0")
	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		srv.Handler().ServeHTTP(w, r)
	})
	h := identity.Middleware(func(context.Context, string) (*identity.Principal, error) {
		t.Fatal("lookup must not run without a bearer")
		return nil, identity.ErrUnauthorized
	})(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
	if reached {
		t.Fatal("MCP handler ran before auth middleware")
	}
}

func TestInitializeAndListTools(t *testing.T) {
	srv := New("substrate-test", "v0")
	AddTool(srv, &mcp.Tool{
		Name:        "mcpx.test.echo",
		Description: "Echo a message. Test-only; domain tools register themselves.",
	}, echoTool)

	sess := connect(t, srv)

	if sess.InitializeResult() == nil {
		t.Fatal("initialize returned no result")
	}
	listed, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "mcpx.test.echo" {
		t.Fatalf("tools = %+v, want mcpx.test.echo", names(listed.Tools))
	}
	if listed.Tools[0].InputSchema == nil {
		t.Fatal("input schema was not generated from the struct")
	}
	schemaJSON, err := json.Marshal(listed.Tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schemaJSON), `"message"`) {
		t.Fatalf("generated schema missing jsonschema field: %s", schemaJSON)
	}
}

func TestTypedToolRoundTrip(t *testing.T) {
	srv := New("substrate-test", "v0")
	AddTool(srv, &mcp.Tool{
		Name:        "mcpx.test.echo",
		Description: "Echo a message.",
	}, echoTool)

	sess := connect(t, srv)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "mcpx.test.echo",
		Arguments: echoIn{Message: "ping"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out echoOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Message != "ping" {
		t.Fatalf("echoed %q, want ping", out.Message)
	}
}

func TestPolicyDenialIsErrorWithCodeFirst(t *testing.T) {
	srv := New("substrate-test", "v0")
	AddTool(srv, &mcp.Tool{
		Name:        "mcpx.test.echo",
		Description: "Echo a message.",
	}, denyTool)

	sess := connect(t, srv)

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "mcpx.test.echo",
		Arguments: echoIn{Message: "nope"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("policy denial must set isError")
	}
	if len(res.Content) == 0 {
		t.Fatal("policy denial must include content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want text", res.Content[0])
	}
	if text.Text != policy.CodeDeniedScope {
		t.Fatalf("content[0] = %q, want %s first so hooks can branch without parsing prose", text.Text, policy.CodeDeniedScope)
	}
}

func connect(t *testing.T, srv *Server) *mcp.ClientSession {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/mcp", srv.Handler())
	h := identity.Middleware(func(context.Context, string) (*identity.Principal, error) {
		return &identity.Principal{DisplayName: "test", Trust: identity.TrustHuman}, nil
	})(mux)
	httpSrv := httptest.NewServer(h)
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "mcpx-test", Version: "v0"}, nil)
	sess, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             httpSrv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: "test-token"}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("initialize over streamable HTTP: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func names(tools []*mcp.Tool) []string {
	out := make([]string, len(tools))
	for i, tool := range tools {
		out[i] = tool.Name
	}
	return out
}

type bearerRT struct {
	token string
}

func (b bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}
