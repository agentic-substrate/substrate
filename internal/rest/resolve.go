package rest

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

func resolveRepoPaths(ctx context.Context, tx pgx.Tx, repos []string) ([]scope.Path, error) {
	if len(repos) == 0 {
		return nil, nil
	}
	q := store.New(tx)
	out := make([]scope.Path, 0, len(repos))
	for _, key := range repos {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM scope WHERE kind = 'repo' AND key = $1 LIMIT 1`, key).Scan(&id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: repo not found", policyDenied(key))
			}
			return nil, fmt.Errorf("lookup repo %s: %w", key, err)
		}
		path, err := pathFromLeaf(ctx, q, pgtype.UUID{Bytes: id, Valid: true})
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

func policyDenied(key string) error {
	return fmt.Errorf("%w: repo %s", policy.ErrDeniedScope, key)
}

func globalPath(ctx context.Context, tx pgx.Tx) (scope.Path, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM scope WHERE kind = 'global' LIMIT 1`).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("lookup global: %w", err)
	}
	return pathFromLeaf(ctx, store.New(tx), pgtype.UUID{Bytes: id, Valid: true})
}

func pathFromLeaf(ctx context.Context, q *store.Queries, leaf pgtype.UUID) (scope.Path, error) {
	var segs []scope.Segment
	id := leaf
	for id.Valid {
		row, err := q.GetScope(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("walk scope: %w", err)
		}
		segs = append(segs, scope.Segment{Kind: scope.Kind(row.Kind), Name: row.Key})
		id = row.ParentID
	}
	for i, j := 0, len(segs)-1; i < j; i, j = i+1, j-1 {
		segs[i], segs[j] = segs[j], segs[i]
	}
	p := scope.Path(segs)
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func chainIDs(ctx context.Context, q *store.Queries, p scope.Path) ([]uuid.UUID, error) {
	var parent pgtype.UUID
	ids := make([]uuid.UUID, 0, len(p))
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
			return nil, fmt.Errorf("lookup scope %s: %w", p[:i+1].String(), err)
		}
		ids = append(ids, uuid.UUID(row.ID.Bytes))
		parent = row.ID
	}
	return ids, nil
}

func overlayScopes(ctx context.Context, q *store.Queries, principal *identity.Principal, path scope.Path) ([]uuid.UUID, error) {
	chain, err := chainIDs(ctx, q, path)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, 3)
	if principal != nil {
		row, err := q.LookupScope(ctx, store.LookupScopeParams{
			Kind:     store.ScopeKindUser,
			Key:      principal.ID.String(),
			ParentID: pgtype.UUID{},
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("lookup user scope: %w", err)
		}
		if err == nil && row.ID.Valid {
			out = append(out, uuid.UUID(row.ID.Bytes))
		}
	}
	var team, org uuid.UUID
	for _, id := range chain {
		row, err := q.GetScope(ctx, pgtype.UUID{Bytes: id, Valid: true})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("get scope: %w", err)
		}
		switch row.Kind {
		case store.ScopeKindOrg:
			org = uuid.UUID(row.ID.Bytes)
		case store.ScopeKindTeam:
			team = uuid.UUID(row.ID.Bytes)
		}
	}
	if team != uuid.Nil {
		out = append(out, team)
	}
	if org != uuid.Nil {
		out = append(out, org)
	}
	return out, nil
}

func resolveScopeID(ctx context.Context, q *store.Queries, p scope.Path) (uuid.UUID, error) {
	var parent pgtype.UUID
	var last uuid.UUID
	for i := range p {
		row, err := q.LookupScope(ctx, store.LookupScopeParams{
			Kind:     store.ScopeKind(p[i].Kind),
			Key:      p[i].Name,
			ParentID: parent,
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("unknown scope %s: %w", p[:i+1].String(), err)
		}
		last = uuid.UUID(row.ID.Bytes)
		parent = row.ID
	}
	return last, nil
}
