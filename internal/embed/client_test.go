package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbedPostsToAPIEmbed(t *testing.T) {
	t.Parallel()
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeEmbed(w, [][]float32{unit(Dim)})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	got, err := c.Embed(t.Context(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if path != "/api/embed" {
		t.Fatalf("path = %q, want /api/embed (EDD §8.4)", path)
	}
	if len(got) != 1 || len(got[0]) != Dim {
		t.Fatalf("got %d vectors of len %v, want 1x%d", len(got), lens(got), Dim)
	}
}

func TestEmbedUsesNomicAndRejectsWrongDimension(t *testing.T) {
	t.Parallel()
	var model string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		model = req.Model
		writeEmbed(w, [][]float32{unit(64)})
	}))
	t.Cleanup(srv.Close)

	_, err := New(srv.URL).Embed(t.Context(), []string{"hello"})
	if err == nil {
		t.Fatal("accepted a 64-d vector; want rejection so vector(768) is not written with the wrong model")
	}
	if !strings.Contains(err.Error(), "768") {
		t.Fatalf("error %q should name the expected dimension", err)
	}
	if model != Model {
		t.Fatalf("model = %q, want %s", model, Model)
	}
}

func TestEmbedBatchesAtMost64(t *testing.T) {
	t.Parallel()
	var maxBatch atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if n := int64(len(req.Input)); n > maxBatch.Load() {
			maxBatch.Store(n)
		}
		vecs := make([][]float32, len(req.Input))
		for i := range vecs {
			vecs[i] = unit(Dim)
		}
		writeEmbed(w, vecs)
	}))
	t.Cleanup(srv.Close)

	texts := make([]string, MaxBatch+1)
	for i := range texts {
		texts[i] = "x"
	}
	got, err := New(srv.URL).Embed(t.Context(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(texts) {
		t.Fatalf("got %d vectors, want %d", len(got), len(texts))
	}
	if maxBatch.Load() > MaxBatch {
		t.Fatalf("batch size %d exceeds %d (EDD §8.4)", maxBatch.Load(), MaxBatch)
	}
}

func TestEmbedRetriesTwiceThenFails(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	_, err := New(srv.URL).Embed(t.Context(), []string{"hello"})
	if err == nil {
		t.Fatal("expected failure after retries")
	}
	// initial try + two retries
	if hits.Load() != 1+Retries {
		t.Fatalf("attempts = %d, want %d (retry ×2)", hits.Load(), 1+Retries)
	}
}

func TestEmbedDeadline(t *testing.T) {
	t.Parallel()
	if Deadline != 5*time.Second {
		t.Fatalf("Deadline = %s, want 5s (EDD §8.4)", Deadline)
	}
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := New(srv.URL).Embed(ctx, []string{"hello"})
	if err == nil {
		t.Fatal("hanging Ollama succeeded")
	}
	select {
	case <-started:
	default:
		t.Fatal("request never reached Ollama")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Embed took %s with a 200ms context; the 5s client deadline must not delay a shorter caller (MEM-5)", d)
	}
}

// syncBuffer is a concurrency-safe sink for the captured logger. slog.SetDefault
// is process-global, so every other test in this package — and any goroutine
// still draining from one — writes through the handler installed here while this
// test reads it. bytes.Buffer is not safe for that, and the resulting race is
// nondeterministic: it passed CI once before failing.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Not parallel on purpose: it replaces the process-global default logger, so
// running alongside siblings makes what it captures depend on scheduling.
func TestEmbedDoesNotLogOrReturnInput(t *testing.T) {
	secret := "the harness loads validation.py before scoring"
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, secret, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := New(srv.URL).Embed(t.Context(), []string{secret})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("input appeared in error: %v", err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatal("input appeared in a log line")
	}
}

func TestEmbedEmptyInputNoHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("unexpected HTTP call")
	}))
	t.Cleanup(srv.Close)
	got, err := New(srv.URL).Embed(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}

func writeEmbed(w http.ResponseWriter, embeddings [][]float32) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model":      Model,
		"embeddings": embeddings,
	})
}

func unit(n int) []float32 {
	v := make([]float32, n)
	v[0] = 1
	return v
}

func lens(vs [][]float32) []int {
	out := make([]int, len(vs))
	for i, v := range vs {
		out[i] = len(v)
	}
	return out
}
