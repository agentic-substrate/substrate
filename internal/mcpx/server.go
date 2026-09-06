// Package mcpx is the MCP streamable-HTTP transport: typed tool registration
// and policy-denial mapping. Domain packages register tools; this package
// registers none of them.
package mcpx

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is an MCP server that domain packages register tools on.
type Server struct {
	inner *mcp.Server
}

// New returns a server with no tools. Stream resumption stays disabled
// (the SDK default: no EventStore).
func New(name, version string) *Server {
	return &Server{
		inner: mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, nil),
	}
}

// Handler serves /mcp over streamable HTTP.
func (s *Server) Handler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.inner
	}, nil)
}

// AddTool registers a typed tool. Schemas are generated from jsonschema tags
// on In and Out. Policy denials returned by h become isError results with the
// machine-readable SUBSTRATE_* code as the first content item.
func AddTool[In, Out any](s *Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(s.inner, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if err != nil {
			if mapped := MapError(err); mapped != nil {
				return mapped, out, nil
			}
		}
		return res, out, err
	})
}
