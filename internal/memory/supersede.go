package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

// Supersede writes a new row, sets superseded_by on the old one, and inserts
// a supersedes edge. The old row is never deleted (Gotcha 6). reason is
// required and stored on audit.
func (s *Service) Supersede(ctx context.Context, in SupersedeIn) (SupersedeOut, error) {
	if strings.TrimSpace(in.Reason) == "" {
		return SupersedeOut{}, fmt.Errorf("memory.supersede: missing reason")
	}
	if strings.TrimSpace(in.OldID) == "" {
		return SupersedeOut{}, fmt.Errorf("memory.supersede: missing old_id")
	}
	oldID, err := uuid.Parse(in.OldID)
	if err != nil {
		return SupersedeOut{}, fmt.Errorf("memory.supersede: old_id: %w", err)
	}
	p := identity.FromContext(ctx)
	if p == nil {
		return SupersedeOut{}, store.ErrNoPrincipal
	}
	st, err := s.requireStore()
	if err != nil {
		return SupersedeOut{}, err
	}

	body := capBody(stripControls(in.Body))
	if err := scanSecrets(body); err != nil {
		return SupersedeOut{}, err
	}
	newID, err := uuid.NewV7()
	if err != nil {
		return SupersedeOut{}, fmt.Errorf("memory.supersede: %w", err)
	}
	reason := strings.TrimSpace(in.Reason)
	reqID := identity.RequestIDFrom(ctx)

	err = st.Tx(ctx, func(tx pgx.Tx) error {
		q := store.New(tx)
		old, err := q.GetMemory(ctx, pgUUID(oldID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("memory.supersede: not found")
			}
			return fmt.Errorf("memory.supersede: %w", err)
		}
		sc, err := pathFromScopeID(ctx, q, old.ScopeID)
		if err != nil {
			return err
		}
		if err := policy.Check("memory.supersede", sc, *p); err != nil {
			return err
		}

		src := old.Source
		var srcMap map[string]any
		if json.Unmarshal(old.Source, &srcMap) == nil {
			srcMap["supersede_reason"] = reason
			if b, err := json.Marshal(srcMap); err == nil {
				src = b
			}
		}

		// Humans inherit the superseded row's trust level so a correction of a
		// confirmed fact stays visible to default reads (MEM-4). Agents still
		// land unverified via decideStatus (Gotcha 4 / MEM-6).
		status := decideStatus(p, string(old.Status))
		ids := extractIdentifiers(body, old.Identifiers)
		if _, err := q.InsertMemory(ctx, store.InsertMemoryParams{
			ID:           pgUUID(newID),
			ScopeID:      old.ScopeID,
			Visibility:   old.Visibility,
			OwnerID:      pgUUID(p.ID),
			Tier:         old.Tier,
			Kind:         old.Kind,
			Title:        old.Title,
			Body:         body,
			Identifiers:  ids,
			Status:       status,
			Source:       src,
			Verification: old.Verification,
			CreatedBy:    pgUUID(p.ID),
		}); err != nil {
			return fmt.Errorf("memory.supersede: insert: %w", err)
		}
		n, err := q.SetMemorySupersededBy(ctx, store.SetMemorySupersededByParams{
			ID:           old.ID,
			SupersededBy: pgUUID(newID),
		})
		if err != nil {
			return fmt.Errorf("memory.supersede: %w", err)
		}
		if n != 1 {
			return fmt.Errorf("memory.supersede: already superseded")
		}
		if err := q.InsertMemoryEdge(ctx, store.InsertMemoryEdgeParams{
			FromID:    pgUUID(newID),
			ToID:      old.ID,
			Relation:  store.EdgeRelationSupersedes,
			CreatedBy: pgUUID(p.ID),
		}); err != nil {
			return fmt.Errorf("memory.supersede: edge: %w", err)
		}
		return q.InsertAudit(ctx, store.InsertAuditParams{
			ActorID:     pgUUID(p.ID),
			Action:      "memory.supersede",
			SubjectType: "memory",
			SubjectID:   pgUUID(newID),
			ScopeID:     old.ScopeID,
			Reason:      &reason,
			RequestID:   strPtr(reqID),
		})
	})
	if err != nil {
		return SupersedeOut{}, err
	}
	return SupersedeOut{ID: newID.String()}, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func pathFromScopeID(ctx context.Context, q *store.Queries, id pgtype.UUID) (scope.Path, error) {
	var segs []scope.Segment
	cur := id
	for cur.Valid {
		row, err := q.GetScope(ctx, cur)
		if err != nil {
			return nil, fmt.Errorf("memory.supersede: scope: %w", err)
		}
		segs = append(segs, scope.Segment{Kind: scope.Kind(row.Kind), Name: row.Key})
		cur = row.ParentID
	}
	for i, j := 0, len(segs)-1; i < j; i, j = i+1, j-1 {
		segs[i], segs[j] = segs[j], segs[i]
	}
	p := scope.Path(segs)
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("memory.supersede: scope: %w", err)
	}
	return p, nil
}
