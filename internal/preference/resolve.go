// Package preference resolves the user > team > org overlay. A preference is
// emitted only when no effective instruction claims the same key; a suppressed
// preference is reported in exactly one line (INST-3).
package preference

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/store"
)

// Record is one preference considered during resolution. Scope is a label
// used to group rows (user / team / org, or a scope id from the store).
type Record struct {
	Scope string
	Key   string
	Body  string
}

// Suppression is a preference that lost to an instruction. Line is the
// one-line note the pack must emit (INST-3).
type Suppression struct {
	Key  string
	Body string
}

// Line is the single reported note for a suppressed preference.
func (s Suppression) Line() string {
	return fmt.Sprintf("preference %s=%s suppressed by instruction", s.Key, s.Body)
}

// Walk visits ordered (user, then teams, then org) and keeps the first
// unclaimed preference per key. claimed is the effective instruction key set.
func Walk(ordered []Record, claimed map[string]struct{}) ([]Record, []Suppression) {
	prefSeen := make(map[string]struct{})
	out := make([]Record, 0, len(ordered))
	notes := make([]Suppression, 0)
	for _, p := range ordered {
		if _, taken := claimed[p.Key]; taken {
			if _, already := prefSeen[p.Key]; !already {
				notes = append(notes, Suppression{Key: p.Key, Body: p.Body})
				prefSeen[p.Key] = struct{}{}
			}
			continue
		}
		if _, ok := prefSeen[p.Key]; ok {
			continue
		}
		prefSeen[p.Key] = struct{}{}
		out = append(out, p)
	}
	return out, notes
}

// Resolve loads active preferences on overlayScopes in caller order
// (user, then teams, then org). claimed is the effective instruction key set.
// tx must already carry substrate.* session settings (store.Tx).
func Resolve(ctx context.Context, tx pgx.Tx, overlayScopes []uuid.UUID, claimed map[string]struct{}) ([]Record, []Suppression, error) {
	if len(overlayScopes) == 0 {
		return nil, nil, nil
	}
	ids := make([]pgtype.UUID, len(overlayScopes))
	for i, id := range overlayScopes {
		ids[i] = pgtype.UUID{Bytes: id, Valid: true}
	}
	rows, err := store.New(tx).ListActivePreferences(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("list active preferences: %w", err)
	}
	byScope := make(map[string][]Record, len(overlayScopes))
	for _, r := range rows {
		sid := uuidString(r.ScopeID)
		byScope[sid] = append(byScope[sid], Record{Scope: sid, Key: r.Key, Body: r.Body})
	}
	ordered := make([]Record, 0, len(rows))
	for _, id := range overlayScopes {
		ordered = append(ordered, byScope[id.String()]...)
	}
	prefs, notes := Walk(ordered, claimed)
	return prefs, notes, nil
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}
