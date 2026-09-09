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
}

func newPerRepoFake(t *testing.T, bodyFor func(repos []string) string) *perRepoFake {
	t.Helper()
	f := &perRepoFake{bodyFor: bodyFor, rendered: map[string]struct{}{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/render", func(w http.ResponseWriter, r *http.Request) {
		repos := splitCSV(r.URL.Query().Get("repos"))
		f.mu.Lock()
		f.queries = append(f.queries, strings.Join(repos, ","))
		f.mu.Unlock()
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
	mux.HandleFunc("POST /v1/review", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
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
