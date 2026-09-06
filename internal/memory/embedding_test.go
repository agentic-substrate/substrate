package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agentic-substrate/substrate/internal/mcpx"
	"github.com/agentic-substrate/substrate/internal/store"
)

// fakeEmbedder returns a fixed vector, or fails, on demand.
type fakeEmbedder struct {
	vec   []float32
	err   error
	delay time.Duration
	calls int
}

func (f *fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = f.vec
	}
	return out, nil
}

func unitVec(n int) []float32 {
	v := make([]float32, n)
	v[0] = 1
	return v
}

// orthoVec is unitVec's opposite axis: cosine distance 1 from it, so a row
// embedded with one scores zero semantic similarity against the other.
func orthoVec(n int) []float32 {
	v := make([]float32, n)
	v[1] = 1
	return v
}

// axisEmbedder puts the search query and one chosen document on the SAME axis,
// and everything else on the orthogonal one. That is what makes a ranking test
// meaningful: a fake returning one fixed vector boosts every row equally, so
// the test passes with the semantic weight set arbitrarily high — which is
// exactly the mistake this type exists to prevent.
type axisEmbedder struct {
	query    string // matched exactly: search passes the bare query string
	document string // matched as a substring of title+"\n"+body
}

func (a *axisEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if t == a.query || (a.document != "" && strings.Contains(t, a.document)) {
			out[i] = unitVec(768)
		} else {
			out[i] = orthoVec(768)
		}
	}
	return out, nil
}

// A nil embedder is a supported configuration, not an error. Search must be
// keyword-only rather than failing (MEM-5).
func TestVectorForIsNilWithoutEmbedder(t *testing.T) {
	s := New(nil)
	if got := s.vectorFor(t.Context(), "anything"); got != nil {
		t.Fatalf("vectorFor with no embedder = %v, want nil", got)
	}
}

// The whole of MEM-5: Ollama being down degrades, it does not fail.
func TestVectorForReturnsNilWhenEmbedderFails(t *testing.T) {
	f := &fakeEmbedder{err: errors.New("connection refused")}
	s := New(nil).WithEmbedder(f)
	if got := s.vectorFor(t.Context(), "q"); got != nil {
		t.Fatalf("vectorFor with a failing embedder = %v, want nil", got)
	}
	if f.calls != 1 {
		t.Fatalf("embedder called %d times, want 1", f.calls)
	}
}

// A hung backend must not consume the caller's 2s search budget.
func TestVectorForGivesUpOnASlowEmbedder(t *testing.T) {
	f := &fakeEmbedder{vec: unitVec(768), delay: 5 * time.Second}
	s := New(nil).WithEmbedder(f)
	start := time.Now()
	got := s.vectorFor(t.Context(), "q")
	elapsed := time.Since(start)
	if got != nil {
		t.Fatalf("vectorFor with a hung embedder = %v, want nil", got)
	}
	if elapsed > embedBudget+500*time.Millisecond {
		t.Fatalf("vectorFor took %v, want it to give up near %v", elapsed, embedBudget)
	}
}

// A wrong-width vector must not reach the vector(768) column: Postgres would
// reject it and turn a degradable condition into a failed search.
func TestVectorForRejectsAWrongWidthVector(t *testing.T) {
	s := New(nil).WithEmbedder(&fakeEmbedder{vec: unitVec(64)})
	if got := s.vectorFor(t.Context(), "q"); got != nil {
		t.Fatalf("vectorFor accepted a %d-d vector for a vector(768) column", len(got.Slice()))
	}
}

func TestVectorForAcceptsTheRightWidth(t *testing.T) {
	s := New(nil).WithEmbedder(&fakeEmbedder{vec: unitVec(768)})
	got := s.vectorFor(t.Context(), "q")
	if got == nil {
		t.Fatal("vectorFor with a working embedder = nil, want a vector")
	}
	if n := len(got.Slice()); n != embedDim {
		t.Fatalf("vector width %d, want %d", n, embedDim)
	}
}

// A panicking client must degrade, not take down the request.
func TestVectorForSurvivesAPanickingEmbedder(t *testing.T) {
	s := New(nil).WithEmbedder(panicEmbedder{})
	if got := s.vectorFor(t.Context(), "q"); got != nil {
		t.Fatalf("vectorFor = %v after a panic, want nil", got)
	}
}

type panicEmbedder struct{}

func (panicEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	panic("ollama client blew up")
}

// storeEmbedding must never panic or block when there is no embedder; the
// write path calls it unconditionally.
func TestStoreEmbeddingIsANoOpWithoutEmbedder(t *testing.T) {
	s := New(nil)
	s.storeEmbedding(t.Context(), nil, "id", "title", "body")
}

func TestBackfillIsANoOpWithoutEmbedder(t *testing.T) {
	s := New(nil)
	n, err := s.Backfill(t.Context(), 10)
	if err != nil {
		t.Fatalf("Backfill without an embedder: %v", err)
	}
	if n != 0 {
		t.Fatalf("Backfill filled %d rows, want 0", n)
	}
}

// Register must carry the embedder through to the Service. Dropping it is
// silent in both directions: production quietly falls back to keyword-only,
// and any test that builds its own Service still passes. This is the exact
// wiring that was missing when semantic search first landed.
func TestRegisterWiresTheEmbedder(t *testing.T) {
	f := &fakeEmbedder{vec: unitVec(768)}
	svc := Register(mcpx.New("substrate-test", "v0"), func() *store.Store { return nil }, f)
	if svc.embedder == nil {
		t.Fatal("Register dropped the embedder; semantic search would be dead in production")
	}
	if got := svc.vectorFor(t.Context(), "q"); got == nil {
		t.Fatal("the wired embedder produced no vector")
	}
}

func TestRegisterWithoutEmbedderIsKeywordOnly(t *testing.T) {
	svc := Register(mcpx.New("substrate-test", "v0"), func() *store.Store { return nil }, nil)
	if svc.embedder != nil {
		t.Fatal("Register invented an embedder that was not supplied")
	}
}
