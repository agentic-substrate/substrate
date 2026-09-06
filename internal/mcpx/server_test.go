package mcpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
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

func leakTool(_ context.Context, _ *mcp.CallToolRequest, _ echoIn) (*mcp.CallToolResult, echoOut, error) {
	return nil, echoOut{}, errors.New(`pq: relation "memory" does not exist`)
}

type whoIn struct{}

type whoOut struct {
	PrincipalID string `json:"principal_id" jsonschema:"id of the principal FromContext returned"`
	Label       string `json:"label" jsonschema:"display name from the principal on the handler context"`
}

func whoTool(ctx context.Context, _ *mcp.CallToolRequest, _ whoIn) (*mcp.CallToolResult, whoOut, error) {
	p := identity.FromContext(ctx)
	if p == nil {
		return nil, whoOut{}, nil
	}
	return nil, whoOut{PrincipalID: p.ID.String(), Label: p.DisplayName}, nil
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

func TestInternalErrorDoesNotLeak(t *testing.T) {
	srv := New("substrate-test", "v0")
	AddTool(srv, &mcp.Tool{
		Name:        "mcpx.test.echo",
		Description: "Echo a message.",
	}, leakTool)

	sess := connect(t, srv)
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "mcpx.test.echo",
		Arguments: echoIn{Message: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("internal error must set isError")
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "memory") || strings.Contains(string(raw), "relation") {
		t.Fatalf("internal error leaked to client: %s", raw)
	}
}

func TestSessionHijackDoesNotExecuteAsInitializer(t *testing.T) {
	alice := &identity.Principal{ID: uuid.Must(uuid.NewV7()), DisplayName: "alice", Trust: identity.TrustHuman}
	bob := &identity.Principal{ID: uuid.Must(uuid.NewV7()), DisplayName: "bob", Trust: identity.TrustHuman}
	lookup := func(_ context.Context, tok string) (*identity.Principal, error) {
		switch tok {
		case "alice":
			return alice, nil
		case "bob":
			return bob, nil
		default:
			return nil, identity.ErrUnauthorized
		}
	}

	var seen atomic.Value // string principal id, empty if handler did not run
	srv := New("substrate-test", "v0")
	AddTool(srv, &mcp.Tool{
		Name:        "mcpx.test.who",
		Description: "Report the principal on the handler context. Test-only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ whoIn) (*mcp.CallToolResult, whoOut, error) {
		p := identity.FromContext(ctx)
		id := ""
		if p != nil {
			id = p.ID.String()
		}
		seen.Store(id)
		return nil, whoOut{PrincipalID: id}, nil
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", srv.Handler())
	h := identity.Middleware(lookup)(mux)
	httpSrv := httptest.NewServer(h)
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "mcpx-test", Version: "v0"}, nil)
	sess, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             httpSrv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: "alice"}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	sessionID := sess.ID()
	if sessionID == "" {
		t.Fatal("missing Mcp-Session-Id")
	}

	call := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcpx.test.who","arguments":{}}}`)
	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/mcp", bytes.NewReader(call))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer bob")
	req.Header.Set("Mcp-Session-Id", sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	got, _ := seen.Load().(string)
	if got == alice.ID.String() {
		t.Fatalf("tool executed as initializer %s; Bob's bearer hijacked Alice's session", alice.ID)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403 session user mismatch; body %s; seen %q", resp.StatusCode, body, got)
	}
}

func TestToolHandlerSeesCurrentRequestPrincipal(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	var n atomic.Int32
	var last atomic.Value
	lookup := func(context.Context, string) (*identity.Principal, error) {
		label := fmt.Sprintf("n%d", n.Add(1))
		last.Store(label)
		return &identity.Principal{
			ID:          id,
			DisplayName: label,
			Trust:       identity.TrustHuman,
		}, nil
	}
	srv := New("substrate-test", "v0")
	AddTool(srv, &mcp.Tool{
		Name:        "mcpx.test.who",
		Description: "Report the principal on the handler context. Test-only.",
	}, whoTool)

	sess := connectLookup(t, srv, lookup, "t")
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "mcpx.test.who",
		Arguments: whoIn{},
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
	var out whoOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.PrincipalID != id.String() {
		t.Fatalf("principal_id=%q, want current request %s", out.PrincipalID, id)
	}
	wantLabel, _ := last.Load().(string)
	if out.Label != wantLabel {
		t.Fatalf("label=%q, want current request %q (initialize would freeze n1)", out.Label, wantLabel)
	}
}

func connect(t *testing.T, srv *Server) *mcp.ClientSession {
	t.Helper()
	p := &identity.Principal{ID: uuid.Must(uuid.NewV7()), DisplayName: "test", Trust: identity.TrustHuman}
	return connectLookup(t, srv, func(context.Context, string) (*identity.Principal, error) {
		return p, nil
	}, "test-token")
}

func connectLookup(t *testing.T, srv *Server, lookup identity.LookupFunc, token string) *mcp.ClientSession {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/mcp", srv.Handler())
	h := identity.Middleware(lookup)(mux)
	httpSrv := httptest.NewServer(h)
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "mcpx-test", Version: "v0"}, nil)
	sess, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             httpSrv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{token: token}},
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
