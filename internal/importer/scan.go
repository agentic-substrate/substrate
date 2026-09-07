package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// maxScanFile caps a harness file so a symlink-to-huge or DT_UNKNOWN
	// surprise cannot pull unbounded bytes into inventory.json.
	maxScanFile    int64 = 1 << 20
	maxMemorixJSON int64 = 8 << 20
)

// skipDirs are directories whose contents are never the operator's own
// config: version control internals, dependency trees, and caches. A
// vendored AGENTS.md from a third-party crate or a plugin cache is not an
// instruction this machine authored, and importing one poisons the store
// with rules nobody here wrote.
var skipDirs = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"vendor":        true,
	".substrate":    true,
	".cargo":        true,
	".rustup":       true,
	".cache":        true,
	"cache":         true,
	"marketplaces":  true,
	"containers":    true,
	"site-packages": true,
	".venv":         true,
	"venv":          true,
	"target":        true,
	"dist":          true,
	"__pycache__":   true,
	".mypy_cache":   true,
	".pytest_cache": true,
}

// skipPathContains are dependency and plugin caches that no directory name
// alone identifies. A CLAUDE.md inside the Go module cache belongs to the
// module's author, not to this machine.
var skipPathContains = []string{
	"/go/pkg/mod/",
	"/.codex/.tmp/",
	"/.claude/plugins/",
	"/.codex/plugins/",
	"/.cursor/plugins/",
}

// excluder matches slash-relative paths against operator-supplied globs.
// "**" is expanded to match across separators, which path.Match cannot do.
type excluder struct{ res []*regexp.Regexp }

func newExcluder(patterns []string) (*excluder, error) {
	e := &excluder{}
	for _, p := range patterns {
		if p == "" {
			continue
		}
		// Validate as a shell glob first. globToRegexp quotes everything it
		// does not handle, so "[bad" would compile into a harmless literal
		// and silently match nothing — a typo indistinguishable from a
		// working filter.
		if _, err := path.Match(strings.ReplaceAll(p, "**", "*"), "probe"); err != nil {
			return nil, fmt.Errorf("import scan: -exclude %q: %w", p, err)
		}
		re, err := globToRegexp(p)
		if err != nil {
			return nil, fmt.Errorf("import scan: -exclude %q: %w", p, err)
		}
		e.res = append(e.res, re)
	}
	return e, nil
}

func (e *excluder) match(rel string) bool {
	for _, re := range e.res {
		if re.MatchString(rel) {
			return true
		}
	}
	return false
}

func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				b.WriteString(".*")
				continue
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, err
	}
	return re, nil
}

func skipPath(path string) bool {
	slash := filepath.ToSlash(path)
	for _, frag := range skipPathContains {
		if strings.Contains(slash, frag) {
			return true
		}
	}
	return false
}

// Scan inventories harness config under explicit absolute roots. It never
// consults $HOME and never writes under those roots. Memorix is ingested
// only from a pre-exported JSON file; scan never execs the memorix binary.
func Scan(req Request) (*Inventory, error) {
	if len(req.Roots) == 0 {
		return nil, fmt.Errorf("import scan: -root is required (refuses to guess $HOME)")
	}
	for _, root := range req.Roots {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("import scan: -root must be an absolute path, got %q (refuses to guess $HOME)", root)
		}
	}
	if req.Hostname == "" {
		return nil, fmt.Errorf("import scan: -hostname is required")
	}

	inv := &Inventory{
		Hostname: req.Hostname,
		Roots:    append([]string(nil), req.Roots...),
	}
	ex, err := newExcluder(req.Exclude)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, root := range req.Roots {
		if err := walkRoot(root, seen, inv, ex); err != nil {
			return nil, err
		}
	}
	sort.Slice(inv.Files, func(i, j int) bool {
		return inv.Files[i].Rel < inv.Files[j].Rel
	})
	if err := collectMemorix(req, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

func walkRoot(root string, seen map[string]struct{}, inv *Inventory, ex *excluder) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory somewhere under the root is normal on a
			// real machine (container storage, another user's files). Aborting
			// the whole scan there would make the command useless against the
			// home it exists to inventory, so record it and keep walking.
			inv.Skipped = append(inv.Skipped, Skipped{Source: path, Reason: err.Error()})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "..") {
			return nil
		}
		if skipPath(path) {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if ex.match(slash) {
			return nil
		}
		detected, scope, ok := detectFile(slash)
		if !ok {
			return nil
		}
		if _, dup := seen[path]; dup {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			inv.Skipped = append(inv.Skipped, Skipped{Source: path, Reason: err.Error()})
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > maxScanFile {
			inv.Skipped = append(inv.Skipped, Skipped{
				Source: path,
				Reason: fmt.Sprintf("file exceeds %d byte cap", maxScanFile),
			})
			return nil
		}
		seen[path] = struct{}{}
		body, err := os.ReadFile(path) //nolint:gosec // path is confined to an operator-supplied root; Lstat required a regular file
		if err != nil {
			inv.Skipped = append(inv.Skipped, Skipped{Source: path, Reason: err.Error()})
			return nil
		}
		inv.Files = append(inv.Files, File{
			Path:         path,
			Rel:          slash,
			Size:         int64(len(body)),
			Hash:         sha256Hex(body),
			DetectedType: detected,
			ImpliedScope: scope,
			Content:      string(body),
		})
		return nil
	})
}

func detectFile(rel string) (detected, scope string, ok bool) {
	switch rel {
	case ".claude/CLAUDE.md":
		return "claude", "user", true
	case ".codex/AGENTS.md":
		return "agents", "user", true
	}
	if strings.HasPrefix(rel, ".cursor/rules/") {
		rest := strings.TrimPrefix(rel, ".cursor/rules/")
		if rest != "" {
			return "cursor", "user", true
		}
		return "", "", false
	}
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	switch base {
	case "AGENTS.md":
		return "agents", "repo", true
	case "CLAUDE.md":
		return "claude", "repo", true
	}
	return "", "", false
}

func collectMemorix(req Request, inv *Inventory) error {
	if req.MemorixJSON == "" {
		inv.Skipped = append(inv.Skipped, Skipped{
			Source: "memorix",
			Reason: "memorix JSON not provided; skipping that source",
		})
		return nil
	}
	if !filepath.IsAbs(req.MemorixJSON) {
		return fmt.Errorf("import scan: -memorix-json must be an absolute path, got %q", req.MemorixJSON)
	}
	path := filepath.Clean(req.MemorixJSON)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("import scan: memorix JSON %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("import scan: -memorix-json %s is not a regular file", path)
	}
	if info.Size() > maxMemorixJSON {
		return fmt.Errorf("import scan: -memorix-json %s exceeds %d byte cap", path, maxMemorixJSON)
	}
	body, err := os.ReadFile(path) //nolint:gosec // operator-supplied pre-exported memorix JSON
	if err != nil {
		return fmt.Errorf("import scan: read memorix JSON %s: %w", path, err)
	}
	inv.Files = append(inv.Files, File{
		Path:         path,
		Rel:          "memorix",
		Size:         int64(len(body)),
		Hash:         sha256Hex(body),
		DetectedType: "memorix",
		ImpliedScope: "user",
		Content:      string(body),
	})
	return nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
