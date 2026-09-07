package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentic-substrate/substrate/internal/importer"
)

func importCmd(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("want scan, plan, apply, or cutover subcommand")
	}
	switch args[0] {
	case "scan":
		return importScan(args[1:], stdout, stderr)
	case "plan":
		return importPlan(args[1:], stdout, stderr)
	case "apply":
		return importApply(args[1:], stdout, stderr)
	case "cutover":
		return importCutover(args[1:], stdout, stderr, nil)
	default:
		return fmt.Errorf("unknown import command %q", args[0])
	}
}

func importScan(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("import scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var roots []string
	fs.Func("root", "absolute search root (repeatable; required; no $HOME default)", func(s string) error {
		roots = append(roots, s)
		return nil
	})
	hostname := fs.String("hostname", "", "machine name tagged on inventoried files")
	out := fs.String("out", "", "absolute path to write inventory.json")
	memorixJSON := fs.String("memorix-json", "", "absolute path to a pre-exported memorix JSON file")
	var exclude []string
	fs.Func("exclude", "glob matched against a file's path relative to its root; ** spans separators (repeatable)", func(s string) error {
		exclude = append(exclude, s)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(roots) == 0 {
		return fmt.Errorf("import scan: -root is required (refuses to guess $HOME)")
	}
	if *hostname == "" {
		return fmt.Errorf("import scan: -hostname is required")
	}
	if *out == "" {
		return fmt.Errorf("import scan: -out is required")
	}
	if !filepath.IsAbs(*out) {
		return fmt.Errorf("import scan: -out must be an absolute path, got %q", *out)
	}
	if *memorixJSON != "" && !filepath.IsAbs(*memorixJSON) {
		return fmt.Errorf("import scan: -memorix-json must be an absolute path, got %q", *memorixJSON)
	}
	outPath := filepath.Clean(*out)
	for _, root := range roots {
		if pathInside(filepath.Clean(root), outPath) {
			return fmt.Errorf("import scan: -out %s is inside -root %s (refuses to overwrite inventoried files)", outPath, root)
		}
	}
	inv, err := importer.Scan(importer.Request{
		Roots:       roots,
		Hostname:    *hostname,
		MemorixJSON: *memorixJSON,
		Exclude:     exclude,
	})
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stderr, nil))
	for _, s := range inv.Skipped {
		log.Info("skipped source", "source", s.Source, "reason", s.Reason)
	}
	if err := writeJSON(*out, inv); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, *out)
	return err
}

func importPlan(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("import plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "absolute path to write plan.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("import plan: -out is required")
	}
	if !filepath.IsAbs(*out) {
		return fmt.Errorf("import plan: -out must be an absolute path, got %q", *out)
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("import plan: want one or more inventory.json files")
	}
	invs := make([]importer.Inventory, 0, fs.NArg())
	for _, p := range fs.Args() {
		inv, err := readInventory(p)
		if err != nil {
			return err
		}
		invs = append(invs, inv)
	}
	outPath := filepath.Clean(*out)
	for _, inv := range invs {
		for _, f := range inv.Files {
			if f.Path != "" && filepath.Clean(f.Path) == outPath {
				return fmt.Errorf("import plan: -out %s equals an inventoried path (refuses to overwrite inventoried files)", outPath)
			}
		}
	}
	plan, err := importer.BuildPlan(invs, nil)
	if err != nil {
		return err
	}
	if err := writeJSON(*out, plan); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, *out)
	return err
}

func importApply(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("import apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	machine := fs.String("machine", "", "hostname this apply is for")
	trusted := fs.String("trusted", "", "most-trusted hostname; only it may create active rows")
	server := fs.String("server", os.Getenv("SUBSTRATE_URL"), "control plane base URL")
	token := fs.String("token", os.Getenv("SUBSTRATE_TOKEN"), "bearer token")
	scope := fs.String("scope", "", "scope path for imported rows")
	dryRun := fs.Bool("dry-run", false, "classify and print; write nothing (default unless -commit)")
	commit := fs.Bool("commit", false, "perform database writes; without this, apply is a dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("import apply: want one plan.json")
	}
	if *machine == "" {
		return fmt.Errorf("import apply: -machine is required")
	}
	if *trusted == "" {
		return fmt.Errorf("import apply: -trusted is required")
	}
	if *server == "" {
		return fmt.Errorf("import apply: -server or SUBSTRATE_URL is required")
	}
	if *token == "" {
		return fmt.Errorf("import apply: -token or SUBSTRATE_TOKEN is required")
	}
	if *scope == "" {
		return fmt.Errorf("import apply: -scope is required")
	}
	if *dryRun && *commit {
		return fmt.Errorf("import apply: -dry-run and -commit cannot both be set")
	}
	doCommit := *commit && !*dryRun
	raw, err := os.ReadFile(fs.Arg(0)) //nolint:gosec // operator-supplied plan.json path
	if err != nil {
		return fmt.Errorf("import apply: read %s: %w", fs.Arg(0), err)
	}
	var plan importer.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return fmt.Errorf("import apply: parse %s: %w", fs.Arg(0), err)
	}
	body, err := json.Marshal(map[string]any{
		"machine":         *machine,
		"trusted_machine": *trusted,
		"scope":           *scope,
		"dry_run":         !doCommit,
		"commit":          doCommit,
		"plan":            plan,
	})
	if err != nil {
		return err
	}
	url := strings.TrimRight(*server, "/") + "/v1/import"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body)) //nolint:gosec // G704: -server is the operator's control plane
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+*token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req) //nolint:gosec // G704: -server is the operator's control plane
	if err != nil {
		return fmt.Errorf("import apply: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("import apply: read response: %w", err)
	}
	if res.StatusCode >= 400 {
		return fmt.Errorf("import apply: %s: %s", res.Status, respBody)
	}
	var parsed importer.ApplyResult
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		out := parsed.Format()
		if strings.TrimSpace(out) != "" {
			_, err = fmt.Fprint(stdout, out)
			return err
		}
	}
	_, err = fmt.Fprint(stdout, string(respBody))
	return err
}

func readInventory(path string) (importer.Inventory, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied inventory.json path
	if err != nil {
		return importer.Inventory{}, fmt.Errorf("import plan: read %s: %w", path, err)
	}
	var inv importer.Inventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		return importer.Inventory{}, fmt.Errorf("import plan: parse %s: %w", path, err)
	}
	return inv, nil
}

// pathInside reports whether child is root or a path under it. Rel-based so
// /home/u-other is not treated as inside /home/u.
func pathInside(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	cleanup = false
	return nil
}
