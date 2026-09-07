package cutover

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreRequiresRoot(t *testing.T) {
	// Defaulting empty Roots to os.Getenv("HOME") is the one-line change that makes this red.
	canary := t.TempDir()
	mustWrite(t, filepath.Join(canary, ".claude", "CLAUDE.md"), "# from HOME\n")
	t.Setenv("HOME", canary)
	_, err := Restore(Request{Commit: true, Installer: &FakeInstaller{}})
	if err == nil || !strings.Contains(err.Error(), "-root") {
		t.Fatalf("got %v, want -root required", err)
	}
}

func TestRestoreRejectsRelativeRoot(t *testing.T) {
	// Accepting a relative Roots entry is the one-line change that makes this red.
	t.Setenv("HOME", t.TempDir())
	_, err := Restore(Request{Roots: []string{"relative/home"}, Commit: true, Installer: &FakeInstaller{}})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("got %v, want absolute -root", err)
	}
}

func TestRestoreReturnsOriginalHash(t *testing.T) {
	// Restoring by writing empty/truncated bytes, or leaving the rendered
	// file in place, is the one-line change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	original := "# original\nalways gofmt\n"
	rendered := renderedClaude("2026-09-07T00:00:00Z")
	mustWriteMode(t, live, rendered, 0o640)
	mustWriteMode(t, live+BackupSuffix, original, 0o640)

	rep, err := Restore(Request{Roots: []string{root}, Commit: true, Installer: &FakeInstaller{}})
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil {
		t.Fatal("Restore returned nil report")
	}
	got, err := os.ReadFile(live) //nolint:gosec // live is under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(got) != sha256Hex([]byte(original)) {
		t.Fatalf("restore did not return original hash\n got %s\nwant %s\nbody %q", sha256Hex(got), sha256Hex([]byte(original)), got)
	}
	info, err := os.Stat(live)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode %o, want 0640", info.Mode().Perm())
	}
	if _, err := os.Lstat(live + BackupSuffix); !os.IsNotExist(err) {
		t.Fatalf("left %s behind: %v", live+BackupSuffix, err)
	}
}

func TestRestorePutsBackDirectoryStore(t *testing.T) {
	// os.RemoveAll on the backup directory is the one-line change that makes this red.
	root := t.TempDir()
	store := filepath.Join(root, ".memorix")
	backup := store + BackupSuffix
	mustWriteMode(t, filepath.Join(backup, "memories.json"), `{"memories":["keep-me"]}`+"\n", 0o600)

	if _, err := Restore(Request{Roots: []string{root}, Commit: true, Installer: &FakeInstaller{}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(store, "memories.json")) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "keep-me") {
		t.Fatalf("store body %q, want original memories", got)
	}
	if _, err := os.Lstat(backup); !os.IsNotExist(err) {
		t.Fatalf("left store backup behind: %v", err)
	}
}

func TestRestoreUninstallsUnit(t *testing.T) {
	// Skipping installer.Uninstall on commit is the one-line change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".codex", "AGENTS.md")
	mustWrite(t, live, "rendered\n")
	mustWrite(t, live+BackupSuffix, "original\n")
	fake := &FakeInstaller{}
	if _, err := Restore(Request{Roots: []string{root}, Commit: true, Installer: fake}); err != nil {
		t.Fatal(err)
	}
	if fake.Uninstalls != 1 {
		t.Fatalf("Uninstalls = %d, want 1", fake.Uninstalls)
	}
	if len(fake.Installs) != 0 {
		t.Fatalf("restore installed a unit: %+v", fake.Installs)
	}
}

func TestRestoreDryRunDoesNotWrite(t *testing.T) {
	// Applying renames when Commit is false, or returning empty Format, is
	// the one-line change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	original := "# original\n"
	mustWrite(t, live, "rendered\n")
	mustWrite(t, live+BackupSuffix, original)
	before := snapshotTree(t, root)
	if strings.TrimSpace(before) == "" {
		t.Fatal("fixture snapshot was empty")
	}
	fake := &FakeInstaller{}
	rep, err := Restore(Request{Roots: []string{root}, Commit: false, Installer: fake})
	if err != nil {
		t.Fatal(err)
	}
	out := rep.Format()
	if strings.TrimSpace(out) == "" {
		t.Fatal("dry-run printed nothing")
	}
	if !strings.Contains(out, live) {
		t.Fatalf("dry-run omitted target path:\n%s", out)
	}
	if !strings.Contains(out, sha256Hex([]byte("rendered\n"))) {
		t.Fatalf("dry-run omitted current sha256:\n%s", out)
	}
	if !strings.Contains(out, sha256Hex([]byte(original))) {
		t.Fatalf("dry-run omitted restored sha256:\n%s", out)
	}
	if snapshotTree(t, root) != before {
		t.Fatal("restore --dry-run mutated the fixture tree")
	}
	if fake.Uninstalls != 0 || len(fake.Installs) != 0 {
		t.Fatalf("dry-run touched the unit installer: installs=%d uninstalls=%d", len(fake.Installs), fake.Uninstalls)
	}
}

func TestRestoreDryRunSurfacesMissingBackupConflict(t *testing.T) {
	// Swallowing a rename that would fail because the live path is a
	// directory and the backup is a file (or Stat errors) is the change
	// that makes this red. Dry-run must classify the same decisions.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(live, 0o750); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, live+BackupSuffix, "original\n")
	_, err := Restore(Request{Roots: []string{root}, Commit: false, Installer: &FakeInstaller{}})
	if err == nil {
		t.Fatal("dry-run succeeded on a restore that cannot rename a file over a directory")
	}
}

func TestRestoreDoesNotDeleteWithoutRename(t *testing.T) {
	// os.Remove(backup) after copying, or truncating the backup, is the
	// change that makes this red. Restore must rename the backup onto the
	// live path so the original bytes survive as the live file.
	root := t.TempDir()
	live := filepath.Join(root, ".cursor", "rules", "substrate.mdc")
	original := "alwaysApply: true\n# original cursor rule\n"
	mustWrite(t, live, "rendered cursor\n")
	mustWrite(t, live+BackupSuffix, original)

	if _, err := Restore(Request{Roots: []string{root}, Commit: true, Installer: &FakeInstaller{}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("body %q, want original (deleted or truncated rather than renamed back)", got)
	}
	if _, err := os.Lstat(live + BackupSuffix); !os.IsNotExist(err) {
		t.Fatal("backup still present; rename did not consume *.pre-substrate")
	}
}

func TestSystemdUnitPinsMountRoot(t *testing.T) {
	// Emitting -roots $HOME or omitting /work is the one-line change that makes this red.
	unit := SystemdUnit(UnitSpec{
		Binary:  "/usr/local/bin/substrate-adapter",
		Home:    "/tmp/fake-home",
		Server:  "https://cp.example",
		Token:   "secret",
		Machine: "wsl",
		Roots:   []string{DefaultMountRoot},
	})
	if !strings.Contains(unit, "-roots /work") {
		t.Fatalf("unit missing CONT-4 mount root:\n%s", unit)
	}
	if strings.Contains(unit, "-roots $HOME") || strings.Contains(unit, "-home $HOME") {
		t.Fatalf("unit guesses $HOME:\n%s", unit)
	}
	if !strings.Contains(unit, "-home /tmp/fake-home") {
		t.Fatalf("unit missing explicit -home:\n%s", unit)
	}
}

func TestLaunchdPlistPinsMountRoot(t *testing.T) {
	// Omitting /work from the ProgramArguments array is the change that makes this red.
	plist := LaunchdPlist(UnitSpec{
		Binary:  "/usr/local/bin/substrate-adapter",
		Home:    "/tmp/fake-home",
		Server:  "https://cp.example",
		Token:   "secret",
		Machine: "mac",
		Roots:   []string{DefaultMountRoot},
	})
	if !strings.Contains(plist, "/work") {
		t.Fatalf("plist missing CONT-4 mount root:\n%s", plist)
	}
}

func renderedClaude(generatedAt string) string {
	return "# generated\ndo not edit\n\n<!-- sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa generated-at:" + generatedAt + " -->\n"
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	mustWriteMode(t, path, body, 0o644)
}

func mustWriteMode(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.WriteString(filepath.ToSlash(rel))
		b.WriteByte(' ')
		b.WriteString(info.Mode().String())
		if !d.IsDir() {
			body, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir
			if err != nil {
				return err
			}
			sum := sha256.Sum256(body)
			b.WriteByte(' ')
			b.WriteString(hex.EncodeToString(sum[:]))
		}
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
