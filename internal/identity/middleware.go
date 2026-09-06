package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type ctxKey int

const (
	principalKey ctxKey = iota
	requestIDKey
)

// ErrUnauthorized is returned for missing, revoked, expired, or unknown tokens.
var ErrUnauthorized = errors.New("unauthorized")

// WithPrincipal stores p on ctx for handlers and store.Tx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// FromContext returns the authenticated principal, or nil.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey).(*Principal)
	return p
}

// WithRequestID stores the per-request id used in logs and audit.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFrom returns the request id, or empty.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// LookupFunc turns a bearer token into a principal.
type LookupFunc func(ctx context.Context, token string) (*Principal, error)

// Middleware authenticates Bearer tokens and skips /healthz and /readyz so a
// missing database cannot take probes down with it (EDD R27).
func Middleware(lookup LookupFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}
			raw, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			p, err := lookup(r.Context(), raw)
			if err != nil || p == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			rid, err := uuid.NewV7()
			if err != nil {
				rid = uuid.New()
			}
			ctx := WithPrincipal(r.Context(), p)
			ctx = WithRequestID(ctx, rid.String())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(header[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}
