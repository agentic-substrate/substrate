package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkedDirectoryFollowsManifestGitSHA(t *testing.T) {
	repo, gitPath, shaOld, shaNew, bodyOld, bodyNew := makeSkillsRepo(t)
	name := "team/alpha/lint"

	currentSHA := shaNew
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/render":
			_ = json.NewEncoder(w).Encode(map[string]any{"targets": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/skills/manifest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skills": []map[string]string{{
					"name":     name,
					"git_path": gitPath,
					"git_sha":  currentSHA,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	got := readLinkedSkill(t, home, name)
	if got != bodyNew {
		t.Fatalf("linked SKILL.md after first pull:\n%s\nwant newer sha %s body:\n%s", got, shaNew, bodyNew)
	}

	currentSHA = shaOld
	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync after pin to older sha: %v", err)
	}
	got = readLinkedSkill(t, home, name)
	if got != bodyOld {
		t.Fatalf("linked SKILL.md after pin to older sha %s:\n%s\nwant:\n%s", shaOld, got, bodyOld)
	}
}

func TestLinkedDirectoryOmitsSkillNotInManifest(t *testing.T) {
	repo, gitPath, _, sha, _, _ := makeSkillsRepo(t)
	keep := "team/alpha/lint"
	drop := "team/beta/secret"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/render":
			_ = json.NewEncoder(w).Encode(map[string]any{"targets": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/skills/manifest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skills": []map[string]string{{
					"name":     keep,
					"git_path": gitPath,
					"git_sha":  sha,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(linkedSkillDir(home, keep)); err != nil {
		t.Fatalf("matching skill was not linked: %v", err)
	}
	if _, err := os.Stat(linkedSkillDir(home, drop)); err == nil {
		t.Fatalf("skill %q was linked though it is not in the manifest (SKILL-2)", drop)
	}
}

func makeSkillsRepo(t *testing.T) (dir, gitPath, shaOld, shaNew, bodyOld, bodyNew string) {
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
	shaOld = strings.TrimSpace(gitRevParse(t, dir))
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(bodyNew), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "newer")
	shaNew = strings.TrimSpace(gitRevParse(t, dir))
	return dir, gitPath, shaOld, shaNew, bodyOld, bodyNew
}

func gitRevParse(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD") //nolint:gosec // argv is a test literal; Dir is t.TempDir
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return string(out)
}

func linkedSkillDir(home, name string) string {
	return filepath.Join(home, ".agents", "skills", filepath.FromSlash(name))
}

func readLinkedSkill(t *testing.T, home, name string) string {
	t.Helper()
	path := filepath.Join(linkedSkillDir(home, name), "SKILL.md")
	b, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir
	if err != nil {
		t.Fatalf("read linked SKILL.md: %v", err)
	}
	claude := filepath.Join(home, ".claude", "skills", filepath.FromSlash(name))
	target, err := os.Readlink(claude)
	if err != nil {
		t.Fatalf("harness symlink %s: %v", claude, err)
	}
	want := linkedSkillDir(home, name)
	if target != want {
		t.Fatalf("symlink %s -> %s, want %s", claude, target, want)
	}
	return string(b)
}
