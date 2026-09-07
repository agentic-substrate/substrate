package rest

import (
	"io"
	"net/http"
	"testing"
)

func TestSkillsManifestOmitsOffChainSkill(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)

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

	offID := id()
	offName := "team/alpha/other-" + offID[:8]
	verID := id()
	exec(`INSERT INTO skill (id, name, scope_id, visibility, owner_id, description)
		VALUES ($1, $2, $3, 'team', $4, 'off-chain skill')`, offID, offName, w.projectOff, w.alice)
	exec(`INSERT INTO skill_version (id, skill_id, semver, git_sha, git_path, author_id, approval)
		VALUES ($1, $2, '1.0.0', 'offchainsha', 'skills/team/alpha/other', $3, 'approved')`, verID, offID, w.alice)
	exec(`UPDATE skill SET active_version_id = $1 WHERE id = $2`, verID, offID)

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
			Name string `json:"name"`
		} `json:"skills"`
	}
	decodeJSON(t, res, &out)

	sawOnChain := false
	for _, s := range out.Skills {
		if s.Name == w.skillName {
			sawOnChain = true
		}
		if s.Name == offName {
			t.Fatalf("alice saw off-chain skill %q; SKILL-2 links only skills on this machine's repo chain", offName)
		}
	}
	if !sawOnChain {
		t.Fatal("alice missing on-chain team skill; the test cannot prove off-chain filtering")
	}
}
