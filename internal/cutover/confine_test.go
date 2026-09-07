package cutover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCutoverCommitDoesNotTouchUnrelatedRoot(t *testing.T) {
	// WithMountRoot(req.Roots) in planCutover's classifyStore loop is the
	// one-line production change that makes this red. The sentinel lives in
	// a second TempDir that stands in for DefaultMountRoot; the test never
	// points at the real mount root.
	owned := t.TempDir()
	unrelated := t.TempDir()
	setMountRoot(t, unrelated)

	sentinel := filepath.Join(unrelated, memorixStore)
	body := "unrelated-store\n"
	mustWriteMode(t, sentinel, body, 0o640)
	wantSHA, wantMode := sentinelState(t, sentinel)

	live := filepath.Join(owned, ".claude", "CLAUDE.md")
	mustWrite(t, live, "# original\n")
	_, err := Cutover(Request{
		Roots:     []string{owned},
		Home:      owned,
		Commit:    true,
		Installer: &FakeInstaller{},
		Files:     []Replacement{{Path: live, Content: renderedClaude("2026-09-07T00:00:00Z")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(sentinel); err != nil {
		t.Fatalf("cutover displaced sentinel outside Roots: %v", err)
	}
	gotSHA, gotMode := sentinelState(t, sentinel)
	if gotSHA != wantSHA || gotMode != wantMode {
		t.Fatalf("cutover mutated sentinel outside Roots: sha %s→%s mode %o→%o", wantSHA, gotSHA, wantMode, gotMode)
	}
	got, err := os.ReadFile(sentinel) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("sentinel body %q, want %q", got, body)
	}
	assertNoBackupSuffix(t, unrelated)
}

func TestRestoreDoesNotTouchUnrelatedRoot(t *testing.T) {
	// Discover(WithMountRoot(req.Roots)) in plannedLivePaths is the one-line
	// production change that makes this red. Restore must invert the given
	// roots, not discover DefaultMountRoot on the side.
	owned := t.TempDir()
	unrelated := t.TempDir()
	setMountRoot(t, unrelated)

	repo := filepath.Join(unrelated, "acme", "api")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(repo, "AGENTS.md")
	original := "# stay put\n"
	rendered := renderedClaude("2026-09-07T00:00:00Z")
	mustWriteMode(t, live, rendered, 0o640)
	mustWriteMode(t, live+BackupSuffix, original, 0o640)
	wantSHA, wantMode := sentinelState(t, live)
	backupSHA, backupMode := sentinelState(t, live+BackupSuffix)

	ownedLive := filepath.Join(owned, ".claude", "CLAUDE.md")
	mustWrite(t, ownedLive, "rendered\n")
	mustWrite(t, ownedLive+BackupSuffix, "original\n")

	_, err := Restore(Request{Roots: []string{owned}, Home: owned, Commit: true, Installer: &FakeInstaller{}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(live); err != nil {
		t.Fatalf("restore displaced sentinel outside Roots: %v", err)
	}
	if _, err := os.Lstat(live + BackupSuffix); err != nil {
		t.Fatalf("restore consumed sentinel backup outside Roots: %v", err)
	}
	gotSHA, gotMode := sentinelState(t, live)
	if gotSHA != wantSHA || gotMode != wantMode {
		t.Fatalf("restore mutated sentinel outside Roots: sha %s→%s mode %o→%o", wantSHA, gotSHA, wantMode, gotMode)
	}
	gotBackupSHA, gotBackupMode := sentinelState(t, live+BackupSuffix)
	if gotBackupSHA != backupSHA || gotBackupMode != backupMode {
		t.Fatalf("restore consumed sentinel backup outside Roots: sha %s→%s mode %o→%o", backupSHA, gotBackupSHA, backupMode, gotBackupMode)
	}
	got, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != rendered {
		t.Fatalf("sentinel body %q, want still-rendered %q", got, rendered)
	}
}

func setMountRoot(t *testing.T, root string) {
	t.Helper()
	orig := DefaultMountRoot
	DefaultMountRoot = root
	t.Cleanup(func() { DefaultMountRoot = orig })
}

func sentinelState(t *testing.T, path string) (sha string, mode os.FileMode) {
	t.Helper()
	sum, mode, err := fileHash(path)
	if err != nil {
		t.Fatal(err)
	}
	return sum, mode
}

func assertNoBackupSuffix(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(d.Name(), BackupSuffix) {
			t.Errorf("wrote %s outside given roots", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
