package mcpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/policy"
)

//go:embed testdata/tool_schemas.json
var schemaSnapshot []byte

func TestSchemaSnapshotFailsNamingTheTool(t *testing.T) {
	srv := New("substrate-test", "v0")
	AddTool(srv, &mcp.Tool{
		Name:        "mcpx.test.echo",
		Description: "Echo a message.",
	}, echoTool)

	sess := connect(t, srv)
	listed, err := sess.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := toolSchemas(listed.Tools)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["mcpx.test.echo"]; !ok {
		t.Fatal("expected generated schema for mcpx.test.echo")
	}
	if err := checkToolNames(listed.Tools); err != nil {
		t.Fatal(err)
	}

	want := map[string]json.RawMessage{
		"mcpx.test.echo": json.RawMessage(`{"type":"object","properties":{"other":{"type":"string"}}}`),
	}
	err = diffToolSchemas(got, want)
	if err == nil {
		t.Fatal("expected a mismatch when the input schema changes")
	}
	if !strings.Contains(err.Error(), "mcpx.test.echo") {
		t.Fatalf("mismatch must name the tool, got %v", err)
	}
}

func TestSchemaSnapshotMatchesGolden(t *testing.T) {
	srv := New("substrate-test", "v0")
	AddTool(srv, &mcp.Tool{
		Name:        "mcpx.test.echo",
		Description: "Echo a message.",
	}, echoTool)

	sess := connect(t, srv)
	listed, err := sess.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := toolSchemas(listed.Tools)
	if err != nil {
		t.Fatal(err)
	}
	want, err := loadSchemaSnapshot(schemaSnapshot)
	if err != nil {
		b, _ := json.MarshalIndent(got, "", "  ")
		t.Fatalf("%v\ncurrent schemas:\n%s", err, b)
	}
	if err := diffToolSchemas(got, want); err != nil {
		t.Fatal(err)
	}
}

func TestDiffToolSchemasEmptyWant(t *testing.T) {
	err := diffToolSchemas(map[string]json.RawMessage{
		"mcpx.test.echo": json.RawMessage(`{}`),
	}, map[string]json.RawMessage{})
	if err == nil || !strings.Contains(err.Error(), "mcpx.test.echo") {
		t.Fatalf("missing golden must name the tool, got %v", err)
	}
}

func TestCheckToolNamesRejectsUnderscoreFallbackNeeded(t *testing.T) {
	err := checkToolNames([]*mcp.Tool{{Name: "memory write"}})
	if err == nil {
		t.Fatal("expected invalid name")
	}
	err = checkToolNames([]*mcp.Tool{{Name: "memory_write"}})
	if err == nil || !strings.Contains(err.Error(), "memory_write") {
		t.Fatalf("names without dots must name the tool, got %v", err)
	}
}

func TestMapErrorKnownCodes(t *testing.T) {
	t.Parallel()
	if mapped := MapError(errors.New("not a policy error")); mapped != nil {
		t.Fatalf("unknown errors must not be remapped, got %+v", mapped)
	}

	cases := []struct {
		err  error
		code string
	}{
		{policy.ErrDeniedScope, policy.CodeDeniedScope},
		{policy.ErrDeniedVisibility, policy.CodeDeniedVisibility},
		{policy.ErrNeedsReview, policy.CodeNeedsReview},
		{policy.ErrBudgetTooSmall, policy.CodeBudgetTooSmall},
		{policy.ErrSecretDetected, policy.CodeSecretDetected},
	}
	for _, tc := range cases {
		res := MapError(fmt.Errorf("%w: detail", tc.err))
		if res == nil || !res.IsError {
			t.Fatalf("%v: want isError mapping", tc.err)
		}
		text, ok := res.Content[0].(*mcp.TextContent)
		if !ok || text.Text != tc.code {
			t.Fatalf("%v: content[0] = %v, want %s", tc.err, res.Content, tc.code)
		}
		if strings.HasPrefix(tc.code, "ACP_") || !strings.HasPrefix(tc.code, "SUBSTRATE_") {
			t.Fatalf("error code %q must be SUBSTRATE_*, never ACP_*", tc.code)
		}
	}
}

func loadSchemaSnapshot(b []byte) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(raw))
	for name, schema := range raw {
		canon, err := canonicalJSON(schema)
		if err != nil {
			return nil, fmt.Errorf("schema for tool %q: %w", name, err)
		}
		out[name] = canon
	}
	return out, nil
}
