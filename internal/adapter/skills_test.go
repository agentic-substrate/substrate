package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	// One-line production change that makes this go red: `return nil` at the
	// start of pruneSkills so a previously linked skill is never removed.
	repo := initSkillsRepo(t)
	keep := "team/alpha/lint"
	drop := "team/beta/secret"
	sha := commitSkills(t, repo, map[string]string{
		"skills/" + keep: "---\nname: lint\n---\n# keep\n",
		"skills/" + drop: "---\nname: secret\n---\n# drop\n",
	})
	var mu sync.Mutex
	manifest := []map[string]string{
		{"name": keep, "git_path": "skills/" + keep, "git_sha": sha},
		{"name": drop, "git_path": "skills/" + drop, "git_sha": sha},
	}
	srv := skillsManifestServer(t, func() []map[string]string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]map[string]string, len(manifest))
		copy(out, manifest)
		return out
	})

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if _, err := os.Stat(linkedSkillDir(home, keep)); err != nil {
		t.Fatalf("keep skill was not linked: %v", err)
	}
	if _, err := os.Stat(linkedSkillDir(home, drop)); err != nil {
		t.Fatalf("drop skill was not linked on first Sync: %v", err)
	}
	if _, err := os.Lstat(harnessSkillLink(home, drop)); err != nil {
		t.Fatalf("drop skill harness symlink missing on first Sync: %v", err)
	}

	mu.Lock()
	manifest = []map[string]string{
		{"name": keep, "git_path": "skills/" + keep, "git_sha": sha},
	}
	mu.Unlock()
	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if _, err := os.Stat(linkedSkillDir(home, keep)); err != nil {
		t.Fatalf("keep skill was pruned: %v", err)
	}
	if _, err := os.Stat(linkedSkillDir(home, drop)); err == nil {
		t.Fatalf("previously linked skill %q was not pruned (SKILL-2)", drop)
	}
	if _, err := os.Lstat(harnessSkillLink(home, drop)); err == nil {
		t.Fatalf("harness symlink for pruned skill %q still exists", drop)
	}
}

// One-line production change that makes this go red: `return err` from the
// materializeSkill loop in linkSkills (skip prune, fail Sync).
func TestArchiveFailureKeepsLastGoodCopyAndStillPrunes(t *testing.T) {
	repo := initSkillsRepo(t)
	alpha := "team/alpha/lint"
	beta := "team/beta/fmt"
	drop := "team/gamma/secret"
	sha := commitSkills(t, repo, map[string]string{
		"skills/" + alpha: "---\nname: lint\n---\n# alpha\n",
		"skills/" + beta:  "---\nname: fmt\n---\n# beta\n",
		"skills/" + drop:  "---\nname: secret\n---\n# secret\n",
	})
	missing := strings.Repeat("a", 40)

	var mu sync.Mutex
	manifest := []map[string]string{
		{"name": alpha, "git_path": "skills/" + alpha, "git_sha": sha},
		{"name": beta, "git_path": "skills/" + beta, "git_sha": sha},
		{"name": drop, "git_path": "skills/" + drop, "git_sha": sha},
	}
	srv := skillsManifestServer(t, func() []map[string]string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]map[string]string, len(manifest))
		copy(out, manifest)
		return out
	})

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	wantBeta := readLinkedSkill(t, home, beta)
	if _, err := os.Stat(linkedSkillDir(home, drop)); err != nil {
		t.Fatalf("drop skill missing after first Sync: %v", err)
	}

	mu.Lock()
	manifest = []map[string]string{
		{"name": alpha, "git_path": "skills/" + alpha, "git_sha": sha},
		{"name": beta, "git_path": "skills/" + beta, "git_sha": missing},
	}
	mu.Unlock()

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync after one skill's git archive failed: %v", err)
	}
	if got := readLinkedSkill(t, home, beta); got != wantBeta {
		t.Fatalf("failed skill was not kept at last good copy:\n%s\nwant:\n%s", got, wantBeta)
	}
	if _, err := os.Stat(linkedSkillDir(home, alpha)); err != nil {
		t.Fatalf("good skill was not linked: %v", err)
	}
	if _, err := os.Stat(linkedSkillDir(home, drop)); err == nil {
		t.Fatal("skill dropped from the manifest was not pruned after a sibling archive failure")
	}
	if _, err := os.Lstat(harnessSkillLink(home, drop)); err == nil {
		t.Fatal("harness symlink for the pruned skill still exists")
	}
}

// One-line production change that makes this go red: `return err` from
// linkSkills when ensureSkillsMirror fails (instead of keeping last copies).
func TestSkillsRepoFetchFailureKeepsLastLinkedVersion(t *testing.T) {
	repo, gitPath, _, sha, _, bodyNew := makeSkillsRepo(t)
	name := "team/alpha/lint"
	srv := skillsManifestServer(t, func() []map[string]string {
		return []map[string]string{{
			"name": name, "git_path": gitPath, "git_sha": sha,
		}}
	})
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	got := readLinkedSkill(t, home, name)
	if got != bodyNew {
		t.Fatalf("linked SKILL.md:\n%s\nwant:\n%s", got, bodyNew)
	}

	mirror := filepath.Join(home, ".substrate", "skills.git")
	runGit(t, mirror, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync after skills-repo fetch failure: %v", err)
	}
	got = readLinkedSkill(t, home, name)
	if got != bodyNew {
		t.Fatalf("SKILL.md changed after fetch failure:\n%s\nwant unchanged:\n%s", got, bodyNew)
	}
}

// One-line production change that makes this go red: `return err` from
// linkSkills when clone/fetch fails, so Run's first Sync exits the daemon.
func TestRunDoesNotExitWhenSkillsRepoFetchFails(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	srv := newFake(t)
	sseReady := make(chan struct{})
	srv.sseReady = sseReady

	cfg := testConfig(home, state, srv.URL)
	cfg.Interval = time.Hour
	cfg.SkillsRepo = filepath.Join(t.TempDir(), "missing.git")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, cfg) }()

	select {
	case <-sseReady:
	case err := <-errCh:
		t.Fatalf("Run returned on skills-repo fetch failure: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("SSE never connected")
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after cancel")
	}
}

// One-line production change that makes this go red: skip export when
// skill_link.git_sha still matches, even if dest is gone.
func TestMissingLinkedDirectoryIsReExportedAtSameSHA(t *testing.T) {
	repo, gitPath, _, sha, _, bodyNew := makeSkillsRepo(t)
	name := "team/alpha/lint"
	srv := skillsManifestServer(t, func() []map[string]string {
		return []map[string]string{{
			"name": name, "git_path": gitPath, "git_sha": sha,
		}}
	})
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if err := os.RemoveAll(linkedSkillDir(home, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync after dest deleted: %v", err)
	}
	got := readLinkedSkill(t, home, name)
	if got != bodyNew {
		t.Fatalf("re-exported SKILL.md:\n%s\nwant:\n%s", got, bodyNew)
	}
}

// One-line production change that makes this go red: gitSHAPattern allows {7,64}.
func TestSkillRefRejectsAbbreviatedGitSHA(t *testing.T) {
	err := validateSkillRef(manifestSkill{
		Name:    "team/alpha/lint",
		GitPath: "skills/team/alpha/lint",
		GitSHA:  "deadbee",
	})
	if err == nil {
		t.Fatal("7-char git_sha was accepted; pins must be full 40 or 64 hex")
	}
}

// One-line production change that makes this go red: skip rev-parse ^{commit}
// and archive the tree-ish as given, so a tree object id exports.
func TestSkillExportRejectsTreeObjectSHA(t *testing.T) {
	repo, gitPath, _, sha, _, _ := makeSkillsRepo(t)
	treeOut, err := gitOutput(repo, "rev-parse", sha+"^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	tree := strings.TrimSpace(treeOut)
	if tree == sha {
		t.Fatal("tree object id unexpectedly equals commit id")
	}
	name := "team/alpha/lint"
	srv := skillsManifestServer(t, func() []map[string]string {
		return []map[string]string{{
			"name": name, "git_path": gitPath, "git_sha": tree,
		}}
	})
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(linkedSkillDir(home, name)); err == nil {
		t.Fatal("tree object git_sha was archived; pin must resolve to a commit")
	}
}

// One-line production change that makes this go red: git archive without
// neutralizing export-subst, so $Format:%H$ is rewritten to the commit id.
func TestSkillExportKeepsExportSubstPlaceholders(t *testing.T) {
	repo := initSkillsRepo(t)
	gitPath := "skills/team/alpha/lint"
	body := "---\nname: lint\n---\n# $Format:%H$\n"
	skillDir := filepath.Join(repo, filepath.FromSlash(gitPath))
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, ".gitattributes"), []byte("SKILL.md export-subst\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "export-subst")
	sha := strings.TrimSpace(gitRevParse(t, repo))
	name := "team/alpha/lint"
	srv := skillsManifestServer(t, func() []map[string]string {
		return []map[string]string{{
			"name": name, "git_path": gitPath, "git_sha": sha,
		}}
	})
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	if _, err := Sync(context.Background(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	got := readLinkedSkill(t, home, name)
	if got != body {
		t.Fatalf("linked SKILL.md was rewritten (export-subst?):\n%s\nwant blob bytes:\n%s", got, body)
	}
	if strings.Contains(got, sha) {
		t.Fatalf("linked SKILL.md contains the commit id; export-subst leaked: %q", got)
	}
}

func skillsManifestServer(t *testing.T, skills func() []map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/render":
			_ = json.NewEncoder(w).Encode(map[string]any{"targets": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/skills/manifest":
			_ = json.NewEncoder(w).Encode(map[string]any{"skills": skills()})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/events":
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no flush", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func initSkillsRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	return dir
}

func commitSkills(t *testing.T, repo string, files map[string]string) string {
	t.Helper()
	for gitPath, body := range files {
		skillDir := filepath.Join(repo, filepath.FromSlash(gitPath))
		if err := os.MkdirAll(skillDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "skills")
	return strings.TrimSpace(gitRevParse(t, repo))
}

func harnessSkillLink(home, name string) string {
	return filepath.Join(home, ".claude", "skills", filepath.FromSlash(name))
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
	out, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return out
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

// Skills being switched off must be visible in the sync result, not only
// absent from it. An unconfigured SkillsRepo is the deployment's default
// (deploy/server/configmap.yaml ships SUBSTRATE_SKILLS_REPO empty), so a
// daemon with skills entirely disabled is otherwise indistinguishable from one
// that linked every skill correctly -- the same failure shape UnscopedRemotes
// and UnknownRemotes exist to prevent.
//
// One-line production change that makes this go red: drop the SkillsSkipped
// assignment from linkSkills' unconfigured branch.
func TestUnconfiguredSkillsRepoIsSurfacedNotSilent(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newFake(t)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = "" // the shipped default

	res, err := Sync(context.Background(), db, cfg)
	if err != nil {
		t.Fatalf("an unconfigured skills repo must not fail the cycle: %v", err)
	}
	if res.SkillsSkipped == "" {
		t.Fatal("skills were disabled and the sync result does not say so")
	}
	if !strings.Contains(res.SkillsSkipped, "not configured") {
		t.Fatalf("SkillsSkipped = %q; it must name the reason", res.SkillsSkipped)
	}
}

// A configured-but-broken remote is the other half: today it is logged and
// swallowed, so a permanently unreachable skills repo looks like a healthy
// cycle to everything downstream of Sync. It must stay non-fatal -- skills
// failing is not a reason to stop rendering instructions -- but it must be
// reported.
//
// One-line production change that makes this go red: drop the SkillsSkipped
// assignment from linkSkills' mirror-failure branch.
func TestUnreachableSkillsRepoIsSurfacedNotSilent(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newFake(t)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = filepath.Join(t.TempDir(), "missing.git")

	res, err := Sync(context.Background(), db, cfg)
	if err != nil {
		t.Fatalf("an unreachable skills repo must not fail the cycle: %v", err)
	}
	if res.SkillsSkipped == "" {
		t.Fatal("the skills mirror could not be built and the sync result does not say so")
	}
}

// The happy path must not report a skip, or the field is noise and stops being
// read -- the same reason the drift hash excludes the footer.
func TestWorkingSkillsRepoReportsNoSkip(t *testing.T) {
	repo, gitPath, _, sha, _, _ := makeSkillsRepo(t)
	srv := skillsManifestServer(t, func() []map[string]string {
		return []map[string]string{{
			"name": "team/alpha/lint", "git_path": gitPath, "git_sha": sha,
		}}
	})
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.SkillsRepo = repo

	res, err := Sync(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.SkillsSkipped != "" {
		t.Fatalf("a working skills repo reported a skip: %q", res.SkillsSkipped)
	}
}
