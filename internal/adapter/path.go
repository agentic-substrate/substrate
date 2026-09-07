package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentic-substrate/substrate/internal/render"
)

func destsFor(cfg Config, serverPath string, checkouts []Workspace) ([]string, error) {
	if serverPath == "" {
		return nil, fmt.Errorf("adapter: empty target path")
	}
	if filepath.IsAbs(serverPath) {
		return nil, fmt.Errorf("adapter: refusing absolute path %s", serverPath)
	}
	if strings.Contains(serverPath, "..") {
		return nil, fmt.Errorf("adapter: refusing path %s", serverPath)
	}
	if !allowedRenderPath(serverPath) {
		return nil, fmt.Errorf("adapter: refusing path %s", serverPath)
	}
	if strings.HasPrefix(serverPath, "~/") {
		dest := filepath.Join(cfg.Home, filepath.FromSlash(serverPath[2:]))
		if err := Confine(cfg.Home, dest); err != nil {
			return nil, fmt.Errorf("adapter: path %s escapes home", serverPath)
		}
		return []string{dest}, nil
	}
	var dests []string
	for _, c := range checkouts {
		dest := filepath.Join(c.Path, filepath.FromSlash(serverPath))
		if err := Confine(c.Path, dest); err != nil {
			return nil, fmt.Errorf("adapter: path %s escapes checkout %s", serverPath, c.Path)
		}
		dests = append(dests, dest)
	}
	return dests, nil
}

func allowedRenderPath(serverPath string) bool {
	home := strings.HasPrefix(serverPath, "~/")
	for _, spec := range render.Specs() {
		for _, p := range spec.Paths {
			if p != serverPath {
				continue
			}
			if home {
				return strings.HasPrefix(p, "~/")
			}
			return !strings.HasPrefix(p, "~/")
		}
	}
	return false
}

// Confine reports whether dest stays inside root after resolving symlinks.
// A dest-dir symlink that points outside root is an error — lexical
// filepath.Rel would miss it.
func Confine(root, dest string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	resolvedDest, err := resolveDest(dest)
	if err != nil {
		return err
	}
	if !within(resolvedRoot, resolvedDest) {
		return fmt.Errorf("escapes %s", resolvedRoot)
	}
	return nil
}

// resolveDest EvalSymlinks dest, or dest's nearest existing ancestor plus the
// missing tail, so a not-yet-created nested path can still be confined.
func resolveDest(dest string) (string, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	var tail []string
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("adapter: cannot resolve %s", dest)
		}
		tail = append([]string{filepath.Base(cur)}, tail...)
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			return filepath.Join(append([]string{resolved}, tail...)...), nil
		}
		if _, statErr := os.Lstat(parent); statErr == nil {
			return "", err
		}
		cur = parent
	}
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func targetName(serverPath string) string {
	switch {
	case strings.Contains(serverPath, "CLAUDE.md"):
		return "claude"
	case strings.Contains(serverPath, "substrate.mdc"):
		return "cursor"
	default:
		return "agents"
	}
}
