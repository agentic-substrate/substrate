package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type renderTarget struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type api struct {
	base    string
	token   string
	client  *http.Client
	timeout time.Duration
}

func newAPI(cfg Config) *api {
	return &api{
		base:    strings.TrimRight(cfg.Server, "/"),
		token:   cfg.Token,
		client:  cfg.httpClient(),
		timeout: cfg.httpTimeout(),
	}
}

func newHookAPI(cfg Config) *api {
	timeout := cfg.hookTimeout()
	inner := cfg.httpClient()
	client := &http.Client{
		Transport:     inner.Transport,
		CheckRedirect: inner.CheckRedirect,
		Jar:           inner.Jar,
		Timeout:       timeout,
	}
	return &api{
		base:    strings.TrimRight(cfg.Server, "/"),
		token:   cfg.Token,
		client:  client,
		timeout: timeout,
	}
}

type httpStatusError struct {
	status int
	msg    string
}

func (e *httpStatusError) Error() string { return e.msg }

func isClientError(err error) bool {
	var hs *httpStatusError
	if !errors.As(err, &hs) {
		return false
	}
	return hs.status >= 400 && hs.status < 500
}

func (a *api) callTimeout() time.Duration {
	if a != nil && a.timeout > 0 {
		return a.timeout
	}
	return DefaultHTTPTimeout
}

// renderResponse is GET /v1/render. Scope is the chain the server compiled
// these targets for; the adapter never derives that chain itself.
type renderResponse struct {
	Targets []renderTarget `json:"targets"`
	Scope   string         `json:"scope"`
}

func (a *api) getRender(ctx context.Context, machine string, repos []string) (renderResponse, int, error) {
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout())
	defer cancel()
	u, err := url.Parse(a.base + "/v1/render")
	if err != nil {
		return renderResponse{}, 0, fmt.Errorf("adapter: render url: %w", err)
	}
	q := u.Query()
	if machine != "" {
		q.Set("machine", machine)
	}
	if len(repos) > 0 {
		q.Set("repos", strings.Join(repos, ","))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return renderResponse{}, 0, err
	}
	a.auth(req)
	res, err := a.client.Do(req)
	if err != nil {
		return renderResponse{}, 0, fmt.Errorf("adapter: GET /v1/render: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return renderResponse{}, res.StatusCode, err
	}
	if res.StatusCode != http.StatusOK {
		return renderResponse{}, res.StatusCode, fmt.Errorf("adapter: GET /v1/render: %s", strings.TrimSpace(string(body)))
	}
	var out renderResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return renderResponse{}, res.StatusCode, fmt.Errorf("adapter: decode render: %w", err)
	}
	return out, res.StatusCode, nil
}

type batchResult struct {
	ClientID  string `json:"client_id"`
	SubjectID string `json:"subject_id"`
	Duplicate bool   `json:"duplicate"`
}

func (a *api) postMemoryBatch(ctx context.Context, items []json.RawMessage) ([]batchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout())
	defer cancel()
	payload, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("adapter: encode batch: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/v1/memory/batch", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	a.auth(req)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adapter: POST /v1/memory/batch: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var out []batchResult
		if json.Unmarshal(body, &out) == nil && len(out) > 0 {
			return out, &httpStatusError{
				status: res.StatusCode,
				msg:    fmt.Sprintf("adapter: POST /v1/memory/batch: status %d", res.StatusCode),
			}
		}
		return nil, &httpStatusError{
			status: res.StatusCode,
			msg:    fmt.Sprintf("adapter: POST /v1/memory/batch: status %d", res.StatusCode),
		}
	}
	var out []batchResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("adapter: decode batch: %w", err)
	}
	return out, nil
}

type cacheItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Identifiers []string  `json:"identifiers"`
	Status      string    `json:"status"`
	UpdatedAt   time.Time `json:"updated_at"`
	ScopePath   string    `json:"scope_path"`
}

func (a *api) getMemoryCache(ctx context.Context, repos []string, since time.Time) ([]cacheItem, error) {
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout())
	defer cancel()
	u, err := url.Parse(a.base + "/v1/memory/cache")
	if err != nil {
		return nil, fmt.Errorf("adapter: cache url: %w", err)
	}
	q := u.Query()
	if len(repos) == 0 {
		return nil, fmt.Errorf("adapter: GET /v1/memory/cache: repos is required")
	}
	q.Set("repos", strings.Join(repos, ","))
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	a.auth(req)
	res, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adapter: GET /v1/memory/cache: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adapter: GET /v1/memory/cache: %s", strings.TrimSpace(string(body)))
	}
	var out struct {
		Memories []cacheItem `json:"memories"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("adapter: decode cache: %w", err)
	}
	return out.Memories, nil
}

type manifestSkill struct {
	Name    string `json:"name"`
	GitPath string `json:"git_path"`
	GitSHA  string `json:"git_sha"`
}

func (a *api) getSkillsManifest(ctx context.Context, repos []string) ([]manifestSkill, error) {
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout())
	defer cancel()
	u, err := url.Parse(a.base + "/v1/skills/manifest")
	if err != nil {
		return nil, fmt.Errorf("adapter: manifest url: %w", err)
	}
	q := u.Query()
	if len(repos) > 0 {
		q.Set("repos", strings.Join(repos, ","))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	a.auth(req)
	res, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adapter: GET /v1/skills/manifest: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adapter: GET /v1/skills/manifest: %s", strings.TrimSpace(string(body)))
	}
	var out struct {
		Skills []manifestSkill `json:"skills"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("adapter: decode manifest: %w", err)
	}
	return out.Skills, nil
}

func (a *api) postReview(ctx context.Context, scope, path, diff string) error {
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout())
	defer cancel()
	payload, err := json.Marshal(map[string]any{
		"kind":  "drift_proposal",
		"scope": scope,
		"payload": map[string]string{
			"path":  path,
			"diff":  diff,
			"title": "drift in " + path,
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/v1/review", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	a.auth(req)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("adapter: POST /v1/review: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return fmt.Errorf("adapter: POST /v1/review: status %d", res.StatusCode)
	}
	return nil
}

func (a *api) auth(req *http.Request) {
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
}

func listenSSE(ctx context.Context, cfg Config, wake chan<- struct{}) {
	a := newAPI(cfg)
	for {
		if ctx.Err() != nil {
			return
		}
		err := a.consumeSSE(ctx, wake)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (a *api) consumeSSE(ctx context.Context, wake chan<- struct{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/v1/events", nil)
	if err != nil {
		return err
	}
	a.auth(req)
	req.Header.Set("Accept", "text/event-stream")
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("adapter: GET /v1/events: status %d", res.StatusCode)
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return sc.Err()
}

func deniedStatus(code int) bool {
	return code == http.StatusForbidden || code == http.StatusNotFound
}
