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

// The same three endpoints with no repo key at all, in the same order. A key
// the caller may not read falls back to exactly these responses.
func noRepoReadPaths() []string {
	return []string{
		"/v1/render?machine=wsl",
		"/v1/skills/manifest",
		"/v1/memory/cache",
	}
}

// A repo key bound to another team and a repo key bound nowhere must be told
// apart by nobody: before #109 the first answered 200 and the second 403, and
// the status code alone enumerated which repo keys this control plane binds.
//
// The gate is a fallback, not a refusal. Both cases resolve to nothing and the
// handler renders the global chain, so both answer 200 with the byte-identical
// body the same request produces with no ?repos= at all -- asserted here as the
// third leg, because "both are 403" would close the oracle too while taking
// visibility='global' content away from readers it was published for.
//
// Making scope_readable return true unconditionally turns this red: the foreign
// key then resolves to team alpha's chain for bob and its body diverges from
// both the missing-key body and the no-repos body.
func TestReadEndpointsRepoDenialIsIndistinguishable(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	const missingKey = "github.com/acme/does-not-exist"
	for i, foreign := range repoReadPaths(w.repoKey) {
		missing := repoReadPaths(missingKey)[i]
		none := noRepoReadPaths()[i]
		// bob is a member of team beta; the repo is bound under team alpha.
		fRes := doJSON(t, srv, http.MethodGet, foreign, "bob", nil)
		fBody := readBody(t, fRes)
		mRes := doJSON(t, srv, http.MethodGet, missing, "bob", nil)
		mBody := readBody(t, mRes)
		nRes := doJSON(t, srv, http.MethodGet, none, "bob", nil)
		nBody := readBody(t, nRes)
		if fRes.StatusCode != http.StatusOK || mRes.StatusCode != http.StatusOK || nRes.StatusCode != http.StatusOK {
			t.Fatalf("%s: codes = %d (foreign), %d (missing), %d (no repos), want 200 for all:\n%s\n%s\n%s",
				foreign, fRes.StatusCode, mRes.StatusCode, nRes.StatusCode, fBody, mBody, nBody)
		}
		if fBody != mBody {
			t.Fatalf("%s: a foreign repo and a missing repo are distinguishable:\n foreign: %s\n missing: %s",
				foreign, fBody, mBody)
		}
		if fBody != nBody {
			t.Fatalf("%s: a repo key the caller may not read did not fall back to the no-repos response:\n foreign: %s\n none:    %s",
				foreign, fBody, nBody)
		}
	}
}

// The content half of the fallback, and the anti-vacuity control for the
// byte-comparison above: `global-public-fact` is authored at team alpha's
// project with visibility='global', an explicit decision that the row is
// readable by everyone. bob is not a member of team alpha and still receives it
// through the foreign repo key. Refusing the key with a 403 -- the shape this
// PR carried before review -- turns this red.
func TestReadEndpointsFallbackPreservesGlobalVisibility(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	const cache = 2 // /v1/memory/cache in repoReadPaths order
	res := doJSON(t, srv, http.MethodGet, repoReadPaths(w.repoKey)[cache], "bob", nil)
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bob naming a foreign repo key = %d, want 200: %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "global-public-fact") {
		t.Fatalf("a non-member lost visibility='global' content by naming a repo key:\n%s", body)
	}
	// The row bob must still not see, so this is not passing because RLS was
	// disabled wholesale.
	if strings.Contains(body, "team-secret-fact") {
		t.Fatalf("bob (non-member) received team-visible content:\n%s", body)
	}
}

// The positive control for the gate. An always-false scope_readable keeps
// TestReadEndpointsRepoDenialIsIndistinguishable green -- every caller falls
// back and every body matches -- so this asserts the content a member must
// still receive through the repo chain; an always-false predicate turns it red.
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
// dropping the is_admin arm from scope_readable silently drops it to the global
// chain and it loses every marker below (#103 left this untested).
func TestReadEndpointsResolveRepoForAdmin(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	want := []string{"python.version", w.skillName, "team-secret-fact"}
	for i, path := range repoReadPaths(w.repoKey) {
		res := doJSON(t, srv, http.MethodGet, path, "admin", nil)
		body := readBody(t, res)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s as human_admin = %d, want 200: %s", path, res.StatusCode, body)
		}
		if !strings.Contains(body, want[i]) {
			t.Fatalf("%s as human_admin is missing %q; the is_admin bypass did not resolve the key:\n%s",
				path, want[i], body)
		}
	}
}
