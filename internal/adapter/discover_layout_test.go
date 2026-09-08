package adapter

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFlatAndNestedLayoutsGetTheSameScope is the #55 acceptance in one test:
// the same repo checked out flat at <root>/<repo> and nested at
// <root>/<org>/<repo> is discovered in both places and gets the same scope,
// because the scope comes from the git remote and not from the path.
func TestFlatAndNestedLayoutsGetTheSameScope(t *testing.T) {
	const origin = "git@github.com:acme/known.git"
	flatRoot := t.TempDir()
	nestedRoot := t.TempDir()
	flat := filepath.Join(flatRoot, "known")
	nested := filepath.Join(nestedRoot, "acme", "known")
	initRepo(t, flat, origin)
	initRepo(t, nested, origin)
	for _, dir := range []string{flat, nested} {
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("hand-edited\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	home := t.TempDir()
	srv := newFake(t)
	srv.bound["github.com/acme/known"] = true
	srv.setTargets(agentsTarget(t))
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{flatRoot, nestedRoot}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	byPath := map[string]string{}
	for _, r := range srv.reviews() {
		byPath[r.Path] = r.Scope
	}
	flatScope, ok := byPath[filepath.Join(flat, "AGENTS.md")]
	if !ok {
		t.Fatalf("flat <root>/<repo> checkout was not managed; proposals: %+v", byPath)
	}
	nestedScope, ok := byPath[filepath.Join(nested, "AGENTS.md")]
	if !ok {
		t.Fatalf("nested <root>/<org>/<repo> checkout was not managed; proposals: %+v", byPath)
	}
	if flatScope != nestedScope {
		t.Fatalf("same remote got different scopes: flat %q, nested %q", flatScope, nestedScope)
	}
}

// TestWorktreeSiblingSharesThePrimaryScope: `<repo>-wt-*` siblings each have
// their own .git and are discovered like any other checkout; sharing a remote,
// they share a scope.
func TestWorktreeSiblingSharesThePrimaryScope(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "known")
	initRepo(t, primary, "https://github.com/acme/known")
	runGit(t, primary, "commit", "--allow-empty", "-m", "init")
	wt := filepath.Join(root, "known-wt-issue-55")
	runGit(t, primary, "worktree", "add", "-b", "wt", wt)

	spaces, err := Discover([]string{root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	got := map[string]string{}
	for _, w := range spaces {
		got[w.Path] = w.Remote
	}
	if got[primary] == "" {
		t.Fatalf("primary checkout not discovered: %+v", got)
	}
	if got[wt] != got[primary] {
		t.Fatalf("worktree sibling remote = %q, want the primary's %q", got[wt], got[primary])
	}
}
