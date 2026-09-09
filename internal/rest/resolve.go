package rest

import (
	"context"
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

// resolveRepoPaths maps repo keys to the chains the server bound them to, and
// refuses any key this principal may not read.
//
// The scope_readable check is the gate and it lives here, next to the lookup,
// because the `scope` table carries no RLS and policy.Check only asserts that a
// repo-leaf principal has *some* team. This is the write-path resolution: a
// caller that names a repo it cannot reach gets the same policyDenied a missing
// key gets, so maskRepoDenial collapses the two into one 403 and the status code
// cannot be used to enumerate the repo keys this control plane binds (#109).
//
// The read endpoints must not refuse — they use resolveReadableRepoPaths.
//
// It returns the leaf id alongside the chain so the caller can gate on the very
// row the chain was walked from. Looking the key up a second time is not
// equivalent: `scope` is UNIQUE NULLS NOT DISTINCT (parent_id, kind, key)
// (migrations/00002_schema.sql:38), so one key may be bound under two parents,
// and under READ COMMITTED a second statement takes a fresh snapshot — a
// concurrent bind committed between the two lookups flips an unordered LIMIT 1
// onto the other row (#110).
func resolveRepoPaths(ctx context.Context, tx pgx.Tx, repos []string) ([]repoRef, error) {
	refs, ok, err := resolveRepoRefs(ctx, tx, repos)
	if err != nil {
		return nil, err
	}
	if !ok {
		// Deliberately the same error whether the key is foreign or absent.
		return nil, policyDenied(strings.Join(repos, ","))
	}
	return refs, nil
}

// repoRef pairs a resolved repo leaf with the chain walked from it. The id and
// the path must travel together: every later decision about this repo has to be
// made against one row, not against whatever a fresh lookup of the key returns.
type repoRef struct {
	ID   uuid.UUID
	Path scope.Path
}

// resolveReadableRepoPaths maps repo keys to their chains for the read paths.
//
// It reports ok=false — never a distinguishable error — when any requested key
// does not resolve to a chain this principal may read, whether because the key
// is bound to another team or because it is not bound at all. The two cases are
// indistinguishable to the caller because the caller is told nothing: the read
// handlers fall back to the global chain and answer 200 with exactly the body
// the same request would produce with no `?repos=` at all (#109).
//
// Falling back rather than refusing keeps `visibility='global'` content — an
// explicit authoring decision that a row is readable by everyone — reaching the
// readers it was published for, which a 403 would have taken away. The cost is
// that a member who typos their own repo key silently receives global content
// instead of an error; that is accepted, because any signal distinguishing
// "your key did not resolve" from "you asked for nothing" reopens the very
// oracle this closes for the caller who is probing rather than typing.
func resolveReadableRepoPaths(ctx context.Context, tx pgx.Tx, repos []string) ([]scope.Path, bool, error) {
	refs, ok, err := resolveRepoRefs(ctx, tx, repos)
	if err != nil || !ok {
		return nil, ok, err
	}
	paths := make([]scope.Path, 0, len(refs))
	for _, r := range refs {
		paths = append(paths, r.Path)
	}
	return paths, true, nil
}

// resolveRepoRefs is the single lookup both resolvers share.
func resolveRepoRefs(ctx context.Context, tx pgx.Tx, repos []string) ([]repoRef, bool, error) {
	if len(repos) == 0 {
		return nil, true, nil
	}
	q := store.New(tx)
	out := make([]repoRef, 0, len(repos))
	for _, key := range repos {
		var id uuid.UUID
		var readable bool
		err := tx.QueryRow(ctx, `SELECT id, scope_readable(id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid)
                        FROM scope WHERE kind = 'repo' AND key = $1 LIMIT 1`, key).Scan(&id, &readable)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("lookup repo %s: %w", key, err)
		}
		if !readable {
			return nil, false, nil
		}
		path, err := pathFromLeaf(ctx, q, pgtype.UUID{Bytes: id, Valid: true})
		if err != nil {
			return nil, false, err
		}
		out = append(out, repoRef{ID: id, Path: path})
	}
	return out, true, nil
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
