package rest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/render"
	"github.com/agentic-substrate/substrate/internal/store"
)

type world struct {
	alice, bob, lead                        string
	orgID, teamAID, teamBID                 string
	global, org, teamA, teamB, projectA     string
	userA, repo, repoKey                    string
	aliceP, bobP, leadP                     *identity.Principal
	pathStr                                 string
	skillID, skillName, skillGitPath        string
	skillSHA                                string
	memTeam, memOld, memUnverified, memProb string
	memGlobal                               string
	reviewOpen                              string
}

func seedWorld(t *testing.T, conn *pgx.Conn) world {
	t.Helper()
	id := func() string {
		t.Helper()
		var s string
		if err := conn.QueryRow(t.Context(), "SELECT gen_random_uuid()::text").Scan(&s); err != nil {
			t.Fatalf("gen_random_uuid: %v", err)
		}
		return s
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(t.Context(), q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	w := world{
		alice:         id(),
		bob:           id(),
		lead:          id(),
		orgID:         id(),
		teamAID:       id(),
		teamBID:       id(),
		org:           id(),
		teamA:         id(),
		teamB:         id(),
		projectA:      id(),
		userA:         id(),
		repo:          id(),
		skillID:       id(),
		memTeam:       id(),
		memOld:        id(),
		memUnverified: id(),
		memProb:       id(),
		memGlobal:     id(),
		reviewOpen:    id(),
	}
	w.global = ensureGlobal(t, conn)
	orgName := "acme-" + w.orgID[:8]
	w.repoKey = "github.com/acme/secret-" + w.repo[:8]
	w.pathStr = "global:/org:" + orgName + "/team:alpha/project:secret"
	w.skillGitPath = "skills/team/alpha/lint"
	w.skillSHA = "deadbeef0123456789"
	w.skillName = "team/alpha/lint-" + w.skillID[:8]

	exec(`INSERT INTO principal (id, kind, display_name, trust) VALUES
		($1, 'user', 'alice', 'human'), ($2, 'user', 'bob', 'human'), ($3, 'user', 'lead', 'human')`,
		w.alice, w.bob, w.lead)
	exec(`INSERT INTO org (id, name) VALUES ($1, $2)`, w.orgID, orgName)
	exec(`INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'alpha'), ($3, $2, 'beta')`,
		w.teamAID, w.orgID, w.teamBID)
	exec(`INSERT INTO membership (principal_id, team_id, role) VALUES
		($1, $2, 'member'), ($3, $4, 'member'), ($5, $2, 'lead')`,
		w.alice, w.teamAID, w.bob, w.teamBID, w.lead)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'org', $2, $3, 0, 'placeholder')`, w.org, w.global, orgName)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'team', $2, 'alpha', 0, 'placeholder', $3),
		($4, 'team', $2, 'beta', 0, 'placeholder', $5)`,
		w.teamA, w.org, w.teamAID, w.teamB, w.teamBID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'project', $2, 'secret', 0, 'placeholder', $3)`, w.projectA, w.teamA, w.teamAID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'repo', $2, $3, 0, 'placeholder', $4)`, w.repo, w.projectA, w.repoKey, w.teamAID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'user', NULL, $2, 0, 'placeholder')`, w.userA, w.alice)

	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'constraint', 'python.version', '3.12', 'active', $2)`, w.projectA, w.alice)
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'constraint', 'ci.required', 'true', 'active', $2)`, w.projectA, w.alice)

	exec(`INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'theme', 'dark', 'active', $2)`, w.teamA, w.alice)

	verID := id()
	exec(`INSERT INTO skill (id, name, scope_id, visibility, owner_id, description)
		VALUES ($1, $2, $3, 'team', $4, 'lint the module')`, w.skillID, w.skillName, w.projectA, w.alice)
	exec(`INSERT INTO skill_version (id, skill_id, semver, git_sha, git_path, author_id, approval)
		VALUES ($1, $2, '1.0.0', $3, $4, $5, 'approved')`, verID, w.skillID, w.skillSHA, w.skillGitPath, w.alice)
	exec(`UPDATE skill SET active_version_id = $1 WHERE id = $2`, verID, w.skillID)

	insertMem := func(id, vis, status, title string, stale bool) {
		t.Helper()
		q := `INSERT INTO memory (id, scope_id, visibility, owner_id, tier, kind, title, body, status, source, verification, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'semantic', 'fact', $5, $5, $6, '{"machine":"wsl"}', '{"type":"human"}', $4, now(), now())`
		if stale {
			q = `INSERT INTO memory (id, scope_id, visibility, owner_id, tier, kind, title, body, status, source, verification, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'semantic', 'fact', $5, $5, $6, '{"machine":"wsl"}', '{"type":"human"}', $4, now() - interval '2 days', now() - interval '2 days')`
		}
		exec(q, id, w.projectA, vis, w.alice, title, status)
	}
	insertMem(w.memTeam, "team", "confirmed", "team-secret-fact", false)
	insertMem(w.memProb, "team", "probable", "team-probable-fact", false)
	insertMem(w.memUnverified, "team", "unverified", "team-unverified-fact", false)
	insertMem(w.memOld, "team", "confirmed", "team-old-fact", true)
	insertMem(w.memGlobal, "global", "confirmed", "global-public-fact", false)

	exec(`INSERT INTO review_item (id, kind, scope_id, team_id, payload, proposed_by)
		VALUES ($1, 'drift_proposal', $2, $3, '{"title":"open review: x"}', $4)`,
		w.reviewOpen, w.projectA, w.teamAID, w.alice)

	parse := func(s string) uuid.UUID {
		t.Helper()
		u, err := uuid.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	aliceID, bobID, leadID := parse(w.alice), parse(w.bob), parse(w.lead)
	orgID, teamA, teamB := parse(w.orgID), parse(w.teamAID), parse(w.teamBID)
	caps := []string{"memory:write"}
	w.aliceP = &identity.Principal{
		ID: aliceID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamA}, Capabilities: caps,
	}
	w.bobP = &identity.Principal{
		ID: bobID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamB}, Capabilities: caps,
	}
	w.leadP = &identity.Principal{
		ID: leadID, Kind: identity.KindUser, Trust: identity.TrustHuman,
		OrgID: orgID, TeamIDs: []uuid.UUID{teamA}, Capabilities: caps,
	}
	return w
}

type fakeGit struct{ err error }

func (f fakeGit) Check(context.Context) error { return f.err }

func openREST(t *testing.T, dsn string, git GitChecker) (*Handler, *store.Store, *memory.Service) {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	mem := memory.New(func() *store.Store { return st })
	h := New(Options{
		Store:  func() *store.Store { return st },
		Memory: mem,
		Git:    git,
		Now:    func() time.Time { return time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC) },
	})
	t.Cleanup(h.Close)
	return h, st, mem
}

func serveREST(t *testing.T, h *Handler, w world) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", func(rw http.ResponseWriter, r *http.Request) {
		st := h.store()
		if err := st.Ping(r.Context()); err != nil {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusServiceUnavailable)
			_, _ = rw.Write([]byte(`{"status":"not_ready"}`))
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"status":"ok"}`))
	})
	h.Mount(mux)
	wrapped := identity.Middleware(func(_ context.Context, tok string) (*identity.Principal, error) {
		switch tok {
		case "alice":
			return w.aliceP, nil
		case "bob":
			return w.bobP, nil
		case "lead":
			return w.leadP, nil
		default:
			return nil, identity.ErrUnauthorized
		}
	})(mux)
	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, srv *httptest.Server, method, path, token string, body any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func decodeJSON(t *testing.T, res *http.Response, dest any) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if dest == nil {
		return
	}
	if err := json.Unmarshal(b, dest); err != nil {
		t.Fatalf("json %s: %v\nbody: %s", res.Request.URL.Path, err, b)
	}
}

func TestUnauthenticatedV1Rejected(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)
	for _, path := range []string{
		"/v1/render", "/v1/memory/cache", "/v1/skills/manifest",
		"/v1/events", "/v1/review", "/v1/health/git",
	} {
		res := doJSON(t, srv, http.MethodGet, path, "", nil)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without bearer = %d, want 401", path, res.StatusCode)
		}
	}
}

func TestRenderTargetsMatchDriftHash(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	res := doJSON(t, srv, http.MethodGet, "/v1/render?machine=wsl&repos="+w.repoKey, "alice", nil)
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		t.Fatalf("/v1/render = %d, want 200: %s", res.StatusCode, b)
	}
	var out struct {
		Targets []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			SHA256  string `json:"sha256"`
		} `json:"targets"`
	}
	decodeJSON(t, res, &out)

	wantPaths := map[string]bool{}
	for _, spec := range render.Specs() {
		for _, p := range spec.Paths {
			wantPaths[p] = true
		}
	}
	if len(out.Targets) != len(wantPaths) {
		t.Fatalf("got %d targets, want %d (one per render path)", len(out.Targets), len(wantPaths))
	}
	seen := map[string]bool{}
	var hasTeam, hasPython bool
	for _, tgt := range out.Targets {
		if !wantPaths[tgt.Path] {
			t.Errorf("unexpected path %q", tgt.Path)
		}
		if seen[tgt.Path] {
			t.Errorf("duplicate path %q", tgt.Path)
		}
		seen[tgt.Path] = true
		full := sha256.Sum256([]byte(tgt.Content))
		if tgt.SHA256 == hex.EncodeToString(full[:]) {
			t.Fatalf("path %s hashed the footer; adapters will report drift every cycle", tgt.Path)
		}
		if tgt.SHA256 != render.DriftHash(tgt.Content) {
			t.Fatalf("path %s sha256 %s != render.DriftHash", tgt.Path, tgt.SHA256)
		}
		if strings.Contains(tgt.Content, "ci.required") {
			hasTeam = true
		}
		if strings.Contains(tgt.Content, "python.version") {
			hasPython = true
		}
	}
	if !hasPython {
		t.Fatal("render missing python.version instruction")
	}
	if !hasTeam {
		t.Fatal("render missing team-visible ci.required; the test cannot prove RLS")
	}

	bob := doJSON(t, srv, http.MethodGet, "/v1/render?machine=wsl&repos="+w.repoKey, "bob", nil)
	t.Cleanup(func() { _ = bob.Body.Close() })
	var bobOut struct {
		Targets []struct {
			Content string `json:"content"`
		} `json:"targets"`
	}
	decodeJSON(t, bob, &bobOut)
	for _, tgt := range bobOut.Targets {
		if strings.Contains(tgt.Content, "ci.required") {
			t.Fatal("bob (non-member) saw team-visible ci.required on /v1/render")
		}
	}
}

func TestMemoryBatchIdempotentAndAtomic(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, mem := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	payloads := make([]map[string]any, 50)
	ids := make([]string, 50)
	for i := range payloads {
		cid, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = cid.String()
		payloads[i] = map[string]any{
			"client_id":    ids[i],
			"kind":         "fact",
			"title":        fmt.Sprintf("batch-%d", i),
			"body":         fmt.Sprintf("body-%d", i),
			"scope":        w.pathStr,
			"visibility":   "team",
			"verification": map[string]string{"type": "human"},
			"source":       map[string]string{"machine": "wsl"},
			"status":       "confirmed",
		}
	}

	res := doJSON(t, srv, http.MethodPost, "/v1/memory/batch", "alice", payloads)
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		t.Fatalf("batch = %d, want 200: %s", res.StatusCode, b)
	}
	var first []batchResult
	decodeJSON(t, res, &first)
	if len(first) != 50 {
		t.Fatalf("got %d results, want 50", len(first))
	}
	original := map[string]string{}
	for _, r := range first {
		if r.Duplicate {
			t.Fatalf("first drain flagged duplicate for %s", r.ClientID)
		}
		if r.SubjectID == "" {
			t.Fatal("missing subject_id")
		}
		original[r.ClientID] = r.SubjectID
	}
	var n int
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.aliceP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE title LIKE 'batch-%'`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 50 {
		t.Fatalf("memory rows %d, want 50", n)
	}

	replay := doJSON(t, srv, http.MethodPost, "/v1/memory/batch", "alice", payloads)
	if replay.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(replay.Body)
		_ = replay.Body.Close()
		t.Fatalf("replay = %d: %s", replay.StatusCode, b)
	}
	var second []batchResult
	decodeJSON(t, replay, &second)
	if len(second) != 50 {
		t.Fatalf("replay results %d, want 50", len(second))
	}
	for _, r := range second {
		if !r.Duplicate {
			t.Fatalf("replay of %s was not duplicate:true", r.ClientID)
		}
		if r.SubjectID != original[r.ClientID] {
			t.Fatalf("replay subject_id %s, want original %s", r.SubjectID, original[r.ClientID])
		}
	}
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.aliceP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE title LIKE 'batch-%'`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 50 {
		t.Fatalf("replay created memories: count=%d", n)
	}

	crashID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	mem.FailAfterReceipt = func() error { return errors.New("injected crash between receipt and subject") }
	crash := doJSON(t, srv, http.MethodPost, "/v1/memory/batch", "alice", []map[string]any{{
		"client_id":    crashID.String(),
		"kind":         "fact",
		"title":        "crash-atomicity",
		"body":         "must not land",
		"scope":        w.pathStr,
		"visibility":   "team",
		"verification": map[string]string{"type": "human"},
		"source":       map[string]string{"machine": "wsl"},
	}})
	_ = crash.Body.Close()
	if crash.StatusCode == http.StatusOK {
		t.Fatal("injected failure still returned 200")
	}
	mem.FailAfterReceipt = nil

	ctx := identity.WithPrincipal(t.Context(), w.aliceP)
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ingest_receipt WHERE client_id = $1`, crashID).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatal("injected failure left an ingest_receipt without a memory")
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM memory WHERE title = 'crash-atomicity'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatal("injected failure left a memory without a durable receipt")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryCacheDeltaAndRLS(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	since := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	res := doJSON(t, srv, http.MethodGet, "/v1/memory/cache?repos="+w.repoKey+"&since="+since, "alice", nil)
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		t.Fatalf("cache = %d: %s", res.StatusCode, b)
	}
	var aliceOut struct {
		Memories []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"memories"`
	}
	decodeJSON(t, res, &aliceOut)
	seen := map[string]string{}
	for _, m := range aliceOut.Memories {
		seen[m.ID] = m.Status
		if m.Status != "confirmed" && m.Status != "probable" {
			t.Fatalf("cache returned status %q", m.Status)
		}
	}
	if seen[w.memTeam] != "confirmed" {
		t.Fatal("alice missing team-visible confirmed memory; the test cannot prove bob is denied")
	}
	if seen[w.memProb] != "probable" {
		t.Fatal("alice missing probable memory")
	}
	if seen[w.memGlobal] != "confirmed" {
		t.Fatal("alice missing global confirmed memory")
	}
	if _, ok := seen[w.memUnverified]; ok {
		t.Fatal("cache returned unverified memory")
	}
	if _, ok := seen[w.memOld]; ok {
		t.Fatal("cache returned a row older than since=")
	}

	bob := doJSON(t, srv, http.MethodGet, "/v1/memory/cache?since="+since, "bob", nil)
	t.Cleanup(func() { _ = bob.Body.Close() })
	var bobOut struct {
		Memories []struct {
			ID string `json:"id"`
		} `json:"memories"`
	}
	decodeJSON(t, bob, &bobOut)
	for _, m := range bobOut.Memories {
		if m.ID == w.memTeam || m.ID == w.memProb {
			t.Fatal("bob (non-member) saw a team-visible cache row")
		}
	}

	bare, err := st.Pool().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bare.Rollback(t.Context()) })
	var n int
	if err := bare.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE id = $1`, w.memTeam).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("team-visible memory leaked from a bare transaction with no session settings")
	}
}

func TestSkillsManifest(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	res := doJSON(t, srv, http.MethodGet, "/v1/skills/manifest?repos="+w.repoKey, "alice", nil)
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		t.Fatalf("manifest = %d: %s", res.StatusCode, b)
	}
	var out struct {
		Skills []struct {
			Name    string `json:"name"`
			GitPath string `json:"git_path"`
			GitSHA  string `json:"git_sha"`
		} `json:"skills"`
	}
	decodeJSON(t, res, &out)
	found := false
	for _, s := range out.Skills {
		if s.Name == w.skillName {
			found = true
			if s.GitPath != w.skillGitPath || s.GitSHA != w.skillSHA {
				t.Fatalf("skill %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("alice missing team-visible skill in manifest")
	}

	bob := doJSON(t, srv, http.MethodGet, "/v1/skills/manifest?repos="+w.repoKey, "bob", nil)
	t.Cleanup(func() { _ = bob.Body.Close() })
	var bobOut struct {
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	decodeJSON(t, bob, &bobOut)
	for _, s := range bobOut.Skills {
		if s.Name == w.skillName {
			t.Fatal("bob (non-member) saw a team-visible skill")
		}
	}
}

func TestGitHealthDoesNotAffectReadyz(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{err: errors.New("skills repo unreachable")})
	srv := serveREST(t, h, w)

	ready := doJSON(t, srv, http.MethodGet, "/readyz", "", nil)
	if ready.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(ready.Body)
		_ = ready.Body.Close()
		t.Fatalf("/readyz = %d with git down, want 200 (EDD R27): %s", ready.StatusCode, b)
	}
	_ = ready.Body.Close()

	git := doJSON(t, srv, http.MethodGet, "/v1/health/git", "alice", nil)
	if git.StatusCode != http.StatusOK && git.StatusCode != http.StatusServiceUnavailable {
		_ = git.Body.Close()
		t.Fatalf("/v1/health/git status %d", git.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	decodeJSON(t, git, &body)
	if body.Status == "ok" {
		t.Fatal("/v1/health/git reported ok while the skills repo is unreachable")
	}
	if !strings.Contains(body.Reason, "skills repo unreachable") && !strings.Contains(body.Status, "error") {
		t.Fatalf("git health body %+v, want a named failure", body)
	}
}

func TestReviewCreateListDecide(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	created := doJSON(t, srv, http.MethodPost, "/v1/review", "alice", map[string]any{
		"kind":    "drift_proposal",
		"scope":   w.pathStr,
		"team_id": w.teamAID,
		"payload": map[string]string{"title": "drift in AGENTS.md", "diff": "--- a\n+++ b\n"},
	})
	if created.StatusCode != http.StatusOK && created.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(created.Body)
		_ = created.Body.Close()
		t.Fatalf("create review = %d: %s", created.StatusCode, b)
	}
	var item struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Kind   string `json:"kind"`
	}
	decodeJSON(t, created, &item)
	if item.ID == "" || item.Status != "open" {
		t.Fatalf("created %+v", item)
	}

	listed := doJSON(t, srv, http.MethodGet, "/v1/review?team="+w.teamAID+"&status=open", "alice", nil)
	t.Cleanup(func() { _ = listed.Body.Close() })
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decodeJSON(t, listed, &list)
	found := false
	for _, it := range list.Items {
		if it.ID == item.ID || it.ID == w.reviewOpen {
			found = true
		}
	}
	if !found {
		t.Fatal("alice list missing her team's open reviews")
	}

	bobList := doJSON(t, srv, http.MethodGet, "/v1/review?status=open", "bob", nil)
	t.Cleanup(func() { _ = bobList.Body.Close() })
	var bobItems struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decodeJSON(t, bobList, &bobItems)
	for _, it := range bobItems.Items {
		if it.ID == item.ID || it.ID == w.reviewOpen {
			t.Fatal("bob (other team) saw team-A review items")
		}
	}

	aliceDecide := doJSON(t, srv, http.MethodPost, "/v1/review/"+item.ID+"/decide", "alice", map[string]any{
		"decision": "approved",
		"reason":   "proposer must not self-approve",
	})
	_ = aliceDecide.Body.Close()
	if aliceDecide.StatusCode == http.StatusOK {
		t.Fatal("member proposer decided their own review")
	}

	decided := doJSON(t, srv, http.MethodPost, "/v1/review/"+item.ID+"/decide", "lead", map[string]any{
		"decision": "approved",
		"reason":   "looks good",
	})
	if decided.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(decided.Body)
		_ = decided.Body.Close()
		t.Fatalf("lead decide = %d: %s", decided.StatusCode, b)
	}
	var after struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	decodeJSON(t, decided, &after)
	if after.Status != "approved" {
		t.Fatalf("status %q, want approved", after.Status)
	}
}

func TestEventsFanoutAndLaggardDrop(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	liveReq, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	liveReq.Header.Set("Authorization", "Bearer alice")
	liveRes, err := srv.Client().Do(liveReq)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = liveRes.Body.Close() }()
	if liveRes.StatusCode != http.StatusOK {
		t.Fatalf("live SSE = %d", liveRes.StatusCode)
	}

	stallReq, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	stallReq.Header.Set("Authorization", "Bearer alice")
	stallRes, err := srv.Client().Do(stallReq)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stallRes.Body.Close() }()

	got := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(liveRes.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data:") {
				got <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()

	if err := h.WaitListening(2 * time.Second); err != nil {
		t.Fatalf("LISTEN not ready: %v", err)
	}

	fire := func() {
		t.Helper()
		ctx := identity.WithPrincipal(t.Context(), w.aliceP)
		if err := st.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE instruction SET body = body WHERE key = 'ci.required' AND scope_id = $1`, w.projectA)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	fire()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("live SSE client received nothing within 2s of an instruction change")
	}

	for i := 0; i < eventBuffer+4; i++ {
		fire()
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("live client stalled after a laggard stopped reading")
	}
}

func TestHubDropsLaggardWithoutBlockingProducer(t *testing.T) {
	h := newHub(1)
	fast := h.subscribe()
	slow := h.subscribe()
	h.broadcast([]byte("a"))
	if got := string(<-fast); got != "a" {
		t.Fatalf("fast got %q, want a", got)
	}
	h.broadcast([]byte("overflow"))
	drained := 0
	for range slow {
		drained++
	}
	if drained == 0 {
		t.Fatal("laggard dropped with an empty buffer; overflow was not the cause")
	}
	if got := string(<-fast); got != "overflow" {
		t.Fatalf("fast got %q, want overflow", got)
	}
	start := time.Now()
	h.broadcast([]byte("d"))
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("broadcast blocked on a dropped subscriber")
	}
	until := time.After(time.Second)
	for {
		select {
		case msg := <-fast:
			if string(msg) == "d" {
				return
			}
		case <-until:
			t.Fatal("fast subscriber missed an event after the laggard dropped")
		}
	}
}
