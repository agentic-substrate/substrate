package mcpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/policy"
)

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

	got, err := ToolSchemas(listed.Tools)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["mcpx.test.echo"]; !ok {
		t.Fatal("expected generated schema for mcpx.test.echo")
	}
	if err := CheckToolNames(listed.Tools); err != nil {
		t.Fatal(err)
	}

	want := map[string]json.RawMessage{
		"mcpx.test.echo": json.RawMessage(`{"type":"object","properties":{"other":{"type":"string"}}}`),
	}
	err = DiffToolSchemas(got, want)
	if err == nil {
		t.Fatal("expected a mismatch when the input schema changes")
	}
	if !strings.Contains(err.Error(), "mcpx.test.echo") {
		t.Fatalf("mismatch must name the tool, got %v", err)
	}
}

func TestDiffToolSchemasEmptyWant(t *testing.T) {
	err := DiffToolSchemas(map[string]json.RawMessage{
		"mcpx.test.echo": json.RawMessage(`{}`),
	}, map[string]json.RawMessage{})
	if err == nil || !strings.Contains(err.Error(), "mcpx.test.echo") {
		t.Fatalf("missing golden must name the tool, got %v", err)
	}
}

func TestCheckToolNamesRejectsUnderscoreFallbackNeeded(t *testing.T) {
	err := CheckToolNames([]*mcp.Tool{{Name: "memory write"}})
	if err == nil {
		t.Fatal("expected invalid name")
	}
	err = CheckToolNames([]*mcp.Tool{{Name: "memory_write"}})
	if err == nil || !strings.Contains(err.Error(), "memory_write") {
		t.Fatalf("names without dots must name the tool, got %v", err)
	}
}

func TestMapErrorKnownCodes(t *testing.T) {
	t.Parallel()
	internalMsg := `pq: relation "memory" does not exist`
	mapped := MapError(t.Context(), errors.New(internalMsg))
	if mapped == nil || !mapped.IsError {
		t.Fatal("unknown errors must become a generic isError result")
	}
	for _, c := range mapped.Content {
		text, ok := c.(*mcp.TextContent)
		if !ok {
			continue
		}
		if strings.Contains(text.Text, "memory") || strings.Contains(text.Text, internalMsg) {
			t.Fatalf("internal error leaked to client: %q", text.Text)
		}
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
		res := MapError(t.Context(), fmt.Errorf("%w: detail", tc.err))
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
