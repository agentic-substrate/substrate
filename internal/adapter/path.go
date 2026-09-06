package adapter

import (
	"fmt"
	"path/filepath"
	"strings"
)

func destsFor(cfg Config, serverPath string, checkouts []Workspace) ([]string, error) {
	if serverPath == "" {
		return nil, fmt.Errorf("adapter: empty target path")
	}
	if strings.HasPrefix(serverPath, "~/") {
		dest := filepath.Join(cfg.Home, filepath.FromSlash(serverPath[2:]))
		if !within(cfg.Home, dest) {
			return nil, fmt.Errorf("adapter: path %s escapes home", serverPath)
		}
		return []string{dest}, nil
	}
	if filepath.IsAbs(serverPath) {
		return nil, fmt.Errorf("adapter: refusing absolute path %s", serverPath)
	}
	if strings.Contains(serverPath, "..") {
		return nil, fmt.Errorf("adapter: refusing path %s", serverPath)
	}
	var dests []string
	for _, c := range checkouts {
		dest := filepath.Join(c.Path, filepath.FromSlash(serverPath))
		if !within(c.Path, dest) {
			return nil, fmt.Errorf("adapter: path %s escapes checkout %s", serverPath, c.Path)
		}
		dests = append(dests, dest)
	}
	return dests, nil
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
