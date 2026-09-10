package rest

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/agentic-substrate/substrate/internal/importer"
)

// previewBody is importBody with a second block whose kind lands at a
// different depth: plannedTarget files a preference at the team scope and an
// instruction at the leaf, so a preview that reports the depth has two
// distinct answers to report.
func previewBody(t *testing.T, repo, scopePath string) map[string]any {
	t.Helper()
	plan, witness := importScan(t)
	return map[string]any{
		"machine":           "wsl",
		"trusted_machine":   "wsl",
		"client_id":         uuid.NewString(),
		"repo":              repo,
		"scope":             scopePath,
		"plan":              plan,
		"inventory_witness": witness,
	}
}

// planRows decodes every planned row of an /v1/import response.
func planRows(t *testing.T, body string) []importer.PlannedRow {
	t.Helper()
	var res importer.ApplyResult
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode import response: %v\n%s", err, body)
	}
	rows := make([]importer.PlannedRow, 0)
	rows = append(rows, res.Active...)
	rows = append(rows, res.Proposed...)
	rows = append(rows, res.Conflict...)
	rows = append(rows, res.Memory...)
	if len(rows) == 0 {
		t.Fatalf("no planned rows to inspect:\n%s", body)
	}
	return rows
}

// A repo-keyed dry run still previews where each row lands, as the leaf kind
// of the chain and nothing more (#108). Restoring the blanking loop of #103 --
// `rows[i].Scope = ""` -- turns this red on the empty-kind check; stamping the
// full chain instead turns it red on the leak check below it.
func TestImportRepoPreviewReportsLeafKind(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	res := doJSON(t, srv, http.MethodPost, "/v1/import", "alice", previewBody(t, w.repoKey, ""))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("repo-keyed dry run = %d, want 200: %s", res.StatusCode, body)
	}
	kinds := map[string]int{}
	for _, row := range planRows(t, body) {
		if row.Scope == "" {
			t.Fatalf("row %s/%s has no placement preview at all:\n%s", row.Kind, row.Key, body)
		}
		if strings.Contains(row.Scope, ":") || strings.Contains(row.Scope, "/") {
			t.Fatalf("row %s/%s previews a chain %q, want the leaf kind alone:\n%s", row.Kind, row.Key, row.Scope, body)
		}
		kinds[row.Scope]++
	}
	// Anti-vacuity: the preview distinguishes depths, so a handler that
	// hard-coded "repo" for every row would fail here.
	if kinds["repo"] == 0 || kinds["team"] == 0 {
		t.Fatalf("preview kinds = %v, want both a repo-filed and a team-filed row:\n%s", kinds, body)
	}
	// Positive control: the resolved chain's own naming, not a generic shape.
	for _, secret := range []string{w.pathStr, "team:alpha", "project:secret", "acme-" + w.orgID[:8]} {
		if strings.Contains(body, secret) {
			t.Fatalf("the repo-keyed preview leaks %q from the resolved chain:\n%s", secret, body)
		}
	}
}

// One response shape: a scope-path caller gets the same leaf-kind field, not
// the full label. The client supplied the chain, so truncating its own
// argument to this kind reconstructs the label losslessly. Echoing
// `sc.String()` here again turns this red.
func TestImportScopePathPreviewReportsLeafKind(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	res := doJSON(t, srv, http.MethodPost, "/v1/import", "alice", previewBody(t, "", w.pathStr))
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("scope-path dry run = %d, want 200: %s", res.StatusCode, body)
	}
	for _, row := range planRows(t, body) {
		switch row.Scope {
		case "project", "team":
		default:
			t.Fatalf("row %s/%s previews %q, want the leaf kind of its target chain:\n%s", row.Kind, row.Key, row.Scope, body)
		}
	}
}

// The new field must not become the signal Gotcha 13 closed: a repo key bound
// nowhere and one bound where the caller may not write are refused before any
// row is planned, so both answer the identical body. Moving the leaf-kind
// stamp ahead of the denial, or reporting the depth in the denial, turns this
// red.
func TestImportPreviewAddsNoRepoOracle(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	post := func(repo string) (int, string) {
		res := doJSON(t, srv, http.MethodPost, "/v1/import", "bob", previewBody(t, repo, ""))
		return res.StatusCode, readBody(t, res)
	}
	foreignCode, foreignBody := post(w.repoKey)
	missingCode, missingBody := post("github.com/acme/does-not-exist")
	if foreignCode != http.StatusForbidden || missingCode != http.StatusForbidden {
		t.Fatalf("codes = %d (foreign) and %d (missing), want 403 for both:\n%s\n%s",
			foreignCode, missingCode, foreignBody, missingBody)
	}
	if foreignBody != missingBody {
		t.Fatalf("the preview made a foreign repo distinguishable from a missing one:\n foreign: %s\n missing: %s",
			foreignBody, missingBody)
	}
}
