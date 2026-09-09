package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/cutover"
)

// An omitted --root must default exactly as install does -- to the mount root
// -- or the rollback walks a different tree than the install harnessed and
// every *.pre-substrate is orphaned. This is the hazard absRoots documents.
// Mutation that turns this red: defaulting absRoots to os.Getenv("HOME")
// instead of cutover.DefaultMountRoot.
func TestAdapterUninstallOmittingRootUsesMountRootNotHome(t *testing.T) {
	canary := t.TempDir()
	if err := os.WriteFile(filepath.Join(canary, "CLAUDE.md"), []byte("from HOME\n"), 0o600); err != nil {
		t.Fatalf("seed canary: %v", err)
	}
	t.Setenv("HOME", canary)
	mount := t.TempDir()
	orig := cutover.DefaultMountRoot
	cutover.DefaultMountRoot = mount
	t.Cleanup(func() { cutover.DefaultMountRoot = orig })

	out, _, err := run(t, Deps{Installer: &cutover.FakeInstaller{}}, "adapter", "uninstall", "--yes")
	if err != nil {
		t.Fatalf("uninstall with no --root: %v", err)
	}
	if !strings.Contains(out, mount) {
		t.Fatalf("the echoed roots must name the mount root %s, got:\n%s", mount, out)
	}
	got, err := os.ReadFile(filepath.Join(canary, "CLAUDE.md")) //nolint:gosec // path is under t.TempDir
	if err != nil || string(got) != "from HOME\n" {
		t.Fatalf("$HOME canary body %q err %v, want untouched", got, err)
	}
}
