package compiler

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/mcpx"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

// GetIn is the context.get input (EDD §4.1).
type GetIn struct {
	TaskID       string   `json:"task_id,omitempty" jsonschema:"task scope id"`
	Repo         string   `json:"repo,omitempty" jsonschema:"repo remote or repo scope key"`
	Branch       string   `json:"branch,omitempty" jsonschema:"branch name"`
	Files        []string `json:"files,omitempty" jsonschema:"touched or relevant paths"`
	BudgetTokens *int     `json:"budget_tokens,omitempty" jsonschema:"overall token budget; omitted uses the documented section sum, zero or negative is too small"`
}

// GetOut is the context.get result (EDD §4.1).
type GetOut struct {
	PackID   string    `json:"pack_id" jsonschema:"id of this pack"`
	Markdown string    `json:"markdown" jsonschema:"rendered pack"`
	Sections []Section `json:"sections" jsonschema:"per-section token estimates"`
	Items    []Item    `json:"items" jsonschema:"items included in the pack"`
}

// Register attaches context.get.
func Register(s *mcpx.Server, getStore func() *store.Store, mem *memory.Service) *Service {
	svc := New(getStore, mem)
	mcpx.AddTool(s, &mcp.Tool{
		Name:        "context.get",
		Description: "Compile the effective context pack for this session: instructions, preferences, mandatory items, retrieved memories, and the skill index. Call at session start. Instructions are never trimmed.",
	}, svc.getTool)
	return svc
}

func (s *Service) getTool(ctx context.Context, _ *mcp.CallToolRequest, in GetIn) (*mcp.CallToolResult, GetOut, error) {
	out, err := s.Get(ctx, in)
	return nil, out, err
}

// Get resolves repo/branch/task to a scope path and compiles the pack.
func (s *Service) Get(ctx context.Context, in GetIn) (GetOut, error) {
	if identity.FromContext(ctx) == nil {
		return GetOut{}, store.ErrNoPrincipal
	}
	st, err := s.requireStore()
	if err != nil {
		return GetOut{}, err
	}
	var path scope.Path
	err = st.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		path, err = resolvePath(ctx, tx, in)
		return err
	})
	if err != nil {
		return GetOut{}, err
	}
	var taskID *uuid.UUID
	if in.TaskID != "" {
		id, err := uuid.Parse(in.TaskID)
		if err != nil {
			return GetOut{}, fmt.Errorf("%w: task_id", policy.ErrDeniedScope)
		}
		taskID = &id
	}
	pack, err := s.Compile(ctx, Request{
		Scope:  path,
		TaskID: taskID,
		Files:  in.Files,
		Budget: in.BudgetTokens,
	})
	if err != nil {
		return GetOut{}, err
	}
	return GetOut(pack), nil
}

func resolvePath(ctx context.Context, tx pgx.Tx, in GetIn) (scope.Path, error) {
	q := store.New(tx)
	var leaf pgtype.UUID
	switch {
	case in.TaskID != "":
		id, err := uuid.Parse(in.TaskID)
		if err != nil {
			return nil, fmt.Errorf("%w: task_id", policy.ErrDeniedScope)
		}
		leaf = pgtype.UUID{Bytes: id, Valid: true}
	case in.Repo != "":
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM scope WHERE kind = 'repo' AND key = $1 LIMIT 1`, in.Repo).Scan(&id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: repo not found", policy.ErrDeniedScope)
			}
			return nil, fmt.Errorf("lookup repo: %w", err)
		}
		leaf = pgtype.UUID{Bytes: id, Valid: true}
		if in.Branch != "" {
			row, err := q.LookupScope(ctx, store.LookupScopeParams{
				Kind:     store.ScopeKindBranch,
				Key:      in.Branch,
				ParentID: leaf,
			})
			if err == nil {
				leaf = row.ID
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("lookup branch: %w", err)
			}
		}
	default:
		return nil, fmt.Errorf("%w: repo, branch, or task_id required", policy.ErrDeniedScope)
	}
	return pathFromLeaf(ctx, q, leaf)
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
