package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/agentic-substrate/substrate/internal/importer"
)

func importCmd(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("want scan or plan subcommand")
	}
	switch args[0] {
	case "scan":
		return importScan(args[1:], stdout, stderr)
	case "plan":
		return importPlan(args[1:], stdout, stderr)
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
	inv, err := importer.Scan(importer.Request{Roots: roots, Hostname: *hostname})
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

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
