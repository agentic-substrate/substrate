package compiler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/instruction"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/observe"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/preference"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

var errStore = fmt.Errorf("store not configured")

// Compile builds a context pack for req. Sections 1–3 are never trimmed.
func (s *Service) Compile(ctx context.Context, req Request) (Pack, error) {
	p := identity.FromContext(ctx)
	if err := principalOrErr(p); err != nil {
		return Pack{}, err
	}
	if err := req.Scope.Validate(); err != nil {
		return Pack{}, err
	}
	st, err := s.requireStore()
	if err != nil {
		return Pack{}, err
	}
	budget, err := resolveBudget(req.Budget)
	if err != nil {
		return Pack{}, err
	}

	ctx, span := observe.Start(ctx, observe.SpanCompile)
	defer span.End()

	var d draft
	var chain []uuid.UUID
	err = func() error {
		dbCtx, dbSpan := observe.Start(ctx, observe.SpanDatabase)
		defer dbSpan.End()
		return st.TxChecked(dbCtx, "context.get", req.Scope, func(tx pgx.Tx) error {
			var err error
			d, chain, err = loadDraft(dbCtx, tx, req.Scope, p)
			return err
		})
	}()
	if err != nil {
		return Pack{}, err
	}

	if s.mem != nil && len(req.Files) > 0 {
		q := strings.Join(req.Files, " ")
		out, err := s.mem.Search(ctx, memory.SearchIn{Query: q, Limit: 200})
		if err != nil {
			return Pack{}, err
		}
		hits := filterHits(out.Results, chain)
		mems, err := s.loadMemories(ctx, hits)
		if err != nil {
			return Pack{}, err
		}
		d.Memories = capMemories(mems, MemoryItemCap)
	}

	d, md, err := assemble(d, budget)
	if err != nil {
		return Pack{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Pack{}, fmt.Errorf("pack id: %w", err)
	}
	pack := Pack{
		PackID:   id.String(),
		Markdown: md,
		Sections: sectionEstimates(d),
		Items:    itemsOf(d),
	}
	if Estimate(pack.Markdown) > budget {
		return Pack{}, fmt.Errorf("%w: packed markdown exceeds budget", policy.ErrBudgetTooSmall)
	}
	if r := observe.FromContext(ctx); r != nil {
		for _, s := range pack.Sections {
			r.RecordPackTokens(ctx, s.Name, s.Tokens)
		}
	}
	return pack, nil
}

func loadDraft(ctx context.Context, tx pgx.Tx, p scope.Path, principal *identity.Principal) (draft, []uuid.UUID, error) {
	ins, err := instruction.Resolve(ctx, tx, p)
	if err != nil {
		return draft{}, nil, err
	}
	chain, err := chainIDs(ctx, store.New(tx), p)
	if err != nil {
		return draft{}, nil, err
	}
	overlay, err := overlayScopes(ctx, store.New(tx), principal, chain)
	if err != nil {
		return draft{}, nil, err
	}
	prefs, notes, err := preference.Resolve(ctx, tx, overlay, instruction.Keys(ins))
	if err != nil {
		return draft{}, nil, err
	}

	footIns, err := instructionFooters(ctx, tx, chain)
	if err != nil {
		return draft{}, nil, err
	}
	footPref, err := preferenceFooters(ctx, tx, overlay)
	if err != nil {
		return draft{}, nil, err
	}

	var d draft
	for _, r := range ins {
		f := footIns[r.Scope+"\x00"+r.Key]
		d.Instructions = append(d.Instructions, lineItem{
			Title:  r.Key,
			Body:   r.Body,
			Footer: f,
		})
	}
	for _, r := range prefs {
		f := footPref[r.Scope+"\x00"+r.Key]
		d.Preferences = append(d.Preferences, lineItem{
			Title:  r.Key,
			Body:   r.Body,
			Footer: f,
		})
	}
	for _, n := range notes {
		d.Suppressions = append(d.Suppressions, n.Line())
	}
	mand, err := openReviews(ctx, tx, chain)
	if err != nil {
		return draft{}, nil, err
	}
	d.Mandatory = mand
	skills, err := skillIndex(ctx, tx, chain)
	if err != nil {
		return draft{}, nil, err
	}
	d.Skills = skills
	return d, chain, nil
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

func overlayScopes(ctx context.Context, q *store.Queries, principal *identity.Principal, chain []uuid.UUID) ([]uuid.UUID, error) {
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

func instructionFooters(ctx context.Context, tx pgx.Tx, chain []uuid.UUID) (map[string]footer, error) {
	out := make(map[string]footer)
	if len(chain) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, scope_id::text, key, status::text, updated_at
		FROM instruction
		WHERE status = 'active' AND scope_id = ANY($1::uuid[])`, chain)
	if err != nil {
		return nil, fmt.Errorf("instruction footers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, scopeID, key, status string
		var updated time.Time
		if err := rows.Scan(&id, &scopeID, &key, &status, &updated); err != nil {
			return nil, fmt.Errorf("instruction footers: %w", err)
		}
		out[scopeID+"\x00"+key] = footer{
			ID: id, Status: status, Source: "instruction",
			Verification: "none", LastVerified: formatTime(&updated),
		}
	}
	return out, rows.Err()
}

func preferenceFooters(ctx context.Context, tx pgx.Tx, overlay []uuid.UUID) (map[string]footer, error) {
	out := make(map[string]footer)
	if len(overlay) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, scope_id::text, key, status::text, updated_at
		FROM preference
		WHERE status = 'active' AND scope_id = ANY($1::uuid[])`, overlay)
	if err != nil {
		return nil, fmt.Errorf("preference footers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, scopeID, key, status string
		var updated time.Time
		if err := rows.Scan(&id, &scopeID, &key, &status, &updated); err != nil {
			return nil, fmt.Errorf("preference footers: %w", err)
		}
		out[scopeID+"\x00"+key] = footer{
			ID: id, Status: status, Source: "preference",
			Verification: "none", LastVerified: formatTime(&updated),
		}
	}
	return out, rows.Err()
}

func openReviews(ctx context.Context, tx pgx.Tx, chain []uuid.UUID) ([]lineItem, error) {
	if len(chain) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, kind::text, status::text,
		       COALESCE(payload->>'title', kind::text), created_at
		FROM review_item
		WHERE status = 'open' AND scope_id = ANY($1::uuid[])
		ORDER BY created_at`, chain)
	if err != nil {
		return nil, fmt.Errorf("open reviews: %w", err)
	}
	defer rows.Close()
	var out []lineItem
	for rows.Next() {
		var id, kind, status, title string
		var created time.Time
		if err := rows.Scan(&id, &kind, &status, &title, &created); err != nil {
			return nil, fmt.Errorf("open reviews: %w", err)
		}
		out = append(out, lineItem{
			Title: kind,
			Body:  title,
			Footer: footer{
				ID: id, Status: status, Source: "review",
				Verification: "none", LastVerified: formatTime(&created),
			},
		})
	}
	return out, rows.Err()
}

func skillIndex(ctx context.Context, tx pgx.Tx, chain []uuid.UUID) ([]lineItem, error) {
	if len(chain) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, name, description, created_at
		FROM skill
		WHERE scope_id = ANY($1::uuid[])
		ORDER BY name
		LIMIT 20`, chain)
	if err != nil {
		return nil, fmt.Errorf("skill index: %w", err)
	}
	defer rows.Close()
	var out []lineItem
	for rows.Next() {
		var id, name, desc string
		var created time.Time
		if err := rows.Scan(&id, &name, &desc, &created); err != nil {
			return nil, fmt.Errorf("skill index: %w", err)
		}
		out = append(out, lineItem{
			Title: name,
			Body:  desc,
			Footer: footer{
				ID: id, Status: "active", Source: "skill",
				Verification: "none", LastVerified: formatTime(&created),
			},
		})
	}
	return out, rows.Err()
}

func filterHits(hits []memory.SearchHit, chain []uuid.UUID) []memory.SearchHit {
	if len(hits) == 0 || len(chain) == 0 {
		return nil
	}
	ok := make(map[string]struct{}, len(chain))
	for _, id := range chain {
		ok[id.String()] = struct{}{}
	}
	out := make([]memory.SearchHit, 0, len(hits))
	for _, h := range hits {
		if _, yes := ok[h.Scope]; yes {
			out = append(out, h)
		}
	}
	return out
}

func (s *Service) loadMemories(ctx context.Context, hits []memory.SearchHit) ([]lineItem, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(hits))
	for _, h := range hits {
		id, err := uuid.Parse(h.ID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	byID := make(map[string]lineItem, len(ids))
	err = st.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, title, body, status::text,
			       COALESCE(source->>'machine', ''),
			       COALESCE(verification->>'type', ''),
			       last_verified_at
			FROM memory WHERE id = ANY($1::uuid[])`, ids)
		if err != nil {
			return fmt.Errorf("load memories: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, title, body, status, machine, vtype string
			var last pgtype.Timestamptz
			if err := rows.Scan(&id, &title, &body, &status, &machine, &vtype, &last); err != nil {
				return fmt.Errorf("load memories: %w", err)
			}
			src := "memory"
			if machine != "" {
				src = "machine:" + machine
			}
			lv := "-"
			if last.Valid {
				lv = last.Time.UTC().Format(time.RFC3339)
			}
			byID[id] = lineItem{
				Title: title,
				Body:  body,
				Footer: footer{
					ID: id, Status: status, Source: src,
					Verification: vtype, LastVerified: lv,
				},
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	out := make([]lineItem, 0, len(hits))
	for _, h := range hits {
		if it, ok := byID[h.ID]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func resolveBudget(b *int) (int, error) {
	if b == nil {
		return DefaultBudget(), nil
	}
	if *b <= 0 {
		return 0, fmt.Errorf("%w: budget must be positive", policy.ErrBudgetTooSmall)
	}
	return *b, nil
}
