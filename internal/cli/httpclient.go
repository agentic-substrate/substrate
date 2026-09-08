package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPTimeout is the deadline on every control-plane call. It replaces the
// three separately-maintained copies this package consolidated.
const HTTPTimeout = 30 * time.Second

// NewHTTPClient returns the client every substrate command talks to the
// control plane with.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: HTTPTimeout}
}

// apiClient is the shared control-plane caller: bearer auth, absolute URL from
// a base, and a body-carrying error for any non-2xx so the operator sees the
// server's reason rather than just a status line.
type apiClient struct {
	base  string
	token string
	http  *http.Client
	op    string
}

// newAPIClient resolves --server/--token against their environment fallbacks
// and then the credentials `auth login` saved, and refuses to guess either.
func newAPIClient(d Deps, op, server, token string) (*apiClient, error) {
	if server == "" {
		server = d.env("SUBSTRATE_URL")
	}
	if token == "" {
		token = d.env("SUBSTRATE_TOKEN")
	}
	if server == "" || token == "" {
		// auth login wrote these; falling back to them is what makes the
		// login worth doing for the import/review/adapter verbs too.
		if cfg, _, err := loadConfig(d); err == nil {
			if server == "" {
				server = cfg.Server
			}
			if token == "" {
				token = cfg.Token
			}
		}
	}
	if server == "" {
		return nil, &UserError{
			What: op + ": no control plane URL",
			Why:  "neither --server nor SUBSTRATE_URL is set, and no server is saved",
			Next: "re-run with --server https://substrate.example, or run: substrate auth login --server https://substrate.example",
		}
	}
	if token == "" {
		return nil, &UserError{
			What: op + ": no bearer token",
			Why:  "neither --token nor SUBSTRATE_TOKEN is set, and no token is saved",
			Next: "run: substrate auth login --server " + server + ", or pass --token",
		}
	}
	return &apiClient{base: strings.TrimRight(server, "/"), token: token, http: d.httpClient(), op: op}, nil
}

func (a *apiClient) get(ctx context.Context, path string) ([]byte, error) {
	return a.do(ctx, http.MethodGet, path, nil)
}

func (a *apiClient) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	return a.do(ctx, http.MethodPost, path, body)
}

func (a *apiClient) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, rdr) //nolint:gosec // G704: --server is the operator's own control plane
	if err != nil {
		return nil, fmt.Errorf("%s: %w", a.op, err)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.http.Do(req) //nolint:gosec // G704: --server is the operator's own control plane
	if err != nil {
		return nil, &UserError{
			What: a.op + ": cannot reach " + a.base,
			Why:  err.Error(),
			Next: "check --server and that the control plane is up: curl " + a.base + "/healthz",
		}
	}
	defer func() { _ = res.Body.Close() }()
	resp, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", a.op, err)
	}
	if res.StatusCode >= 400 {
		return nil, &UserError{
			What: a.op + ": " + res.Status,
			Why:  strings.TrimSpace(string(resp)),
			Next: "check the token's capabilities and the scope you targeted",
		}
	}
	return resp, nil
}
