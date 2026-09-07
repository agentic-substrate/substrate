// Package mcpx is the MCP streamable-HTTP transport: typed tool registration
// and policy-denial mapping. Domain packages register tools; this package
// registers none of them.
package mcpx

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/observe"
)

const (
	extraPrincipal = "substrate.principal"
	extraRequestID = "substrate.request_id"
	extraSlot      = "substrate.observe_slot"
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

// Handler serves /mcp over streamable HTTP. It copies the already-authenticated
// principal into auth.TokenInfo.UserID on every request so the SDK's session
// hijack check can reject a bearer that does not match the initializer.
func (s *Server) Handler() http.Handler {
	inner := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.inner
	}, nil)
	return auth.RequireBearerToken(func(ctx context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
		p := identity.FromContext(ctx)
		if p == nil {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{
			UserID: p.ID.String(),
			Extra: map[string]any{
				extraPrincipal: p,
				extraRequestID: identity.RequestIDFrom(ctx),
				extraSlot:      observe.SlotFrom(ctx),
			},
		}, nil
	}, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(inner)
}

// AddTool registers a typed tool. Schemas are generated from jsonschema tags
// on In and Out. The handler sees the principal from the current HTTP request,
// not the one frozen onto the session at initialize. Policy denials returned
// by h become isError results with the machine-readable SUBSTRATE_* code as
// the first content item; other errors become a generic isError.
func AddTool[In, Out any](s *Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	name := t.Name
	mcp.AddTool(s.inner, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		ctx = bindRequest(ctx, req)
		ctx = observe.NewLogFields(ctx)
		start := time.Now()
		ctx, span := observe.Start(ctx, name)
		defer span.End()
		res, out, err := h(ctx, req, in)
		elapsed := time.Since(start)
		observe.LogTool(ctx, name, elapsed)
		if r := observe.FromContext(ctx); r != nil {
			r.RecordToolLatency(ctx, name, elapsed)
		}
		if err != nil {
			return MapError(ctx, err), out, nil
		}
		return res, out, nil
	})
}

func bindRequest(ctx context.Context, req *mcp.CallToolRequest) context.Context {
	if req == nil || req.Extra == nil || req.Extra.TokenInfo == nil {
		return ctx
	}
	extra := req.Extra.TokenInfo.Extra
	if extra == nil {
		return ctx
	}
	if p, ok := extra[extraPrincipal].(*identity.Principal); ok && p != nil {
		ctx = identity.WithPrincipal(ctx, p)
	}
	if rid, ok := extra[extraRequestID].(string); ok && rid != "" {
		ctx = identity.WithRequestID(ctx, rid)
	}
	if s, ok := extra[extraSlot].(*observe.Slot); ok && s != nil {
		ctx = s.Restore(ctx)
	}
	return ctx
}
