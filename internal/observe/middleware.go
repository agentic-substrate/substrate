package observe

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/agentic-substrate/substrate/internal/identity"
)

// Middleware attaches the Runtime, a per-request fields slot, and one span
// per request (EDD §12). REST /v1 traffic has no AddTool wrapper, so the
// span must start here; MCP tool spans nest under it. It does not log
// bodies, tokens, or Authorization headers.
func Middleware(r *Runtime) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := WithRuntime(req.Context(), r)
			ctx = withFields(ctx)
			ctx = context.WithValue(ctx, slotKey, SlotFrom(ctx))
			ctx, span := Start(ctx, req.Method+" "+req.URL.Path)
			defer span.End()
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// LogTool writes the EDD §12 request line: ids and paths, never bodies.
func LogTool(ctx context.Context, tool string, d time.Duration) {
	logger := slog.Default()
	if r := FromContext(ctx); r != nil && r.logger != nil {
		logger = r.logger
	}
	pid := ""
	if p := identity.FromContext(ctx); p != nil {
		pid = p.ID.String()
	}
	logger.Info("mcp tool",
		"request_id", identity.RequestIDFrom(ctx),
		"principal_id", pid,
		"tool", tool,
		"scope_path", ScopePathFrom(ctx),
		"latency_ms", d.Milliseconds(),
	)
}
