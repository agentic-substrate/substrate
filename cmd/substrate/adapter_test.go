package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/cutover"
)

func TestAdapterUninstallRequiresRestore(t *testing.T) {
	// Dropping the -restore check is the one-line change that makes this red.
	err := adapterCmd([]string{"uninstall", "-root", t.TempDir()}, &bytes.Buffer{}, &bytes.Buffer{}, &cutover.FakeInstaller{})
	if err == nil || !strings.Contains(err.Error(), "-restore") {
		t.Fatalf("got %v, want -restore required", err)
	}
}

func TestAdapterUninstallOmittingRootUsesMountRootNotHome(t *testing.T) {
	// An omitted -root must default exactly as `import cutover` does, or the
	// rollback walks a different tree than the cutover harnessed and every
	// .pre-substrate is orphaned. Defaulting to os.Getenv("HOME") instead of
	// DefaultMountRoot is the one-line change that makes this red.
	canary := t.TempDir()
	if err := os.WriteFile(filepath.Join(canary, "CLAUDE.md"), []byte("from HOME\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", canary)
	mount := t.TempDir()
	orig := cutover.DefaultMountRoot
	cutover.DefaultMountRoot = mount
	t.Cleanup(func() { cutover.DefaultMountRoot = orig })

	if err := adapterCmd([]string{"uninstall", "-restore"}, &bytes.Buffer{}, &bytes.Buffer{}, &cutover.FakeInstaller{}); err != nil {
		t.Fatalf("uninstall with no -root: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(canary, "CLAUDE.md")); err != nil || string(got) != "from HOME\n" {
		t.Fatalf("$HOME canary body %q err %v, want untouched", got, err)
	}
}

func TestAdapterUninstallRestoreReturnsOriginalHash(t *testing.T) {
	// Leaving the rendered file in place is the one-line change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	original := "# original\nalways gofmt\n"
	if err := os.MkdirAll(filepath.Dir(live), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("rendered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live+".pre-substrate", []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &cutover.FakeInstaller{}
	stdout := &bytes.Buffer{}
	err := adapterCmd([]string{"uninstall", "-restore", "-root", root, "-commit"}, stdout, &bytes.Buffer{}, fake)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if sha256HexCLI(got) != sha256HexCLI([]byte(original)) {
		t.Fatalf("restore did not return original hash\n got %s\nwant %s", sha256HexCLI(got), sha256HexCLI([]byte(original)))
	}
	if fake.Uninstalls != 1 {
		t.Fatalf("Uninstalls = %d, want 1", fake.Uninstalls)
	}
}

func TestAdapterUninstallDryRunDoesNotUninstall(t *testing.T) {
	// Calling Installer.Install or Uninstall when -commit is absent is the
	// change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(live), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("rendered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live+".pre-substrate", []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	fake := &cutover.FakeInstaller{}
	stdout := &bytes.Buffer{}
	err = adapterCmd([]string{"uninstall", "-restore", "-root", root}, stdout, &bytes.Buffer{}, fake)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("dry-run printed nothing")
	}
	got, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Fatal("dry-run mutated the live file")
	}
	if fake.Uninstalls != 0 || len(fake.Installs) != 0 {
		t.Fatalf("dry-run touched the unit: installs=%d uninstalls=%d", len(fake.Installs), fake.Uninstalls)
	}
}

func sha256HexCLI(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
