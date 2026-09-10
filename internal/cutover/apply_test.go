package cutover

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCutoverPartialWriteLeavesRestoreableTree(t *testing.T) {
	// Ignoring Request.WriteFile, or Uninstall returning a "unit not loaded"
	// error after restore has already renamed files back, is the change that
	// makes this red. A disk-full / Ctrl-C / chmod at write N of M must leave
	// *.pre-substrate backups and restore must put the tree back even though
	// the unit was never installed.
	root := t.TempDir()
	originals := map[string]string{
		filepath.Join(".claude", "CLAUDE.md"):  "# original claude\n",
		filepath.Join(".codex", "AGENTS.md"):   "# original agents\n",
		filepath.Join(".cursor", "rules", "a"): "# original cursor\n",
		filepath.Join(".cursor", "rules", "b"): "# original extra\n",
	}
	var files []Replacement
	for rel, body := range originals {
		p := filepath.Join(root, rel)
		mustWriteMode(t, p, body, 0o640)
		files = append(files, Replacement{Path: p, Content: renderedClaude("2026-09-07T00:00:00Z")})
	}
	before := snapshotTree(t, root)
	if strings.TrimSpace(before) == "" {
		t.Fatal("fixture snapshot was empty")
	}

	var writes int
	failing := func(path string, body []byte, mode os.FileMode) error {
		writes++
		if writes == 3 {
			return fmt.Errorf("injected: disk full at write %d of %d", writes, len(files))
		}
		return atomicWrite(path, body, mode)
	}
	inst := OSInstaller{
		GOOS: "linux",
		Exec: func(_ string, args ...string) error {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "disable") || strings.Contains(joined, "unload") {
				return errors.New("failed to disable unit: unit substrate-adapter.service not loaded")
			}
			return nil
		},
	}

	_, err := Cutover(Request{
		Roots:     []string{root},
		Home:      root,
		Commit:    true,
		Installer: inst,
		Files:     files,
		WriteFile: failing,
	})
	if err == nil {
		t.Fatal("cutover succeeded; injected writer never failed")
	}
	if !strings.Contains(err.Error(), "injected: disk full") {
		t.Fatalf("cutover error %v, want injected writer failure", err)
	}
	if strings.Contains(err.Error(), "--restore") {
		t.Fatalf("cutover error %v still recommends obsolete flag --restore", err)
	}
	if !strings.Contains(err.Error(), "substrate adapter uninstall") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("cutover error %v must recommend 'substrate adapter uninstall' and '--force'", err)
	}

	var leftover int
	for _, f := range files {
		if _, statErr := os.Lstat(f.Path + BackupSuffix); statErr == nil {
			leftover++
		}
	}
	if leftover == 0 {
		t.Fatal("partial apply left no *.pre-substrate backups; originals are gone")
	}

	if _, err := Restore(Request{Roots: []string{root}, Home: root, Commit: true, Installer: inst}); err != nil {
		t.Fatalf("restore after partial cutover: %v", err)
	}
	after := snapshotTree(t, root)
	if after != before {
		t.Fatalf("restore did not return the tree\n before:\n%s\n after:\n%s", before, after)
	}
}

func TestApplyRenamesRefusesExistingBackup(t *testing.T) {
	// os.Rename over an existing *.pre-substrate without re-checking is the
	// one-line change that makes this red. Linux rename replaces the dest.
	root := t.TempDir()
	from := filepath.Join(root, ".claude", "CLAUDE.md")
	to := from + BackupSuffix
	mustWrite(t, from, "live\n")
	mustWrite(t, to, "existing-backup\n")
	err := applyRenames([]Rename{{From: from, To: to, SHA: sha256Hex([]byte("live\n"))}})
	if err == nil || !strings.Contains(err.Error(), BackupSuffix) {
		t.Fatalf("got %v, want refuse existing backup", err)
	}
	got, err := os.ReadFile(to) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing-backup\n" {
		t.Fatalf("overwrote existing backup: %q", got)
	}
	live, err := os.ReadFile(from) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(live) != "live\n" {
		t.Fatal("live file was moved after refusing the backup")
	}
}

func TestApplyRenamesRollsBackCompletedOnFailure(t *testing.T) {
	// Leaving the first rename in place when a later one fails is the change
	// that makes this red. Partial apply must not strand originals at
	// *.pre-substrate while later sources are still live.
	root := t.TempDir()
	a := filepath.Join(root, ".claude", "CLAUDE.md")
	b := filepath.Join(root, ".codex", "AGENTS.md")
	mustWrite(t, a, "keep-a\n")
	err := applyRenames([]Rename{
		{From: a, To: a + BackupSuffix},
		{From: b, To: b + BackupSuffix},
	})
	if err == nil {
		t.Fatal("second rename should have failed (source missing)")
	}
	got, err := os.ReadFile(a) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatalf("first rename was not rolled back: %v", err)
	}
	if string(got) != "keep-a\n" {
		t.Fatalf("rolled-back body %q", got)
	}
	if _, err := os.Lstat(a + BackupSuffix); !os.IsNotExist(err) {
		t.Fatal("left *.pre-substrate after rolling back a failed batch")
	}
}

func TestOSInstallerUninstallTreatsUnitNotLoadedAsSuccess(t *testing.T) {
	// Returning the Exec error when systemctl says the unit is not loaded is
	// the one-line change that makes this red. Restore must succeed after a
	// partial cutover that never installed the unit.
	home := t.TempDir()
	inst := OSInstaller{
		GOOS: "linux",
		Exec: func(string, ...string) error {
			return errors.New("failed to disable unit: unit substrate-adapter.service not loaded")
		},
	}
	if err := inst.Uninstall(UnitSpec{Home: home, Roots: []string{DefaultMountRoot}}); err != nil {
		t.Fatalf("Uninstall of a unit that was never installed: %v", err)
	}
}

func TestAtomicWriteFsyncsFileAndParentDir(t *testing.T) {
	// Omitting f.Sync and the parent-dir fsync (adapter/write.go:35-53) is
	// the change that makes this red.
	origFile := syncFile
	origDir := syncParentDir
	t.Cleanup(func() {
		syncFile = origFile
		syncParentDir = origDir
	})
	var fileSynced bool
	var gotDir string
	syncFile = func(f *os.File) error {
		fileSynced = true
		return origFile(f)
	}
	syncParentDir = func(dir string) error {
		gotDir = dir
		return origDir(dir)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "CLAUDE.md")
	if err := atomicWrite(dest, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !fileSynced {
		t.Fatal("atomicWrite did not fsync the temp file")
	}
	if gotDir != dir {
		t.Fatalf("parent dir fsync path = %q, want dest dir %q", gotDir, dir)
	}
}
