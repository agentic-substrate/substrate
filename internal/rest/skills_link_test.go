package rest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/adapter"
)

func TestAdapterLinkedDirectoryFollowsOlderActiveVersion(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)

	repo, gitPath, shaOld, shaNew, bodyOld, bodyNew := makeSkillsGit(t)
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

	var oldVer string
	if err := conn.QueryRow(t.Context(), `SELECT id::text FROM skill_version WHERE skill_id = $1 AND semver = '1.0.0'`, w.skillID).Scan(&oldVer); err != nil {
		t.Fatalf("lookup seeded version: %v", err)
	}
	newVer := id()
	exec(`UPDATE skill_version SET git_sha = $1, git_path = $2 WHERE id = $3`, shaOld, gitPath, oldVer)
	exec(`INSERT INTO skill_version (id, skill_id, semver, git_sha, git_path, author_id, approval)
		VALUES ($1, $2, '2.0.0', $3, $4, $5, 'approved')`, newVer, w.skillID, shaNew, gitPath, w.alice)
	exec(`UPDATE skill SET active_version_id = $1 WHERE id = $2`, newVer, w.skillID)

	offID := id()
	offName := "team/alpha/other-" + offID[:8]
	offVer := id()
	exec(`INSERT INTO skill (id, name, scope_id, visibility, owner_id, description)
		VALUES ($1, $2, $3, 'team', $4, 'off-chain')`, offID, offName, w.projectOff, w.alice)
	exec(`INSERT INTO skill_version (id, skill_id, semver, git_sha, git_path, author_id, approval)
		VALUES ($1, $2, '1.0.0', $3, 'skills/team/alpha/other', $4, 'approved')`, offVer, offID, shaNew, w.alice)
	exec(`UPDATE skill SET active_version_id = $1 WHERE id = $2`, offVer, offID)

	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)

	home := t.TempDir()
	roots := filepath.Join(home, "work")
	checkout := filepath.Join(roots, "proj", "api")
	if err := os.MkdirAll(checkout, 0o750); err != nil {
		t.Fatal(err)
	}
	runGit(t, checkout, "init", "-b", "main")
	runGit(t, checkout, "remote", "add", "origin", w.repoKey)

	state := filepath.Join(home, ".substrate", "adapter.sqlite")
	db, err := adapter.Open(state)
	if err != nil {
		t.Fatalf("adapter.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := adapter.Config{
		Server:     srv.URL,
		Token:      "alice",
		Machine:    "test",
		Home:       home,
		StatePath:  state,
		Roots:      []string{roots},
		SkillsRepo: repo,
	}

	if _, err := adapter.Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	got := readLinkedSKILL(t, home, w.skillName)
	if got != bodyNew {
		t.Fatalf("linked SKILL.md at active 2.0.0:\n%s\nwant sha %s:\n%s", got, shaNew, bodyNew)
	}
	if _, err := os.Stat(linkedDir(home, offName)); err == nil {
		t.Fatalf("off-chain skill %q was linked (SKILL-2)", offName)
	}

	exec(`UPDATE skill SET active_version_id = $1 WHERE id = $2`, oldVer, w.skillID)
	if _, err := adapter.Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync after pin to older version: %v", err)
	}
	got = readLinkedSKILL(t, home, w.skillName)
	if got != bodyOld {
		t.Fatalf("linked SKILL.md after moving active_version_id to older sha %s:\n%s\nwant:\n%s", shaOld, got, bodyOld)
	}
}

func makeSkillsGit(t *testing.T) (dir, gitPath, shaOld, shaNew, bodyOld, bodyNew string) {
	t.Helper()
	dir = t.TempDir()
	gitPath = "skills/team/alpha/lint"
	skillDir := filepath.Join(dir, filepath.FromSlash(gitPath))
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}
	bodyOld = "---\nname: lint\n---\n# older approved version\n"
	bodyNew = "---\nname: lint\n---\n# newer approved version\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(bodyOld), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "older")
	shaOld = strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(bodyNew), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "newer")
	shaNew = strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	return dir, gitPath, shaOld, shaNew, bodyOld, bodyNew
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // argv is test literals; Dir is t.TempDir
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // argv is test literals; Dir is t.TempDir
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func linkedDir(home, name string) string {
	return filepath.Join(home, ".agents", "skills", filepath.FromSlash(name))
}

func readLinkedSKILL(t *testing.T, home, name string) string {
	t.Helper()
	path := filepath.Join(linkedDir(home, name), "SKILL.md")
	b, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir
	if err != nil {
		t.Fatalf("read linked SKILL.md: %v", err)
	}
	return string(b)
}
