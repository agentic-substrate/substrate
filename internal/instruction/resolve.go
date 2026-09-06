// Package instruction resolves active instructions along the scope chain.
// Resolution is deterministic: most-specific-first, first active per key wins.
// Instructions are never scored, ranked, trimmed, or budgeted (INST-1, Gotcha 3).
package instruction

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

// Record is one instruction considered during resolution. Scope is the
// Path.String() of the row's scope (or a scope id once loaded from the store).
type Record struct {
	Scope string
	Kind  string
	Key   string
	Body  string
}

// Walk visits ancestor scopes in the given order and keeps the first record
// per key. Distinct keys accumulate. ancestorKeys must be most-specific-first
// (scope.Path.Ancestors); reversing that order silently picks the global value.
func Walk(ancestorKeys []string, records []Record) []Record {
	byScope := make(map[string][]Record, len(ancestorKeys))
	for _, r := range records {
		byScope[r.Scope] = append(byScope[r.Scope], r)
	}
	seen := make(map[string]struct{})
	out := make([]Record, 0, len(records))
	for _, s := range ancestorKeys {
		for _, r := range byScope[s] {
			if _, ok := seen[r.Key]; ok {
				continue
			}
			seen[r.Key] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}

// Keys returns the set of keys in recs. Preferences consult this set so a
// user cannot override an effective instruction (INST-3).
func Keys(recs []Record) map[string]struct{} {
	m := make(map[string]struct{}, len(recs))
	for _, r := range recs {
		m[r.Key] = struct{}{}
	}
	return m
}

// Resolve loads active instructions on p's ancestor scopes through tx.
// tx must already carry substrate.* session settings (store.Tx); a bare
// connection returns zero rows rather than erroring.
func Resolve(ctx context.Context, tx pgx.Tx, p scope.Path) ([]Record, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	q := store.New(tx)
	ids, ancestorKeys, err := lookupChain(ctx, q, p)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.ListActiveInstructions(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list active instructions: %w", err)
	}
	recs := make([]Record, 0, len(rows))
	for _, r := range rows {
		recs = append(recs, Record{
			Scope: uuidString(r.ScopeID),
			Kind:  string(r.Kind),
			Key:   r.Key,
			Body:  r.Body,
		})
	}
	return Walk(ancestorKeys, recs), nil
}

func lookupChain(ctx context.Context, q *store.Queries, p scope.Path) ([]pgtype.UUID, []string, error) {
	var parent pgtype.UUID
	idsLeastFirst := make([]pgtype.UUID, 0, len(p))
	keysLeastFirst := make([]string, 0, len(p))
	for i := range p {
		row, err := q.LookupScope(ctx, store.LookupScopeParams{
			Kind:     store.ScopeKind(p[i].Kind),
			Key:      p[i].Name,
			ParentID: parent,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, nil, fmt.Errorf("lookup scope %s: %w", p[:i+1].String(), err)
		}
		idsLeastFirst = append(idsLeastFirst, row.ID)
		keysLeastFirst = append(keysLeastFirst, uuidString(row.ID))
		parent = row.ID
	}
	n := len(idsLeastFirst)
	mostSpecificFirst := make([]string, n)
	for i, k := range keysLeastFirst {
		mostSpecificFirst[n-1-i] = k
	}
	return idsLeastFirst, mostSpecificFirst, nil
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
