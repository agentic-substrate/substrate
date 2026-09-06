package mcpx

import (
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/policy"
)

// MapError turns a policy denial into an MCP tool error. The first content
// item is the machine-readable code so a hook can branch without parsing
// prose. Unknown errors return nil and the SDK packs them as ordinary tool
// errors.
func MapError(err error) *mcp.CallToolResult {
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
		return nil
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: code},
			&mcp.TextContent{Text: err.Error()},
		},
	}
}
