package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustWriteCLI seeds one harness file under a scan root.
func mustWriteCLI(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The inventory carries the bodies of every harness file on the machine, so
// it is written 0600 even when it lands on an existing world-readable file.
// Mutation that turns this red: replacing writeJSON's temp-file-plus-chmod
// with os.WriteFile, which keeps an existing file's mode.
func TestImportScanOutModeIsAlways0600(t *testing.T) {
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Shared\nAlways run gofmt.\n")
	out := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(out, []byte("{}\n"), 0o644); err != nil { //nolint:gosec // the fixture must start world-readable
		t.Fatalf("seed a 0644 inventory: %v", err)
	}

	if _, _, err := run(t, Deps{}, "import", "scan",
		"--root", root, "--hostname", "wsl", "--out", out); err != nil {
		t.Fatalf("import scan: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat inventory: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode %o, want 0600", perm)
	}
}

// --exclude drops a path from the inventory and keeps everything else, which
// is the behaviour README documents for keeping fixture trees out of an
// import.
// Mutation that turns this red: dropping Exclude from the importer.Request.
func TestImportScanExcludeFlagDropsMatches(t *testing.T) {
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Mine\nAlways run gofmt.\n")
	mustWriteCLI(t, filepath.Join(root, "fixtures", "eval", "AGENTS.md"), "# Fixture\n")
	out := filepath.Join(t.TempDir(), "inventory.json")

	if _, _, err := run(t, Deps{}, "import", "scan",
		"--root", root, "--hostname", "wsl", "--out", out,
		"--exclude", "fixtures/**"); err != nil {
		t.Fatalf("import scan: %v", err)
	}
	body, err := os.ReadFile(out) //nolint:gosec // out is a t.TempDir path built by this test
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	if strings.Contains(string(body), "fixtures/eval/AGENTS.md") {
		t.Fatal("--exclude did not drop the matching path from inventory.json")
	}
	if !strings.Contains(string(body), ".claude/CLAUDE.md") {
		t.Fatal("--exclude dropped a path it should have kept")
	}
}
