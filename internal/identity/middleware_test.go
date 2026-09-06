package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareSkipsHealthAndReady(t *testing.T) {
	called := false
	lookup := func(context.Context, string) (*Principal, error) {
		called = true
		return nil, errors.New("lookup must not run")
	}
	h := Middleware(lookup)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d, want 200 without a bearer token", path, rec.Code)
		}
	}
	if called {
		t.Fatal("lookup ran for a liveness/readiness probe")
	}
}

func TestMiddlewareUnauthorized(t *testing.T) {
	lookup := func(context.Context, string) (*Principal, error) {
		return nil, ErrUnauthorized
	}
	reached := false
	h := Middleware(lookup)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))

	t.Run("missing header", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/review", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", rec.Code)
		}
		if reached {
			t.Fatal("handler ran with no token")
		}
	})
	t.Run("revoked or unknown token", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/review", nil)
		req.Header.Set("Authorization", "Bearer deadbeef")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", rec.Code)
		}
		if reached {
			t.Fatal("handler ran for a rejected token")
		}
	})
}

func TestMiddlewareSetsPrincipalOnContext(t *testing.T) {
	want := &Principal{DisplayName: "alice", Trust: TrustHuman}
	lookup := func(context.Context, string) (*Principal, error) {
		return want, nil
	}
	h := Middleware(lookup)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := FromContext(r.Context())
		if p == nil || p.DisplayName != "alice" {
			t.Errorf("principal missing from context")
		}
		if RequestIDFrom(r.Context()) == "" {
			t.Errorf("request_id missing from context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/review", nil)
	req.Header.Set("Authorization", "Bearer "+EncodeToken(make([]byte, 32)))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
}
