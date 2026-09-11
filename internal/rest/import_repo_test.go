package rest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/importer"
)

// This pins the nil-means-dry-run contract at the type level:
// a plain bool cannot express "the client omitted commit", and the wire
// protocol keeps that distinction (#97 constraint 7). Changing the field to a
// non-pointer bool fails to compile here.
var _ *bool = importApplyRequest{}.Commit

// importCLAUDE is the file the fake machine "wsl" was scanned from. POST
// /v1/import admits only blocks the inventory contains (#95), so these bodies
// have to come from a real inventory rather than from hand-written hashes.
const importCLAUDE = "## Style\n\n- Always run gofmt.\n\n## Editor\n\n- I prefer tabs over spaces.\n"

// importScan runs the plan half of the real pipeline over one machine's file
// and returns both artifacts the wire body now carries.
func importScan(t *testing.T) (importer.Plan, importer.InventoryWitness) {
	t.Helper()
	invs := []importer.Inventory{{
		Hostname: "wsl",
		Roots:    []string{"/w"},
		Files: []importer.File{{
			Rel: ".claude/CLAUDE.md", Path: "/w/.claude/CLAUDE.md",
			DetectedType: "claude", ImpliedScope: "user", Content: importCLAUDE,
		}},
	}}
	plan, err := importer.BuildPlan(invs, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	witness, err := importer.WitnessInventories(invs)
	if err != nil {
		t.Fatalf("WitnessInventories: %v", err)
	}
	return *plan, witness
}

func importBody(t *testing.T, repo, scopePath string, commit *bool) map[string]any {
	t.Helper()
	plan, witness := importScan(t)
	body := map[string]any{
		"machine":           "wsl",
		"trusted_machine":   "wsl",
		"client_id":         uuid.NewString(),
		"plan":              plan,
		"inventory_witness": witness,
	}
	if repo != "" {
		body["repo"] = repo
	}
	if scopePath != "" {
		body["scope"] = scopePath
	}
	if commit != nil {
		body["commit"] = *commit
	}
	return body
}

// An import names the repo key its checkout belongs to and nothing else. The
// server resolves the chain it bound that key to, exactly as POST /v1/review
// does (#58, R18), files the rows there, and never returns the chain: the
// `scope` table has no RLS, so handing it back would disclose another team's
// org/team/project naming (#97 constraint 8).
func TestImportBindsScopeToRepo(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	commit := true
	res := doJSON(t, srv, http.MethodPost, "/v1/import", "alice", importBody(t, w.repoKey, "", &commit))
	body := readBody(t, res)
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		t.Fatalf("repo-keyed import = %d, want 200 or 201: %s", res.StatusCode, body)
	}
	for _, secret := range []string{w.pathStr, "team:alpha", "project:secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("the import response leaks the resolved chain %q:\n%s", secret, body)
		}
	}

	key := importer.RowKey("instruction", ".claude/CLAUDE.md", "Style", 0)
	var scopeID string
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.aliceP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(),
			`SELECT scope_id::text FROM instruction WHERE key = $1 AND created_by = $2`, key, w.alice).Scan(&scopeID)
	}); err != nil {
		t.Fatalf("read back instruction %q: %v", key, err)
	}
	if scopeID != w.repo {
		t.Fatalf("import landed at scope %s, want the repo scope %s", scopeID, w.repo)
	}
}

// Accepting both would let a caller name a repo and still choose the chain,
// which is the whole point of resolving the key server-side.
func TestImportRejectsScopeAndRepoTogether(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	commit := true
	res := doJSON(t, srv, http.MethodPost, "/v1/import", "alice", importBody(t, w.repoKey, w.pathStr, &commit))
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("scope+repo = %d, want 400", res.StatusCode)
	}
}

// A repo key that does not exist and one bound to a team the caller may not
// write must be told apart by nobody. Distinguishable bodies make the endpoint
// an enumeration oracle for private repo names.
func TestImportRepoDenialIsIndistinguishable(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	commit := true
	post := func(repo string, commit *bool) (int, string) {
		res := doJSON(t, srv, http.MethodPost, "/v1/import", "bob", importBody(t, repo, "", commit))
		return res.StatusCode, readBody(t, res)
	}
	// Both legs matter. The commit leg is denied by RLS at the write; the
	// dry-run leg writes nothing, so RLS never fires and only an explicit
	// writability check on the resolved chain keeps the two indistinguishable.
	indistinguishable := func(leg string, commit *bool) {
		t.Helper()
		foreignCode, foreignBody := post(w.repoKey, commit)
		missingCode, missingBody := post("github.com/acme/does-not-exist", commit)
		if foreignCode != http.StatusForbidden || missingCode != http.StatusForbidden {
			t.Fatalf("%s: codes = %d (foreign) and %d (missing), want 403 for both:\n%s\n%s",
				leg, foreignCode, missingCode, foreignBody, missingBody)
		}
		if foreignBody != missingBody {
			t.Fatalf("%s: a foreign repo and a missing repo are distinguishable:\n foreign: %s\n missing: %s",
				leg, foreignBody, missingBody)
		}
		if !strings.Contains(foreignBody, "not available to this principal") {
			t.Fatalf("%s: expected the uniform denial body, got %s", leg, foreignBody)
		}
	}
	indistinguishable("commit", &commit)
	indistinguishable("dry-run", nil)

	// Anti-vacuity: nothing was written for bob at team alpha's repo.
	var n int
	if err := st.Tx(identity.WithPrincipal(t.Context(), w.bobP), func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM instruction WHERE created_by = $1`, w.bob).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d instruction rows were filed by bob at team alpha's repo", n)
	}
}

// A client that omits `commit` gets a dry run and no write. This is the
// server-side guarantee TestImportApplyDefaultsToDryRun asserted from the
// client; defaulting a missing `commit` to true is what turns it red. The
// commit:true leg is the anti-vacuity control -- without it, a handler that
// never wrote anything would pass.
func TestImportNilCommitIsDryRun(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, st, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	key := importer.RowKey("instruction", ".claude/CLAUDE.md", "Style", 0)
	count := func() int {
		t.Helper()
		var n int
		if err := st.Tx(identity.WithPrincipal(t.Context(), w.aliceP), func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT count(*) FROM instruction WHERE key = $1 AND scope_id = $2`, key, w.repo).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}

	res := doJSON(t, srv, http.MethodPost, "/v1/import", "alice", importBody(t, w.repoKey, "", nil))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("import without commit = %d, want 200: %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `"dry_run":true`) {
		t.Fatalf("import without commit is not a dry run: %s", body)
	}
	if n := count(); n != 0 {
		t.Fatalf("import without commit wrote %d instruction rows", n)
	}

	commit := true
	res = doJSON(t, srv, http.MethodPost, "/v1/import", "alice", importBody(t, w.repoKey, "", &commit))
	body = readBody(t, res)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("import with commit = %d, want 201: %s", res.StatusCode, body)
	}
	if n := count(); n != 1 {
		t.Fatalf("import with commit wrote %d instruction rows, want 1", n)
	}
}

// A body with neither `scope` nor `repo` is a client mistake, not a server
// fault. Dropping the guard turns this 400 into the 500 that scope.Parse's
// failure produces from inside importer.Apply.
func TestImportWithoutScopeOrRepoIsBadRequest(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	res := doJSON(t, srv, http.MethodPost, "/v1/import", "alice", importBody(t, "", "", nil))
	body := readBody(t, res)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("import with neither scope nor repo = %d, want 400: %s", res.StatusCode, body)
	}
}
