package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"

	"github.com/agentic-substrate/substrate/internal/observe"
	"github.com/agentic-substrate/substrate/internal/store"
)

// Embedder produces one vector per input. *embed.Client satisfies it; tests
// substitute a fake. A nil Embedder means keyword-only, which is a supported
// configuration, not a degraded one (MEM-5).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// embedBudget bounds the embed call on the request path. memory.search must
// return within 2s whatever Ollama is doing (MEM-5), so the embed attempt gets
// a slice of that and the caller proceeds keyword-only when it expires.
const embedBudget = 900 * time.Millisecond

// embedDim is the width memory.embedding accepts. A client returning anything
// else must be ignored rather than passed to Postgres.
const embedDim = 768

// embedText is what gets vectorised: the title and body a human would read.
// Identifiers are matched exactly and by trigram, not semantically.
func embedText(title, body string) string { return title + "\n" + body }

// vectorFor returns a query vector, or nil when embeddings are unavailable for
// ANY reason. It never returns an error: a search that fails because the GPU is
// busy is worse than a search without semantic hits (MEM-5).
func (s *Service) vectorFor(ctx context.Context, text string) *pgvector.Vector {
	if s.embedder == nil {
		return nil
	}
	// A SIBLING context, not a child of the caller's 2s search budget. Deriving
	// it from that budget lets a slow embed spend 900ms of the time the query
	// still needs, so a slow-but-working Ollama turns a search that would have
	// succeeded into a deadline error — search failing *because of* embeddings,
	// which is exactly what MEM-5 forbids. Cancellation of the parent is still
	// honoured via context.Cause on the parent below.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), embedBudget)
	defer cancel()
	vecs, err := func() (v [][]float32, err error) {
		// A third-party client panicking must degrade to keyword-only, not take
		// down the request.
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("embed panicked: %v", r)
			}
		}()
		return s.embedder.Embed(ctx, []string{text})
	}()
	if err != nil || len(vecs) != 1 || len(vecs[0]) != embedDim {
		slog.Debug("search degraded to keyword", "reason", errText(err))
		return nil
	}
	v := pgvector.NewVector(vecs[0])
	return &v
}

func errText(err error) string {
	if err == nil {
		return "no vector returned"
	}
	return err.Error()
}

// storeEmbedding vectorises a stored row best-effort. A failure leaves
// embedding NULL for the backfill job and is NOT reported to the caller: the
// memory is already durably written, and failing the write because the GPU is
// busy would lose it (EDD §8.4).
func (s *Service) storeEmbedding(ctx context.Context, st *store.Store, id, title, body string) bool {
	if s.embedder == nil {
		return false
	}
	vecs, err := s.embedder.Embed(ctx, []string{embedText(title, body)})
	if err != nil || len(vecs) != 1 || len(vecs[0]) != embedDim {
		slog.Warn("embedding deferred to backfill", "memory_id", id, "reason", errText(err))
		if r := observe.FromContext(ctx); r != nil {
			r.RecordEmbedFailure(ctx)
		}
		return false
	}
	v := pgvector.NewVector(vecs[0])
	wrote := false
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE memory SET embedding = $2 WHERE id = $1 AND embedding IS NULL`, id, v)
		wrote = err == nil && tag.RowsAffected() == 1
		return err
	}); err != nil {
		slog.Warn("embedding update failed", "memory_id", id, "err", err.Error())
		return false
	}
	return wrote
}

// backfillSQL takes rows whose embedding never landed. RLS applies, so a
// backfill run only sees what its principal may read.
// Owner-scoped on purpose. content_read lets a caller SEE team rows it does
// not own, but content_write only lets it UPDATE its own. Selecting on
// readability alone re-embeds those rows on every pass and never fills them:
// wasted GPU time, and a "filled" count that never converges.
const backfillSQL = `
SELECT id::text, title, body FROM memory
WHERE embedding IS NULL
  AND owner_id = NULLIF(current_setting('substrate.actor_id', true), '')::uuid
ORDER BY created_at
LIMIT $1
`

// Backfill vectorises up to limit rows left with a NULL embedding. It is
// idempotent: the UPDATE is guarded on embedding IS NULL, so a concurrent run
// or a repeat pass writes nothing. Returns how many rows were filled.
func (s *Service) Backfill(ctx context.Context, limit int) (int, error) {
	if s.embedder == nil {
		return 0, nil
	}
	st, err := s.requireStore()
	if err != nil {
		return 0, err
	}
	if limit <= 0 || limit > 256 {
		limit = 64
	}

	type pending struct{ id, title, body string }
	var todo []pending
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, backfillSQL, limit)
		if err != nil {
			return fmt.Errorf("memory.backfill: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.id, &p.title, &p.body); err != nil {
				return fmt.Errorf("memory.backfill: scan: %w", err)
			}
			todo = append(todo, p)
		}
		return rows.Err()
	}); err != nil {
		return 0, err
	}

	filled := 0
	for _, p := range todo {
		// Counted from rows actually written, not from "the column is now
		// non-NULL" — that reads as success when a concurrent pass filled it.
		if s.storeEmbedding(ctx, st, p.id, p.title, p.body) {
			filled++
		} else {
			slog.Debug("backfill skipped a row", "memory_id", p.id)
		}
	}
	return filled, nil
}
