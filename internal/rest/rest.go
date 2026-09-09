package rest

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/instruction"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/observe"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/preference"
	"github.com/agentic-substrate/substrate/internal/render"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

const (
	maxBodyBytes  = 1 << 20 // 1 MiB
	maxBatchItems = 50      // EDD §7.2
)

// gitHealthVar is the R27 metric: Git reachability lives here, never on /readyz.
var gitHealthVar = expvar.NewString("substrate_git_health")

// GitChecker reports skills-repo reachability. /readyz must never call it
// (EDD R27): a Git outage must not take the process out of rotation.
type GitChecker interface {
	Check(ctx context.Context) error
}

// Options wires the /v1 surface.
type Options struct {
	Store   func() *store.Store
	Memory  *memory.Service
	Git     GitChecker
	Now     func() time.Time
	Observe *observe.Runtime
}

// Handler serves /v1 for the adapter and CLI.
type Handler struct {
	getStore func() *store.Store
	mem      *memory.Service
	git      GitChecker
	now      func() time.Time
	hub      *hub
	obs      *observe.Runtime
}

func (h *Handler) store() *store.Store {
	if h == nil || h.getStore == nil {
		return nil
	}
	return h.getStore()
}

// New constructs a /v1 handler. Close releases the LISTEN connection.
func New(opts Options) *Handler {
	getStore := opts.Store
	if getStore == nil {
		getStore = func() *store.Store { return nil }
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Handler{
		getStore: getStore,
		mem:      opts.Memory,
		git:      opts.Git,
		now:      now,
		hub:      newHub(eventBuffer),
		obs:      opts.Observe,
	}
}

// Close stops the audit LISTEN loop.
func (h *Handler) Close() {
	if h != nil && h.hub != nil {
		h.hub.stop()
	}
}

// WaitListening blocks until LISTEN substrate_audit is armed.
func (h *Handler) WaitListening(d time.Duration) error {
	if h == nil || h.hub == nil {
		return fmt.Errorf("LISTEN substrate_audit: handler not started")
	}
	h.ensureListen()
	return h.hub.waitReady(d)
}

// Mount registers the /v1 routes on mux. Auth is the caller's middleware.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/render", h.render)
	mux.HandleFunc("GET /v1/skills/manifest", h.skillsManifest)
	mux.HandleFunc("POST /v1/memory/batch", h.memoryBatch)
	mux.HandleFunc("GET /v1/memory/cache", h.memoryCache)
	mux.HandleFunc("POST /v1/review", h.reviewCreate)
	mux.HandleFunc("GET /v1/review", h.reviewList)
	mux.HandleFunc("POST /v1/review/{id}/decide", h.reviewDecide)
	mux.HandleFunc("GET /v1/events", h.events)
	mux.HandleFunc("GET /v1/health/git", h.gitHealth)
	mux.HandleFunc("POST /v1/import", h.importApply)
}

type renderTarget struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type batchResult struct {
	ClientID  string `json:"client_id"`
	SubjectID string `json:"subject_id"`
	Duplicate bool   `json:"duplicate"`
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request) {
	st, err := h.requireStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	repos := splitCSV(r.URL.Query().Get("repos"))
	cfg, _, err := h.effectiveConfig(r.Context(), st, repos)
	if err != nil {
		writePolicy(w, err)
		return
	}
	targets := make([]renderTarget, 0, 5)
	for _, spec := range render.Specs() {
		content, err := render.Render(spec.Target, cfg)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		sum := render.DriftHash(content)
		for _, path := range spec.Paths {
			targets = append(targets, renderTarget{Path: path, Content: content, SHA256: sum})
		}
	}
	// The chain these targets were compiled for is deliberately NOT returned.
	// The `scope` table carries no RLS and a repo key resolves for anyone, so
	// echoing the chain would hand a non-member the org, team and project
	// names of a repo it may not read. A drift proposal names the repo key the
	// adapter already has and POST /v1/review re-derives the chain server-side
	// (#58), so no client ever needs to be told one.
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}

func (h *Handler) effectiveConfig(ctx context.Context, st *store.Store, repos []string) (render.EffectiveConfig, scope.Path, error) {
	p := identity.FromContext(ctx)
	if p == nil {
		return render.EffectiveConfig{}, nil, store.ErrNoPrincipal
	}
	var cfg render.EffectiveConfig
	cfg.GeneratedAt = h.now().UTC().Format(time.RFC3339)
	var used scope.Path
	err := st.Tx(ctx, func(tx pgx.Tx) error {
		paths, err := resolveRepoPaths(ctx, tx, repos)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			g, err := globalPath(ctx, tx)
			if err != nil {
				return err
			}
			paths = []scope.Path{g}
		}
		used = paths[0]
		for _, path := range paths {
			if err := policy.Check("render", path, *p); err != nil {
				return err
			}
		}
		seenIns := map[string]struct{}{}
		var ins []instruction.Record
		skillSeen := map[string]struct{}{}
		var skills []render.Skill
		q := store.New(tx)
		for _, path := range paths {
			got, err := instruction.Resolve(ctx, tx, path)
			if err != nil {
				return err
			}
			for _, rec := range got {
				if _, ok := seenIns[rec.Key]; ok {
					continue
				}
				seenIns[rec.Key] = struct{}{}
				ins = append(ins, rec)
			}
			chain, err := chainIDs(ctx, q, path)
			if err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `SELECT name, description FROM skill WHERE scope_id = ANY($1::uuid[]) ORDER BY name`, chain)
			if err != nil {
				return fmt.Errorf("skills: %w", err)
			}
			for rows.Next() {
				var name, desc string
				if err := rows.Scan(&name, &desc); err != nil {
					rows.Close()
					return err
				}
				if _, ok := skillSeen[name]; ok {
					continue
				}
				skillSeen[name] = struct{}{}
				skills = append(skills, render.Skill{Name: name, Description: desc})
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		overlay, err := overlayScopes(ctx, q, p, used)
		if err != nil {
			return err
		}
		prefs, notes, err := preference.Resolve(ctx, tx, overlay, instruction.Keys(ins))
		if err != nil {
			return err
		}
		cfg.Instructions = ins
		cfg.Preferences = prefs
		cfg.Suppressed = notes
		cfg.Skills = skills
		return nil
	})
	return cfg, used, err
}

func (h *Handler) skillsManifest(w http.ResponseWriter, r *http.Request) {
	st, err := h.requireStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	p := identity.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, store.ErrNoPrincipal)
		return
	}
	repos := splitCSV(r.URL.Query().Get("repos"))
	type skill struct {
		Name    string `json:"name"`
		GitPath string `json:"git_path"`
		GitSHA  string `json:"git_sha"`
	}
	var out []skill
	err = st.Tx(r.Context(), func(tx pgx.Tx) error {
		var paths []scope.Path
		if len(repos) > 0 {
			var err error
			paths, err = resolveRepoPaths(r.Context(), tx, repos)
			if err != nil {
				return err
			}
			for _, path := range paths {
				if err := policy.Check("skills.manifest", path, *p); err != nil {
					return err
				}
			}
		}
		if len(paths) == 0 {
			g, err := globalPath(r.Context(), tx)
			if err != nil {
				return err
			}
			paths = []scope.Path{g}
		}
		q := store.New(tx)
		ids := make([]pgtype.UUID, 0)
		seen := map[uuid.UUID]struct{}{}
		for _, path := range paths {
			chain, err := chainIDs(r.Context(), q, path)
			if err != nil {
				return err
			}
			for _, id := range chain {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				ids = append(ids, pgtype.UUID{Bytes: id, Valid: true})
			}
		}
		rows, err := q.ListApprovedSkills(r.Context(), ids)
		if err != nil {
			return err
		}
		for _, row := range rows {
			out = append(out, skill{Name: row.Name, GitPath: row.GitPath, GitSHA: row.GitSha})
		}
		return nil
	})
	if err != nil {
		writePolicy(w, err)
		return
	}
	if out == nil {
		out = []skill{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

func (h *Handler) memoryBatch(w http.ResponseWriter, r *http.Request) {
	if h.mem == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("store not configured"))
		return
	}
	var items []memory.BatchItem
	if err := decodeJSONBody(w, r, &items); err != nil {
		writeDecodeErr(w, err)
		return
	}
	if len(items) > maxBatchItems {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("memory.batch: at most %d items", maxBatchItems))
		return
	}
	// An item about a checkout names its repo key and nothing else, exactly as
	// POST /v1/review does (#58): the server owns the repo-key-to-chain
	// binding, so a PostToolUse shim can neither invent a chain nor be told
	// one. The resolved chain is used to place the row and is never returned.
	anyRepo := false
	for i := range items {
		if items[i].Repo == "" {
			continue
		}
		anyRepo = true
		if items[i].Scope != "" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("memory.batch: scope and repo are mutually exclusive"))
			return
		}
		st, serr := h.requireStore()
		if serr != nil {
			writeErr(w, http.StatusServiceUnavailable, serr)
			return
		}
		sc, rerr := h.repoScope(r.Context(), st, items[i].Repo)
		if rerr != nil {
			writePolicy(w, maskRepoDenial(rerr, true))
			return
		}
		items[i].Scope = sc.String()
		items[i].Repo = ""
	}
	out, err := h.mem.Batch(r.Context(), items)
	if err != nil {
		writePolicy(w, maskRepoDenial(err, anyRepo))
		return
	}
	results := make([]batchResult, len(out))
	for i, r := range out {
		results[i] = batchResult{ClientID: r.ClientID, SubjectID: r.SubjectID, Duplicate: r.Duplicate}
	}
	writeJSON(w, http.StatusOK, results)
}

func (h *Handler) memoryCache(w http.ResponseWriter, r *http.Request) {
	st, err := h.requireStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	p := identity.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, store.ErrNoPrincipal)
		return
	}
	since := time.Time{}
	if raw := r.URL.Query().Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("since: %w", err))
			return
		}
		since = t
	}
	repos := splitCSV(r.URL.Query().Get("repos"))
	type mem struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Body        string    `json:"body"`
		Identifiers []string  `json:"identifiers"`
		Status      string    `json:"status"`
		UpdatedAt   time.Time `json:"updated_at"`
	}
	var out []mem
	err = st.Tx(r.Context(), func(tx pgx.Tx) error {
		allow := map[uuid.UUID]struct{}{}
		if len(repos) > 0 {
			paths, err := resolveRepoPaths(r.Context(), tx, repos)
			if err != nil {
				return err
			}
			q := store.New(tx)
			for _, path := range paths {
				if err := policy.Check("memory.cache", path, *p); err != nil {
					return err
				}
				ids, err := chainIDs(r.Context(), q, path)
				if err != nil {
					return err
				}
				for _, id := range ids {
					allow[id] = struct{}{}
				}
			}
		}
		rows, err := store.New(tx).ListMemoryCache(r.Context(), pgtype.Timestamptz{Time: since, Valid: true})
		if err != nil {
			return err
		}
		for _, row := range rows {
			sid := uuid.UUID(row.ScopeID.Bytes)
			if len(allow) > 0 {
				if _, ok := allow[sid]; !ok {
					continue
				}
			}
			updated := time.Time{}
			if row.UpdatedAt.Valid {
				updated = row.UpdatedAt.Time
			}
			out = append(out, mem{
				ID:          uuid.UUID(row.ID.Bytes).String(),
				Title:       row.Title,
				Body:        row.Body,
				Identifiers: row.Identifiers,
				Status:      string(row.Status),
				UpdatedAt:   updated,
			})
		}
		return nil
	})
	if err != nil {
		writePolicy(w, err)
		return
	}
	if out == nil {
		out = []mem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": out})
}

func (h *Handler) reviewCreate(w http.ResponseWriter, r *http.Request) {
	st, err := h.requireStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	p := identity.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, store.ErrNoPrincipal)
		return
	}
	var in struct {
		Kind    string          `json:"kind"`
		Scope   string          `json:"scope"`
		Repo    string          `json:"repo"`
		TeamID  string          `json:"team_id"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := decodeJSONBody(w, r, &in); err != nil {
		writeDecodeErr(w, err)
		return
	}
	// A proposal about a checkout names the repo key and nothing else. The
	// server owns the binding from a repo key to its chain, so the caller
	// cannot file a row at a chain it invented or was told (#58). `scope` and
	// `repo` are mutually exclusive: accepting both would let a caller name a
	// repo and still choose the chain.
	if in.Repo != "" && in.Scope != "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("scope and repo are mutually exclusive"))
		return
	}
	var sc scope.Path
	if in.Repo != "" {
		sc, err = h.repoScope(r.Context(), st, in.Repo)
		err = maskRepoDenial(err, true)
	} else {
		sc, err = scope.Parse(in.Scope)
	}
	if err != nil {
		if in.Repo != "" {
			writePolicy(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	kind := store.ReviewKind(in.Kind)
	switch kind {
	case store.ReviewKindPromotion, store.ReviewKindInstructionChange, store.ReviewKindSkillProposal,
		store.ReviewKindContradiction, store.ReviewKindDriftProposal, store.ReviewKindImportConflict:
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown review kind %q", in.Kind))
		return
	}
	teamID := uuid.Nil
	if in.TeamID != "" {
		teamID, err = uuid.Parse(in.TeamID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("team_id: %w", err))
			return
		}
	} else if len(p.TeamIDs) > 0 {
		teamID = p.TeamIDs[0]
	}
	if len(in.Payload) == 0 {
		in.Payload = []byte(`{}`)
	}
	id, err := uuid.NewV7()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var row store.InsertReviewItemRow
	err = st.TxChecked(r.Context(), "review.create", sc, func(tx pgx.Tx) error {
		q := store.New(tx)
		scopeID, err := resolveScopeID(r.Context(), q, sc)
		if err != nil {
			return err
		}
		row, err = q.InsertReviewItem(r.Context(), store.InsertReviewItemParams{
			ID:         pgUUID(id),
			Kind:       kind,
			ScopeID:    pgUUID(scopeID),
			TeamID:     pgUUID(teamID),
			Payload:    in.Payload,
			ProposedBy: pgUUID(p.ID),
		})
		return err
	})
	if err != nil {
		writePolicy(w, maskRepoDenial(err, in.Repo != ""))
		return
	}
	h.recordReviewOpen(r.Context())
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     uuid.UUID(row.ID.Bytes).String(),
		"kind":   string(row.Kind),
		"status": string(row.Status),
	})
}

// repoScope resolves a repo key to the chain the server bound it to. The
// result is used to place a row, never returned to the caller: resolveRepoPaths
// reads the RLS-free `scope` table, so handing the chain back would disclose
// another team's naming.
//
// The scope_writable check is the gate, and it must live here rather than at
// the write. For a repo leaf policy.Check only asserts the principal has some
// team, so the real refusal for a foreign repo used to be RLS 42501 raised by
// the INSERT. A caller that asks for a preview and never writes never reaches
// that INSERT, so resolving alone would answer "this key exists here" with a
// 200 and turn the endpoint into a repo-name enumeration oracle. Refusing at
// resolve time makes a foreign repo and a nonexistent one indistinguishable
// whether or not the request goes on to write anything.
func (h *Handler) repoScope(ctx context.Context, st *store.Store, key string) (scope.Path, error) {
	var out scope.Path
	err := st.Tx(ctx, func(tx pgx.Tx) error {
		paths, err := resolveRepoPaths(ctx, tx, []string{key})
		if err != nil {
			return err
		}
		if len(paths) != 1 {
			return policyDenied(key)
		}
		var writable bool
		if err := tx.QueryRow(ctx, `SELECT scope_writable(id, NULLIF(current_setting('substrate.actor_id', true), '')::uuid)
                        FROM scope WHERE kind = 'repo' AND key = $1 LIMIT 1`, key).Scan(&writable); err != nil {
			return fmt.Errorf("repo writability %s: %w", key, err)
		}
		if !writable {
			return policyDenied(key)
		}
		out = paths[0]
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) reviewList(w http.ResponseWriter, r *http.Request) {
	st, err := h.requireStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	if identity.FromContext(r.Context()) == nil {
		writeErr(w, http.StatusUnauthorized, store.ErrNoPrincipal)
		return
	}
	var team pgtype.UUID
	if raw := r.URL.Query().Get("team"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("team: %w", err))
			return
		}
		team = pgUUID(id)
	}
	var status *string
	if raw := r.URL.Query().Get("status"); raw != "" {
		status = &raw
	}
	type item struct {
		ID      string          `json:"id"`
		Kind    string          `json:"kind"`
		Status  string          `json:"status"`
		Payload json.RawMessage `json:"payload"`
		TeamID  string          `json:"team_id,omitempty"`
	}
	var items []item
	err = st.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := store.New(tx).ListReviewItems(r.Context(), store.ListReviewItemsParams{
			TeamID: team,
			Status: status,
		})
		if err != nil {
			return err
		}
		for _, row := range rows {
			it := item{
				ID:      uuid.UUID(row.ID.Bytes).String(),
				Kind:    string(row.Kind),
				Status:  string(row.Status),
				Payload: row.Payload,
			}
			if row.TeamID.Valid {
				it.TeamID = uuid.UUID(row.TeamID.Bytes).String()
			}
			items = append(items, it)
		}
		return nil
	})
	if err != nil {
		writePolicy(w, err)
		return
	}
	if items == nil {
		items = []item{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) reviewDecide(w http.ResponseWriter, r *http.Request) {
	st, err := h.requireStore()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	p := identity.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, store.ErrNoPrincipal)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("id: %w", err))
		return
	}
	var in decideInput
	if err := decodeJSONBody(w, r, &in); err != nil {
		writeDecodeErr(w, err)
		return
	}
	var status store.ReviewStatus
	switch in.Decision {
	case "approved":
		status = store.ReviewStatusApproved
	case "rejected":
		status = store.ReviewStatusRejected
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("decision must be approved or rejected"))
		return
	}
	if strings.TrimSpace(in.Reason) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("reason is required"))
		return
	}
	commit := in.committing()
	var out decideResult
	run := func(tx pgx.Tx) error {
		planned, err := planReviewDecision(r.Context(), tx, id, in, status)
		if err != nil {
			return err
		}
		out = planned
		if !commit {
			out.DryRun = true
			return nil
		}
		return commitReviewDecision(r.Context(), tx, p, id, status, in.Reason, &out)
	}
	if commit {
		err = st.Tx(r.Context(), run)
	} else {
		err = st.TxReadOnly(r.Context(), run)
	}
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows), errors.Is(err, errReviewNotDecidable):
			writeErr(w, http.StatusForbidden, errReviewNotDecidable)
			return
		case isDecideBadRequest(err):
			writeErr(w, http.StatusBadRequest, err)
			return
		default:
			writePolicy(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
	h.recordReviewOpen(r.Context())
}

func (h *Handler) recordReviewOpen(ctx context.Context) {
	if h == nil || h.obs == nil {
		return
	}
	st := h.store()
	if st == nil {
		return
	}
	_ = st.Tx(ctx, func(tx pgx.Tx) error {
		// LEFT JOIN against the enum so a kind with zero open rows is
		// recorded as 0. GROUP BY over open rows alone drops that kind
		// and the last gauge value sticks (empty queue, alert still lit).
		rows, err := tx.Query(ctx, `
			SELECT k.kind::text, COALESCE(c.n, 0)
			FROM unnest(enum_range(NULL::review_kind)) AS k(kind)
			LEFT JOIN (
				SELECT kind, count(*) AS n
				FROM review_item
				WHERE status = 'open'
				GROUP BY kind
			) c ON c.kind = k.kind`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var kind string
			var n int64
			if err := rows.Scan(&kind, &n); err != nil {
				return err
			}
			h.obs.SetReviewOpen(ctx, kind, n)
		}
		return rows.Err()
	})
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	if identity.FromContext(r.Context()) == nil {
		writeErr(w, http.StatusUnauthorized, store.ErrNoPrincipal)
		return
	}
	if _, err := h.requireStore(); err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	h.ensureListen()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	ch := h.hub.subscribe()
	defer h.hub.unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *Handler) gitHealth(w http.ResponseWriter, r *http.Request) {
	if h.git == nil {
		gitHealthVar.Set("error:skills repo not configured")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"reason": "skills repo not configured",
		})
		return
	}
	if err := h.git.Check(r.Context()); err != nil {
		gitHealthVar.Set("error:" + err.Error())
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"reason": err.Error(),
		})
		return
	}
	gitHealthVar.Set("ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ensureListen() {
	st := h.store()
	if st == nil || st.Pool() == nil || h.hub == nil {
		return
	}
	h.hub.ensureListen(st.Pool())
}

func (h *Handler) requireStore() (*store.Store, error) {
	st := h.store()
	if st == nil {
		return nil, fmt.Errorf("store not configured")
	}
	return st, nil
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	err := json.NewDecoder(r.Body).Decode(dest)
	if err == nil {
		return nil
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return errBodyTooLarge
	}
	return err
}

var errBodyTooLarge = errors.New("request body too large")

func writeDecodeErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errBodyTooLarge) {
		writeErr(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	writeErr(w, http.StatusBadRequest, err)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write response", "err", err)
	}
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// errRLSRefused is the single body every row-level-security refusal returns.
var errRLSRefused = fmt.Errorf("%w: row-level security refused this write", policy.ErrDeniedScope)

// maskRepoDenial collapses "no such repo" and "bound to a scope you may not
// write" into one indistinguishable 403. Told apart, they are an oracle: the
// `scope` table has no RLS and resolveRepoPaths resolves any key for anyone, so
// a caller could POST guessed repo keys in a loop and learn exactly which repos
// this control plane binds -- private repo names, which is the same class of
// disclosure the chain naming was removed to prevent.
//
// Only denials are masked. A genuine server fault must still surface as a 500
// rather than being reported to the caller as a permission problem.
func maskRepoDenial(err error, repoPath bool) error {
	if err == nil || !repoPath {
		return err
	}
	var pgErr *pgconn.PgError
	isDenial := errors.Is(err, policy.ErrDeniedScope) ||
		errors.Is(err, policy.ErrDeniedVisibility) ||
		errors.Is(err, policy.ErrNeedsReview) ||
		(errors.As(err, &pgErr) && pgErr.Code == "42501")
	if !isDenial {
		return err
	}
	// The key is deliberately not echoed. The caller supplied it, so repeating
	// it adds nothing, and leaving it out makes the two outcomes byte-identical
	// instead of merely similarly-shaped.
	return fmt.Errorf("%w: repo is not available to this principal", policy.ErrDeniedScope)
}

func writePolicy(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, store.ErrNoPrincipal):
		code = http.StatusUnauthorized
	case errors.Is(err, policy.ErrDeniedScope), errors.Is(err, policy.ErrDeniedVisibility):
		code = http.StatusForbidden
	case errors.Is(err, policy.ErrNeedsReview):
		code = http.StatusForbidden
	case errors.Is(err, policy.ErrSecretDetected):
		code = http.StatusBadRequest
	case errors.As(err, &pgErr) && pgErr.Code == "42501":
		// Never echo the raw driver text. "new row violates row-level security
		// policy for table X" names the table and distinguishes an RLS refusal
		// from every other denial, which is enough to probe with.
		code = http.StatusForbidden
		err = errRLSRefused
	default:
		slog.Error("v1 handler failed", "err", err)
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
