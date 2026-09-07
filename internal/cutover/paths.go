package cutover

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/agentic-substrate/substrate/internal/adapter"
	"github.com/agentic-substrate/substrate/internal/render"
)

// Destinations maps a render.Specs path to local files under home and
// discovered checkouts. Absolute server paths are refused.
func Destinations(home string, checkouts []string, serverPath string) ([]string, error) {
	if serverPath == "" {
		return nil, fmt.Errorf("cutover: empty target path")
	}
	if filepath.IsAbs(serverPath) {
		return nil, fmt.Errorf("cutover: refusing absolute path %s", serverPath)
	}
	if strings.Contains(serverPath, "..") {
		return nil, fmt.Errorf("cutover: refusing path %s", serverPath)
	}
	if !allowedRenderPath(serverPath) {
		return nil, fmt.Errorf("cutover: refusing path %s", serverPath)
	}
	if strings.HasPrefix(serverPath, "~/") {
		if home == "" {
			return nil, fmt.Errorf("cutover: home is required for %s", serverPath)
		}
		dest := filepath.Join(home, filepath.FromSlash(serverPath[2:]))
		if err := adapter.Confine(home, dest); err != nil {
			return nil, fmt.Errorf("cutover: path %s escapes home", serverPath)
		}
		return []string{dest}, nil
	}
	var dests []string
	for _, c := range checkouts {
		dest := filepath.Join(c, filepath.FromSlash(serverPath))
		if err := adapter.Confine(c, dest); err != nil {
			return nil, fmt.Errorf("cutover: path %s escapes checkout %s", serverPath, c)
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
