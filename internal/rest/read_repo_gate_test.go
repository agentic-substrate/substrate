package rest

import (
	"net/http"
	"strings"
	"testing"
)

// The three repo-keyed read endpoints. Each takes ?repos= and each resolved
// the key through resolveRepoPaths with no membership gate before #109.
func repoReadPaths(key string) []string {
	return []string{
		"/v1/render?machine=wsl&repos=" + key,
		"/v1/skills/manifest?repos=" + key,
		"/v1/memory/cache?repos=" + key,
	}
}

// Dropping the scope_readable gate in resolveRepoPaths turns this red: a
// foreign-but-existing repo key answers 200 while a nonexistent one answers
// 403, and the status code alone enumerates the repo keys this control plane
// binds -- the disclosure maskRepoDenial exists to prevent (#109, #103).
func TestReadEndpointsRepoDenialIsIndistinguishable(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	const missingKey = "github.com/acme/does-not-exist"
	for i, foreign := range repoReadPaths(w.repoKey) {
		missing := repoReadPaths(missingKey)[i]
		// bob is a member of team beta; the repo is bound under team alpha.
		fRes := doJSON(t, srv, http.MethodGet, foreign, "bob", nil)
		fBody := readBody(t, fRes)
		mRes := doJSON(t, srv, http.MethodGet, missing, "bob", nil)
		mBody := readBody(t, mRes)
		if fRes.StatusCode != http.StatusForbidden || mRes.StatusCode != http.StatusForbidden {
			t.Fatalf("%s: codes = %d (foreign) and %d (missing), want 403 for both:\n%s\n%s",
				foreign, fRes.StatusCode, mRes.StatusCode, fBody, mBody)
		}
		if fBody != mBody {
			t.Fatalf("%s: a foreign repo and a missing repo are distinguishable:\n foreign: %s\n missing: %s",
				foreign, fBody, mBody)
		}
		if !strings.Contains(fBody, "not available to this principal") {
			t.Fatalf("%s: expected the uniform denial body, got %s", foreign, fBody)
		}
	}
}

// The positive control for the gate above. An always-false predicate keeps
// TestReadEndpointsRepoDenialIsIndistinguishable green, so this asserts the
// content a member must still receive; making scope_readable return false
// unconditionally turns this red.
func TestReadEndpointsStillServeAMember(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	// One marker per endpoint, each only reachable through the repo chain.
	want := []string{"python.version", w.skillName, "team-secret-fact"}
	for i, path := range repoReadPaths(w.repoKey) {
		res := doJSON(t, srv, http.MethodGet, path, "alice", nil)
		body := readBody(t, res)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s as a member = %d, want 200: %s", path, res.StatusCode, body)
		}
		if !strings.Contains(body, want[i]) {
			t.Fatalf("%s as a member is missing %q:\n%s", path, want[i], body)
		}
	}
}

// scope_writable short-circuits on substrate.is_admin and whatever replaces it
// on the read paths must too. This admin holds no membership anywhere, so
// dropping the is_admin arm from scope_readable turns every one of these into
// the same 403 a foreign repo gets (#103 left this untested).
func TestReadEndpointsResolveRepoForAdmin(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	for _, path := range repoReadPaths(w.repoKey) {
		res := doJSON(t, srv, http.MethodGet, path, "admin", nil)
		body := readBody(t, res)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s as human_admin = %d, want 200: %s", path, res.StatusCode, body)
		}
	}
}
