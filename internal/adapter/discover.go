package adapter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Discover finds git checkouts two levels deep under each root (`<root>/*/*`,
// EDD R26). Extra configured roots are scanned the same way.
func Discover(roots []string) ([]Workspace, error) {
	var out []Workspace
	for _, root := range roots {
		if root == "" {
			continue
		}
		projs, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("adapter: scan %s: %w", root, err)
		}
		for _, proj := range projs {
			if !proj.IsDir() {
				continue
			}
			projPath := filepath.Join(root, proj.Name())
			repos, err := os.ReadDir(projPath)
			if err != nil {
				continue
			}
			for _, repo := range repos {
				if !repo.IsDir() {
					continue
				}
				repoPath := filepath.Join(projPath, repo.Name())
				if !isGitCheckout(repoPath) {
					continue
				}
				ws, err := inspectCheckout(repoPath)
				if err != nil {
					return nil, err
				}
				out = append(out, ws)
			}
		}
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
