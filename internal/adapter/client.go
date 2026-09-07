package adapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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

func (a *api) callTimeout() time.Duration {
	if a != nil && a.timeout > 0 {
		return a.timeout
	}
	return DefaultHTTPTimeout
}

func (a *api) getRender(ctx context.Context, machine string, repos []string) ([]renderTarget, int, error) {
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout())
	defer cancel()
	u, err := url.Parse(a.base + "/v1/render")
	if err != nil {
		return nil, 0, fmt.Errorf("adapter: render url: %w", err)
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
		return nil, 0, err
	}
	a.auth(req)
	res, err := a.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("adapter: GET /v1/render: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, res.StatusCode, fmt.Errorf("adapter: GET /v1/render: %s", strings.TrimSpace(string(body)))
	}
	var out struct {
		Targets []renderTarget `json:"targets"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, res.StatusCode, fmt.Errorf("adapter: decode render: %w", err)
	}
	return out.Targets, res.StatusCode, nil
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
