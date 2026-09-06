package mcpx

import (
	"context"
	"errors"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
)

// MapError turns a tool error into an MCP isError result. Policy denials put
// the machine-readable SUBSTRATE_* code first so a hook can branch without
// parsing prose. Unknown errors are logged with request_id and returned as a
// generic message so driver text never reaches the harness.
func MapError(ctx context.Context, err error) *mcp.CallToolResult {
	if err == nil {
		return nil
	}
	code := ""
	switch {
	case errors.Is(err, policy.ErrDeniedScope):
		code = policy.CodeDeniedScope
	case errors.Is(err, policy.ErrDeniedVisibility):
		code = policy.CodeDeniedVisibility
	case errors.Is(err, policy.ErrNeedsReview):
		code = policy.CodeNeedsReview
	case errors.Is(err, policy.ErrBudgetTooSmall):
		code = policy.CodeBudgetTooSmall
	case errors.Is(err, policy.ErrSecretDetected):
		code = policy.CodeSecretDetected
	default:
		slog.Error("mcp tool failed", "err", err, "request_id", identity.RequestIDFrom(ctx))
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{Text: "internal error"},
			},
		}
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: code},
			&mcp.TextContent{Text: err.Error()},
		},
	}
}
