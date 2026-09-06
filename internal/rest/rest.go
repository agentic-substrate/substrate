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
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/preference"
	"github.com/agentic-substrate/substrate/internal/render"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
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
	Store  func() *store.Store
	Memory *memory.Service
	Git    GitChecker
	Now    func() time.Time
}

// Handler serves /v1 for the adapter and CLI.
type Handler struct {
	getStore func() *store.Store
	mem      *memory.Service
	git      GitChecker
	now      func() time.Time
	hub      *hub
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
		if len(repos) > 0 {
			paths, err := resolveRepoPaths(r.Context(), tx, repos)
			if err != nil {
				return err
			}
			for _, path := range paths {
				if err := policy.Check("skills.manifest", path, *p); err != nil {
					return err
				}
			}
		}
		rows, err := store.New(tx).ListApprovedSkills(r.Context())
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
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("memory.batch: %w", err))
		return
	}
	out, err := h.mem.Batch(r.Context(), items)
	if err != nil {
		writePolicy(w, err)
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
		TeamID  string          `json:"team_id"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sc, err := scope.Parse(in.Scope)
	if err != nil {
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
		writePolicy(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     uuid.UUID(row.ID.Bytes).String(),
		"kind":   string(row.Kind),
		"status": string(row.Status),
	})
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
	var in struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
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
	var row store.DecideReviewItemRow
	err = st.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		row, err = store.New(tx).DecideReviewItem(r.Context(), store.DecideReviewItemParams{
			ID:        pgUUID(id),
			Status:    status,
			DecidedBy: pgUUID(p.ID),
			Reason:    &in.Reason,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusForbidden, fmt.Errorf("review not decidable by this principal"))
			return
		}
		writePolicy(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":     uuid.UUID(row.ID.Bytes).String(),
		"status": string(row.Status),
		"reason": in.Reason,
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
		code = http.StatusForbidden
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
