package adapter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MaxDiscoverDepth is how far below a root a checkout may sit. Depth 1 is the
// flat `<root>/<repo>` layout, depth 2 the `<root>/<project>/<repo>` one; both
// are in use, so both are scanned (#55). Scope never comes from this shape —
// it is derived from the checkout's git remote — so the depth only decides
// what is looked at, never what it is called.
const MaxDiscoverDepth = 2

// Discover finds git checkouts up to MaxDiscoverDepth below each root. A
// directory holding a .git entry is a checkout and is not descended into, so a
// checkout's submodules and nested clones do not double-register. Worktree
// siblings (`<repo>-wt-*`) have their own .git file and are discovered like any
// other checkout; they share the primary checkout's remote and therefore its
// scope.
func Discover(roots []string) ([]Workspace, error) {
	var out []Workspace
	for _, root := range roots {
		if root == "" {
			continue
		}
		found, err := discoverUnder(root, 1)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

func discoverUnder(dir string, depth int) ([]Workspace, error) {
	if depth > MaxDiscoverDepth {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		if depth > 1 {
			// An unreadable subdirectory is skipped, not fatal: one bad mode
			// bit under a root must not stop the machine's other checkouts.
			return nil, nil
		}
		return nil, fmt.Errorf("adapter: scan %s: %w", dir, err)
	}
	var out []Workspace
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if isGitCheckout(path) {
			ws, err := inspectCheckout(path)
			if err != nil {
				return nil, err
			}
			out = append(out, ws)
			continue
		}
		nested, err := discoverUnder(path, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, nested...)
	}
	return out, nil
}

func isGitCheckout(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func inspectCheckout(dir string) (Workspace, error) {
	ws := Workspace{Path: dir}
	if remote, err := gitOutput(dir, "remote", "get-url", "origin"); err == nil {
		ws.Remote = NormalizeRemote(strings.TrimSpace(remote))
	}
	if branch, err := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		ws.Branch = strings.TrimSpace(branch)
	}
	return ws, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // argv is our literals; Dir is a discovered checkout
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// NormalizeRemote is the EDD §3 remote_norm form: scheme, userinfo, and
// trailing .git stripped, lowercased.
func NormalizeRemote(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimRight(s, "/")
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "@"); j >= 0 {
			s = s[j+1:]
		}
	} else if at := strings.Index(s, "@"); at >= 0 && strings.Contains(s[at+1:], ":") {
		rest := s[at+1:]
		host, path, ok := strings.Cut(rest, ":")
		if ok {
			s = host + "/" + strings.TrimPrefix(path, "/")
		}
	}
	return strings.ToLower(s)
}
