package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/agentic-substrate/substrate/internal/render"
)

// perRepoFake answers /v1/render the way the real server does: the content is a
// function of the repos asked about. Asking about two repos at once merges
// them, which is the shape of the defect -- the adapter must never ask that
// way, so the merged body must never reach any file on disk.
type perRepoFake struct {
	mu       sync.Mutex
	queries  []string
	URL      string
	bodyFor  func(repos []string) string
	rendered map[string]struct{}
	// failRepo is a repo whose /v1/render answers 500. A server-side failure
	// for one repo must not let a half-rendered machine reach disk.
	failRepo string
}

func newPerRepoFake(t *testing.T, bodyFor func(repos []string) string) *perRepoFake {
	t.Helper()
	f := &perRepoFake{bodyFor: bodyFor, rendered: map[string]struct{}{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/render", func(w http.ResponseWriter, r *http.Request) {
		repos := splitCSV(r.URL.Query().Get("repos"))
		f.mu.Lock()
		f.queries = append(f.queries, strings.Join(repos, ","))
		fail := f.failRepo
		f.mu.Unlock()
		for _, repo := range repos {
			if fail != "" && repo == fail {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
		}
		body := f.bodyFor(repos)
		var targets []target
		for _, spec := range render.Specs() {
			for _, p := range spec.Paths {
				targets = append(targets, target{Path: p, Content: body, SHA256: render.DriftHash(body)})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"targets": targets})
	})
	mux.HandleFunc("GET /v1/skills/manifest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"skills": []any{}})
	})
	// 201, not 202: postReview accepts only 200 and 201, so a 202 here makes
	// every drift proposal fail and the drift path -- the one that rewrites a
	// changed file -- dead in every test using this fake.
	mux.HandleFunc("POST /v1/review", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "{}", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

func (f *perRepoFake) renderQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

// One machine must not get one merged config (SCOPE-1, INST-4). Two checkouts
// under one machine each get their own repo's instructions, and neither gets
// the other's. The home-scoped file, of which there is exactly one per machine,
// carries the global render -- never a per-repo blob and never a merge.
func TestSyncRendersPerCheckoutNotMerged(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	initRepo(t, alpha, "https://github.com/acme/alpha.git")
	initRepo(t, beta, "https://github.com/acme/beta.git")

	const globalBody = "global-only rules\n"
	bodyFor := func(repos []string) string {
		if len(repos) == 0 {
			return globalBody
		}
		sorted := append([]string(nil), repos...)
		sort.Strings(sorted)
		var b strings.Builder
		for _, r := range sorted {
			b.WriteString("rules for " + r + "\n")
		}
		return b.String()
	}
	srv := newPerRepoFake(t, bodyFor)

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	wantAlpha := bodyFor([]string{"github.com/acme/alpha"})
	wantBeta := bodyFor([]string{"github.com/acme/beta"})
	if got := readFile(t, filepath.Join(alpha, "AGENTS.md")); got != wantAlpha {
		t.Fatalf("alpha/AGENTS.md = %q, want %q; each checkout gets only its own repo's instructions", got, wantAlpha)
	}
	if got := readFile(t, filepath.Join(beta, "AGENTS.md")); got != wantBeta {
		t.Fatalf("beta/AGENTS.md = %q, want %q", got, wantBeta)
	}
	if got := readFile(t, filepath.Join(alpha, "CLAUDE.md")); got != wantAlpha {
		t.Fatalf("alpha/CLAUDE.md = %q, want %q", got, wantAlpha)
	}

	// The home file is machine-wide, so it may carry nothing repo-specific.
	// Claude Code loads it on every session, which makes a merged blob here the
	// worst instance of the defect, not the mildest.
	for _, rel := range []string{".codex/AGENTS.md", ".claude/CLAUDE.md", ".cursor/rules/substrate.mdc"} {
		if got := readFile(t, filepath.Join(home, rel)); got != globalBody {
			t.Fatalf("~/%s = %q, want the global render %q", rel, got, globalBody)
		}
	}

	// No request may ever name more than one repo: a merged answer that is
	// discarded is still a merged answer the next refactor will start writing.
	for _, q := range srv.renderQueries() {
		if strings.Contains(q, ",") {
			t.Fatalf("render was asked about several repos at once: %q", q)
		}
	}
}

// Rendering twice with unchanged server content must leave every managed file
// byte-identical (Gotcha 9): a per-cycle difference would report drift forever.
func TestPerCheckoutRenderIsByteStableAcrossCycles(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	initRepo(t, alpha, "https://github.com/acme/alpha.git")
	srv := newPerRepoFake(t, func(repos []string) string {
		return "body for [" + strings.Join(repos, ",") + "]\n"
	})
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	managed := []string{
		filepath.Join(alpha, "AGENTS.md"),
		filepath.Join(home, ".claude/CLAUDE.md"),
	}
	var first []string
	for cycle := 1; cycle <= 2; cycle++ {
		if _, err := Sync(t.Context(), db, cfg); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
		var now []string
		for _, p := range managed {
			b, err := os.ReadFile(p) //nolint:gosec // p is under t.TempDir
			if err != nil {
				t.Fatalf("cycle %d: %v", cycle, err)
			}
			now = append(now, render.DriftHash(string(b)))
		}
		if cycle == 1 {
			first = now
			continue
		}
		for i := range managed {
			if now[i] != first[i] {
				t.Fatalf("%s changed between cycles: %s -> %s", managed[i], first[i], now[i])
			}
		}
	}
}

// managedTree lists every file under dir relative to dir, skipping the git
// metadata a checkout carries and the adapter's own state database. A test
// that only reads the paths it expects cannot see a file the adapter should
// never have written -- an other-repo leftover, or a stale path from a
// previous layout -- so the tree itself is asserted, not just its members.
func managedTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(filepath.Base(rel), "adapter.sqlite") {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

func assertManagedTree(t *testing.T, dir string, want []string) {
	t.Helper()
	got := managedTree(t, dir)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s contains %v, want exactly %v; an unexpected file means the adapter wrote somewhere it should not have", dir, got, want)
	}
}

// A checkout must contain the managed files and nothing else. Reading only the
// expected paths cannot catch a stale or leftover file written beside them,
// and a stale AGENTS.md variant is read by the harness just like a live one.
func TestSyncWritesNoFilesOutsideTheManagedSet(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	initRepo(t, alpha, "https://github.com/acme/alpha.git")
	initRepo(t, beta, "https://github.com/acme/beta.git")

	srv := newPerRepoFake(t, func(repos []string) string {
		return "body for [" + strings.Join(repos, ",") + "]\n"
	})
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	assertManagedTree(t, alpha, []string{"AGENTS.md", "CLAUDE.md"})
	assertManagedTree(t, beta, []string{"AGENTS.md", "CLAUDE.md"})
	assertManagedTree(t, home, []string{
		".codex/AGENTS.md",
		".claude/CLAUDE.md",
		".cursor/rules/substrate.mdc",
	})
}

// One repo's render failing must leave the machine exactly as it was, not
// half-written: fetchTargets abandons the cycle, and a sibling checkout that
// was already managed keeps the bytes it had. A partial machine is worse than
// a stale one, because nothing reports it.
func TestSyncWritesNothingWhenOneRepoRenderFails(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	initRepo(t, alpha, "https://github.com/acme/alpha.git")
	initRepo(t, beta, "https://github.com/acme/beta.git")

	srv := newPerRepoFake(t, func(repos []string) string {
		return "body for [" + strings.Join(repos, ",") + "]\n"
	})
	srv.failRepo = "github.com/acme/beta"

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	// Seed a managed file so "unchanged" is an assertion about a write that
	// did not happen, not about a file that was never created.
	const seeded = "previous cycle's alpha rules\n"
	seededPath := filepath.Join(alpha, "AGENTS.md")
	if err := os.WriteFile(seededPath, []byte(seeded), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(t.Context(), db, cfg); err == nil {
		t.Fatal("Sync returned nil with one repo's render failing; a partially rendered machine must be an error")
	}

	got := readFile(t, seededPath)
	if got != seeded {
		t.Fatalf("alpha/AGENTS.md = %q, want it left byte-identical at %q", got, seeded)
	}
	assertManagedTree(t, alpha, []string{"AGENTS.md"})
	assertManagedTree(t, beta, nil)
	assertManagedTree(t, home, nil)
}
