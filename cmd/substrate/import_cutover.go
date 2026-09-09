package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentic-substrate/substrate/internal/adapter"
	"github.com/agentic-substrate/substrate/internal/cli"
	"github.com/agentic-substrate/substrate/internal/cutover"
)

func importCutover(args []string, stdout, stderr io.Writer, inst cutover.UnitInstaller) error {
	_ = stderr
	fs := flag.NewFlagSet("import cutover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var roots []string
	fs.Func("root", "absolute tree whose harness files are displaced (repeatable; defaults to /work; no $HOME default)", func(s string) error {
		roots = append(roots, s)
		return nil
	})
	server := fs.String("server", os.Getenv("SUBSTRATE_URL"), "control plane base URL")
	token := fs.String("token", os.Getenv("SUBSTRATE_TOKEN"), "bearer token")
	machine := fs.String("machine", "", "machine name sent as GET /v1/render?machine=")
	binary := fs.String("binary", "substrate-adapter", "adapter binary written into the unit")
	dryRun := fs.Bool("dry-run", false, "classify and print; write nothing (default unless -commit)")
	commit := fs.Bool("commit", false, "perform cutover; without this, cutover is a dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	roots = scanRoots(roots)
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return fmt.Errorf("import cutover: -root must be an absolute path, got %q (refuses to guess $HOME)", root)
		}
	}
	if *server == "" {
		return fmt.Errorf("import cutover: -server or SUBSTRATE_URL is required")
	}
	if *token == "" {
		return fmt.Errorf("import cutover: -token or SUBSTRATE_TOKEN is required")
	}
	if *machine == "" {
		return fmt.Errorf("import cutover: -machine is required")
	}
	if *dryRun && *commit {
		return fmt.Errorf("import cutover: -dry-run and -commit cannot both be set")
	}
	if inst == nil {
		inst = cutover.NewOSInstaller()
	}

	targets, err := fetchRenderTargets(context.Background(), *server, *token, *machine)
	if err != nil {
		return err
	}
	home := roots[0]
	checkouts, err := adapter.Discover(roots)
	if err != nil {
		return err
	}
	var checkoutPaths []string
	for _, c := range checkouts {
		checkoutPaths = append(checkoutPaths, c.Path)
	}
	var files []cutover.Replacement
	for _, tgt := range targets {
		dests, err := cutover.Destinations(home, checkoutPaths, tgt.Path)
		if err != nil {
			return err
		}
		for _, dest := range dests {
			files = append(files, cutover.Replacement{Path: dest, Content: tgt.Content})
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("import cutover: GET /v1/render returned no paths under -root")
	}

	rep, err := cutover.Cutover(cutover.Request{
		Roots:     roots,
		Home:      home,
		Commit:    *commit && !*dryRun,
		Installer: inst,
		Files:     files,
		Server:    *server,
		Token:     *token,
		Machine:   *machine,
		Binary:    *binary,
	})
	if err != nil {
		return err
	}
	out := rep.Format()
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("import cutover: produced no plan")
	}
	_, err = fmt.Fprint(stdout, out)
	return err
}

type cliRenderTarget struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

func fetchRenderTargets(ctx context.Context, server, token, machine string) ([]cliRenderTarget, error) {
	u, err := url.Parse(strings.TrimRight(server, "/") + "/v1/render")
	if err != nil {
		return nil, fmt.Errorf("import cutover: render url: %w", err)
	}
	q := u.Query()
	if machine != "" {
		q.Set("machine", machine)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil) //nolint:gosec // G704: -server is the operator's control plane
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := cli.NewHTTPClient()
	res, err := client.Do(req) //nolint:gosec // G704: -server is the operator's control plane
	if err != nil {
		return nil, fmt.Errorf("import cutover: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("import cutover: read response: %w", err)
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("import cutover: %s: %s", res.Status, body)
	}
	var parsed struct {
		Targets []cliRenderTarget `json:"targets"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("import cutover: decode render: %w", err)
	}
	return parsed.Targets, nil
}

// scanRoots is the Discover and confine set. An explicit -root list is used
// as given. When the operator passes none, DefaultMountRoot is the default
// value — not an extra root appended onto whatever they did pass.
func scanRoots(roots []string) []string {
	if len(roots) == 0 {
		return []string{cutover.DefaultMountRoot}
	}
	return append([]string(nil), roots...)
}
