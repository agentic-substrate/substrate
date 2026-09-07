package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/observe"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

// searchSQL has no visibility predicate: RLS is the backstop (SCOPE-3). A
// service-layer filter here would hide whether the policy is actually doing
// the work.
const searchSQL = `
SELECT m.id::text, m.title, left(m.body, 240), m.status::text, m.scope_id::text,
  (
    -- MEM-3 is enforced structurally, not by a comfortable margin. Every other
    -- term is individually bounded and they sum to at most 125 (40 + 30 + 20 +
    -- 25 + 10), so an exact identifier hit at 1000 cannot be outranked by any
    -- combination of substring, trigram, keyword and semantic scores. Sizing
    -- this at 100 left a non-exact row able to reach 125 and win.
    CASE WHEN $1::text = ANY(m.identifiers) THEN 1000 ELSE 0 END
    + CASE WHEN memory_identifiers_text(m.identifiers) ILIKE '%' || $1::text || '%' THEN 40 ELSE 0 END
    + COALESCE(similarity(memory_identifiers_text(m.identifiers), $1::text), 0) * 30
    -- ts_rank is unbounded above; capped so the non-identifier terms cannot sum
    -- past an exact identifier hit (MEM-3).
    + LEAST(COALESCE(ts_rank(m.fts, plainto_tsquery('english', $1::text)), 0) * 10, 10)
    + CASE WHEN m.body ILIKE '%' || $1::text || '%' OR m.title ILIKE '%' || $1::text || '%' THEN 20 ELSE 0 END
    -- Semantic similarity contributes at most 25. Cosine DISTANCE runs 0..2, so
    -- (1 - distance) runs -1..1: without the GREATEST a dissimilar row would
    -- score down to -25 and reorder the keyword ranking, and without the LEAST
    -- a NaN or out-of-range distance could exceed the cap. Clamped, the ceiling
    -- for a row with no exact identifier match stays below the 100 an exact hit
    -- scores. A NULL query vector (Ollama down, or no embedder configured)
    -- makes the term zero and the ranking keyword-only (MEM-5).
    + LEAST(GREATEST(
        CASE WHEN $6::vector IS NOT NULL AND m.embedding IS NOT NULL
             THEN (1 - (m.embedding <=> $6::vector)) * 25 ELSE 0 END, 0), 25)
  )::float8 AS score
FROM memory m
WHERE ($2::uuid IS NULL OR m.scope_id = $2)
  AND ($3::memory_tier IS NULL OR m.tier = $3)
  AND (
    CASE
      WHEN $4::text[] IS NULL THEN m.status IN ('confirmed','probable') AND m.superseded_by IS NULL
      WHEN 'superseded' = ANY($4::text[]) THEN m.superseded_by IS NOT NULL
      ELSE m.status::text = ANY($4::text[]) AND m.superseded_by IS NULL
    END
  )
ORDER BY score DESC, m.created_at DESC
LIMIT $5
`

// Search ranks by exact identifier, then trigram/FTS. Embeddings are not
// required; a missing backend must not fail the call (MEM-5).
func (s *Service) Search(ctx context.Context, in SearchIn) (SearchOut, error) {
	p := identity.FromContext(ctx)
	if p == nil {
		return SearchOut{}, store.ErrNoPrincipal
	}
	st, err := s.requireStore()
	if err != nil {
		return SearchOut{}, err
	}
	observe.SetScopePath(ctx, in.Scope)
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return SearchOut{}, fmt.Errorf("memory.search: missing query")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var scopeID any
	if strings.TrimSpace(in.Scope) != "" {
		sc, err := scope.Parse(in.Scope)
		if err != nil {
			return SearchOut{}, fmt.Errorf("memory.search: scope: %w", err)
		}
		var id uuid.UUID
		if err := st.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			id, err = resolveScopeID(ctx, store.New(tx), sc)
			return err
		}); err != nil {
			return SearchOut{}, err
		}
		scopeID = id
	}

	tier := any(nil)
	if in.Tier != "" {
		t, err := parseTier(in.Tier)
		if err != nil {
			return SearchOut{}, fmt.Errorf("memory.search: %w", err)
		}
		tier = string(t)
	} else {
		tier = string(store.MemoryTierSemantic)
	}

	var statuses any
	if in.Status != nil {
		statuses = in.Status
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Best-effort: nil means keyword-only. Computed before the transaction so a
	// slow embed backend never holds a database connection open.
	qv := s.vectorFor(ctx, q)

	var out SearchOut
	err = st.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, searchSQL, q, scopeID, tier, statuses, limit, qv)
		if err != nil {
			return fmt.Errorf("memory.search: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var h SearchHit
			if err := rows.Scan(&h.ID, &h.Title, &h.Snippet, &h.Status, &h.Scope, &h.Score); err != nil {
				return fmt.Errorf("memory.search: %w", err)
			}
			out.Results = append(out.Results, h)
		}
		return rows.Err()
	})
	if err != nil {
		return SearchOut{}, err
	}
	if out.Results == nil {
		out.Results = []SearchHit{}
	}
	return out, nil
}
