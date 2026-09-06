// Package embed calls Ollama's /api/embed directly from Go (EDD §8.4, R21).
// There is no Python in the Phase 1 request path.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	// Model is nomic-embed-text. Changing it requires a new migration: the
	// column is vector(768) and a different width cannot be stored (EDD §8.4).
	Model = "nomic-embed-text"
	// Dim is the width of Model and of memory.embedding.
	Dim = 768
	// MaxBatch is the largest input array sent in one /api/embed call.
	MaxBatch = 64
	// Deadline is the per-attempt HTTP timeout.
	Deadline = 5 * time.Second
	// Retries is additional attempts after the first failure.
	Retries = 2
)

// Client calls Ollama's /api/embed.
type Client struct {
	base   string
	client *http.Client
}

// New returns a Client talking to base (e.g. http://127.0.0.1:11434).
func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		client: &http.Client{
			Timeout: Deadline,
		},
	}
}

type embedReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResp struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed returns a 768-d vector per input, in order. Batches of MaxBatch.
// Failures are returned to the caller; the write/search path decides whether
// to store NULL or degrade (MEM-5). Inputs are never logged.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if c == nil {
		return nil, fmt.Errorf("embed: client is nil")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += MaxBatch {
		end := i + MaxBatch
		if end > len(texts) {
			end = len(texts)
		}
		chunk, err := c.embedBatch(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}

func (c *Client) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	var last error
	attempts := 1 + Retries
	for i := 0; i < attempts; i++ {
		vecs, retry, err := c.doEmbed(ctx, texts)
		if err == nil {
			return vecs, nil
		}
		last = err
		if !retry || ctx.Err() != nil {
			return nil, err
		}
	}
	return nil, last
}

func (c *Client) doEmbed(ctx context.Context, texts []string) ([][]float32, bool, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, Deadline)
	defer cancel()
	body, err := json.Marshal(embedReq{Model: Model, Input: texts})
	if err != nil {
		return nil, false, fmt.Errorf("embed: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("embed: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		slog.Warn("embed request failed", "n", len(texts), "ms", time.Since(start).Milliseconds())
		return nil, false, fmt.Errorf("embed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("embed: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		slog.Warn("embed non-200", "status", resp.StatusCode, "n", len(texts), "ms", time.Since(start).Milliseconds())
		retry := resp.StatusCode >= 500
		return nil, retry, fmt.Errorf("embed: ollama status %d", resp.StatusCode)
	}
	var parsed embedResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, fmt.Errorf("embed: decode: %w", err)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, false, fmt.Errorf("embed: got %d vectors for %d inputs", len(parsed.Embeddings), len(texts))
	}
	for i, v := range parsed.Embeddings {
		if len(v) != Dim {
			return nil, false, fmt.Errorf("embed: vector %d has %d dims, want %d", i, len(v), Dim)
		}
	}
	slog.Info("embed ok", "n", len(texts), "ms", time.Since(start).Milliseconds())
	return parsed.Embeddings, false, nil
}
