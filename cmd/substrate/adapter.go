package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/agentic-substrate/substrate/internal/cutover"
)

func adapterCmd(args []string, stdout, stderr io.Writer, inst cutover.UnitInstaller) error {
	_ = stderr
	if len(args) == 0 {
		return fmt.Errorf("want uninstall subcommand")
	}
	switch args[0] {
	case "uninstall":
		return adapterUninstall(args[1:], stdout, inst)
	default:
		return fmt.Errorf("unknown adapter command %q", args[0])
	}
}

func adapterUninstall(args []string, stdout io.Writer, inst cutover.UnitInstaller) error {
	fs := flag.NewFlagSet("adapter uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	restore := fs.Bool("restore", false, "rename *.pre-substrate files back over the live paths they displaced")
	var roots []string
	fs.Func("root", "absolute tree to restore (repeatable; required; no $HOME default)", func(s string) error {
		roots = append(roots, s)
		return nil
	})
	dryRun := fs.Bool("dry-run", false, "classify and print; write nothing (default unless -commit)")
	commit := fs.Bool("commit", false, "perform restores; without this, uninstall is a dry-run")
	force := fs.Bool("force", false, "discard live edits that no longer match the DriftHash cutover wrote")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*restore {
		return fmt.Errorf("adapter uninstall: -restore is required (refuses to drop files without putting them back)")
	}
	if len(roots) == 0 {
		return fmt.Errorf("adapter uninstall: -root is required (refuses to guess $HOME)")
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return fmt.Errorf("adapter uninstall: -root must be an absolute path, got %q (refuses to guess $HOME)", root)
		}
	}
	if *dryRun && *commit {
		return fmt.Errorf("adapter uninstall: -dry-run and -commit cannot both be set")
	}
	if inst == nil {
		inst = cutover.NewOSInstaller()
	}
	rep, err := cutover.Restore(cutover.Request{
		Roots:     cutover.WithMountRoot(roots),
		Home:      roots[0],
		Commit:    *commit && !*dryRun,
		Force:     *force,
		Installer: inst,
	})
	if err != nil {
		return err
	}
	out := rep.Format()
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("adapter uninstall: produced no plan")
	}
	_, err = fmt.Fprint(stdout, out)
	return err
}
