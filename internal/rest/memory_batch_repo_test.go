package rest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
)

func observationItem(repo, scopePath string) map[string]any {
	item := map[string]any{
		"client_id":    uuid.NewString(),
		"kind":         "observation",
		"title":        "Edit",
		"body":         "touched a.go",
		"identifiers":  []string{"/w/a.go"},
		"visibility":   "team",
		"verification": map[string]string{"type": "agent_inference"},
		"source":       map[string]string{"machine": "wsl"},
		"status":       "unverified",
	}
	if repo != "" {
		item["repo"] = repo
	}
	if scopePath != "" {
		item["scope"] = scopePath
	}
	return item
}

// A PostToolUse observation names the repo key its cwd belongs to and nothing
// else. The server resolves the chain it bound that key to, exactly as
// POST /v1/review does (#58, R18), places the row there, and never returns the
// chain: the `scope` table has no RLS, so handing it back would disclose
// another team's org/team/project naming.
func TestMemoryBatchBindsScopeToRepo(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	res := doJSON(t, srv, http.MethodPost, "/v1/memory/batch", "alice",
		[]map[string]any{observationItem(w.repoKey, "")})
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("repo-keyed batch = %d, want 200: %s", res.StatusCode, body)
	}
	for _, secret := range []string{w.pathStr, "team:alpha", "project:secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("the batch response leaks the resolved chain %q:\n%s", secret, body)
		}
	}

	var scopeID string
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.aliceP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(),
			`SELECT scope_id::text FROM memory WHERE title = 'Edit' ORDER BY created_at DESC LIMIT 1`).Scan(&scopeID)
	}); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if scopeID != w.repo {
		t.Fatalf("observation landed at scope %s, want the repo scope %s", scopeID, w.repo)
	}
}

// Accepting both would let a caller name a repo and still choose the chain,
// which is the whole point of resolving the key server-side.
func TestMemoryBatchRejectsScopeAndRepoTogether(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	res := doJSON(t, srv, http.MethodPost, "/v1/memory/batch", "alice",
		[]map[string]any{observationItem(w.repoKey, w.pathStr)})
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("scope+repo = %d, want 400", res.StatusCode)
	}
}

// A repo key that does not exist and one bound to a team the caller may not
// write must be told apart by nobody. Distinguishable bodies make the endpoint
// an enumeration oracle for private repo names.
func TestMemoryBatchRepoDenialIsIndistinguishable(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	post := func(repo string) (int, string) {
		res := doJSON(t, srv, http.MethodPost, "/v1/memory/batch", "bob",
			[]map[string]any{observationItem(repo, "")})
		return res.StatusCode, readBody(t, res)
	}
	foreignCode, foreignBody := post(w.repoKey)
	missingCode, missingBody := post("github.com/acme/does-not-exist")

	if foreignCode != http.StatusForbidden || missingCode != http.StatusForbidden {
		t.Fatalf("codes = %d (foreign) and %d (missing), want 403 for both:\n%s\n%s",
			foreignCode, missingCode, foreignBody, missingBody)
	}
	if foreignBody != missingBody {
		t.Fatalf("a foreign repo and a missing repo are distinguishable:\n foreign: %s\n missing: %s", foreignBody, missingBody)
	}
	if !strings.Contains(foreignBody, "not available to this principal") {
		t.Fatalf("expected the uniform denial body, got %s", foreignBody)
	}

	// Anti-vacuity: nothing was written for bob at team alpha's repo.
	var n int
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.bobP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE created_by = $1`, w.bob).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d memory rows were filed by bob at team alpha's repo", n)
	}
}
