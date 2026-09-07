package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const memorixTimeout = 10 * time.Second

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".substrate":   true,
}

// Scan inventories harness config under explicit absolute roots. It never
// consults $HOME and never writes under those roots.
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
	seen := make(map[string]struct{})
	for _, root := range req.Roots {
		if err := walkRoot(root, seen, inv); err != nil {
			return nil, err
		}
	}
	sort.Slice(inv.Files, func(i, j int) bool {
		return inv.Files[i].Rel < inv.Files[j].Rel
	})
	collectMemorix(req, inv)
	return inv, nil
}

func walkRoot(root string, seen map[string]struct{}, inv *Inventory) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "..") {
			return nil
		}
		slash := filepath.ToSlash(rel)
		detected, scope, ok := detectFile(slash)
		if !ok {
			return nil
		}
		if _, dup := seen[path]; dup {
			return nil
		}
		seen[path] = struct{}{}
		body, err := os.ReadFile(path) //nolint:gosec // path is confined to an operator-supplied root
		if err != nil {
			return fmt.Errorf("import scan: read %s: %w", path, err)
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

func collectMemorix(req Request, inv *Inventory) {
	look := req.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look("memorix")
	if err != nil {
		inv.Skipped = append(inv.Skipped, Skipped{
			Source: "memorix",
			Reason: "memorix binary not found; skipping that source",
		})
		return
	}
	run := req.RunCmd
	if run == nil {
		run = runMemorix
	}
	out, err := run(bin, []string{"transfer", "export", "--format", "json"})
	if err != nil {
		inv.Skipped = append(inv.Skipped, Skipped{
			Source: "memorix",
			Reason: "memorix transfer export failed: " + err.Error(),
		})
		return
	}
	inv.Files = append(inv.Files, File{
		Path:         bin,
		Rel:          "memorix",
		Size:         int64(len(out)),
		Hash:         sha256Hex(out),
		DetectedType: "memorix",
		ImpliedScope: "user",
		Content:      string(out),
	})
}

func runMemorix(name string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), memorixTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // name is exec.LookPath("memorix"); args are fixed
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	return stdout.Bytes(), nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
