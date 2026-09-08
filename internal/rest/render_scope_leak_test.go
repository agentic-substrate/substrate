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

// A repo key that does not exist and one that exists but belongs to another
// team must be told apart by nobody. Distinguishable bodies make the endpoint
// an oracle: the `scope` table has no RLS and any key resolves for anyone, so
// a caller could POST guessed keys in a loop and learn which repos this control
// plane binds. Returning either the raw "repo not found" or the driver's
// "new row violates row-level security policy for table ..." is the one-line
// production change that makes this red.
func TestRepoDenialIsIndistinguishableFromRepoNotFound(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	post := func(repo string) (int, string) {
		res := doJSON(t, srv, http.MethodPost, "/v1/review", "bob", map[string]any{
			"kind":    "drift_proposal",
			"repo":    repo,
			"payload": map[string]string{"path": "/w/AGENTS.md", "diff": "@@", "title": "drift"},
		})
		return res.StatusCode, readBody(t, res)
	}

	// Bound, but to a repo under team alpha, which bob is not a member of.
	foreignCode, foreignBody := post(w.repoKey)
	// Not bound at all.
	missingCode, missingBody := post("github.com/acme/does-not-exist")

	if foreignCode != http.StatusForbidden || missingCode != http.StatusForbidden {
		t.Fatalf("codes = %d (foreign) and %d (missing), want 403 for both", foreignCode, missingCode)
	}
	if foreignBody != missingBody {
		t.Fatalf("a foreign repo and a missing repo are distinguishable:\n foreign: %s\n missing: %s", foreignBody, missingBody)
	}
	// Anti-vacuity: if both were empty or both a generic 500 the comparison
	// above would pass while telling us nothing.
	if !strings.Contains(foreignBody, "not available to this principal") {
		t.Fatalf("expected the uniform denial body, got %s", foreignBody)
	}
}

// The repo path masks its own denials, so this covers every other write: an RLS
// refusal must not echo the driver's "new row violates row-level security policy
// for table review_item", which names the table and tells an RLS refusal apart
// from every other kind of denial. Removing `err = errRLSRefused` from
// writePolicy is the one-line production change that makes this red.
func TestRLSRefusalDoesNotEchoTheDriverText(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	// bob names team alpha's scope explicitly. policy.Check lets a repo leaf
	// past on team membership alone, so the refusal comes from RLS itself.
	res := doJSON(t, srv, http.MethodPost, "/v1/review", "bob", map[string]any{
		"kind":    "drift_proposal",
		"scope":   w.pathStr,
		"payload": map[string]string{"path": "/w/AGENTS.md", "diff": "@@", "title": "drift"},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("bob write at team alpha's scope = %d, want 403: %s", res.StatusCode, body)
	}
	for _, leak := range []string{"row-level security policy for table", "review_item", "SQLSTATE"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaks driver detail %q: %s", leak, body)
		}
	}
}
