package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
)

// bob is on team beta. The repo scope, its project and its team belong to team
// alpha, and the `scope` table carries no RLS at all -- any repo key resolves
// for anyone -- so /v1/render answers bob with 200 and merely filters the
// content. It must not additionally hand him the chain: `global:/org:acme-.../
// team:alpha/project:secret/repo:...` names an org, a team and a project he has
// no rights to. Row RLS already covers the content; the naming is the leak.
//
// The team-visible instruction is asserted present for alice first, so this is
// not passing merely because the fixture renders nothing.
func TestRenderDoesNotLeakForeignChainNaming(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	alice := doJSON(t, srv, http.MethodGet, "/v1/render?machine=wsl&repos="+w.repoKey, "alice", nil)
	defer func() { _ = alice.Body.Close() }()
	aliceBody := readBody(t, alice)
	if !strings.Contains(aliceBody, "ci.required") {
		t.Fatal("alice's render lacks the team-visible instruction; the fixture cannot prove anything")
	}

	res := doJSON(t, srv, http.MethodGet, "/v1/render?machine=wsl&repos="+w.repoKey, "bob", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/v1/render for bob = %d, want 200 (this test is about what a 200 body says)", res.StatusCode)
	}
	body := readBody(t, res)

	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if raw, ok := out["scope"]; ok {
		t.Fatalf("render answered a non-member with the chain %s; the server must not tell a client which scope it may not read", raw)
	}
	for _, secret := range []string{w.pathStr, "team:alpha", "project:secret", w.repoKey} {
		if strings.Contains(body, secret) {
			t.Fatalf("render body leaks %q to a non-member of that team:\n%s", secret, body)
		}
	}
	if strings.Contains(body, "ci.required") {
		t.Fatal("bob (non-member) saw team-visible content; row RLS is not applied")
	}
}

// The chain a drift proposal lands at is derived by the server from the repo
// key, and the review_item INSERT policy is the backstop that survives the next
// handler bug: a row may not be filed at a scope the actor cannot write, even
// when it is stamped with the actor's own team.
func TestReviewCreateBindsScopeToRepoAndRLSRefusesForeignScope(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	proposal := func(extra map[string]any) map[string]any {
		body := map[string]any{
			"kind":    "drift_proposal",
			"payload": map[string]string{"path": "/w/AGENTS.md", "diff": "@@", "title": "drift"},
		}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}

	// A member of the repo's team names only the repo key and the server
	// resolves the chain: the row lands at the repo scope, never at whatever
	// the client might have asked for.
	ok := doJSON(t, srv, http.MethodPost, "/v1/review", "alice", proposal(map[string]any{"repo": w.repoKey}))
	if ok.StatusCode != http.StatusCreated {
		t.Fatalf("alice repo proposal = %d, want 201: %s", ok.StatusCode, readBody(t, ok))
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, ok, &created)
	var scopeID string
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.aliceP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT scope_id::text FROM review_item WHERE id = $1`, created.ID).Scan(&scopeID)
	}); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if scopeID != w.repo {
		t.Fatalf("row landed at scope %s, want the repo scope %s", scopeID, w.repo)
	}

	// A caller may not name both: accepting both would let it name a repo and
	// still choose the chain.
	both := doJSON(t, srv, http.MethodPost, "/v1/review", "alice",
		proposal(map[string]any{"repo": w.repoKey, "scope": w.pathStr}))
	defer func() { _ = both.Body.Close() }()
	if both.StatusCode != http.StatusBadRequest {
		t.Fatalf("scope+repo = %d, want 400", both.StatusCode)
	}

	// bob is on team beta. The repo resolves for him -- `scope` has no RLS --
	// and policy.Check passes on team membership alone, so only the INSERT
	// policy's scope_writable stops the row.
	denied := doJSON(t, srv, http.MethodPost, "/v1/review", "bob", proposal(map[string]any{"repo": w.repoKey}))
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("bob repo proposal = %d, want 403: %s", denied.StatusCode, readBody(t, denied))
	}

	// The same refusal with an explicitly named foreign chain: this is the path
	// that exists whatever the handler does, and goes red if the
	// scope_writable clause is dropped from migration 00009.
	explicit := doJSON(t, srv, http.MethodPost, "/v1/review", "bob", proposal(map[string]any{"scope": w.pathStr}))
	defer func() { _ = explicit.Body.Close() }()
	if explicit.StatusCode != http.StatusForbidden {
		t.Fatalf("bob proposal at team alpha's project = %d, want 403: %s", explicit.StatusCode, readBody(t, explicit))
	}

	var n int
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.bobP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(),
			`SELECT count(*) FROM review_item WHERE proposed_by = $1`, w.bob).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d review_item rows were filed by bob at team alpha's scopes", n)
	}
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return string(b)
}
