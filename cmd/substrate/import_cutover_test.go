package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/cutover"
	"github.com/agentic-substrate/substrate/internal/render"
)

func TestImportCutoverRequiresRoot(t *testing.T) {
	// Defaulting empty -root to os.Getenv("HOME") is the one-line change that makes this red.
	canary := t.TempDir()
	t.Setenv("HOME", canary)
	err := importCutover([]string{
		"-server", "http://127.0.0.1:1", "-token", "t", "-machine", "wsl",
	}, &bytes.Buffer{}, &bytes.Buffer{}, &cutover.FakeInstaller{})
	if err == nil || !strings.Contains(err.Error(), "-root") {
		t.Fatalf("got %v, want -root required", err)
	}
}

func TestImportCutoverDryRunDoesNotWriteOrInstall(t *testing.T) {
	// Writing the fixture or calling Installer.Install without -commit is the
	// change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	original := "# original\nalways gofmt\n"
	mustWriteCLI(t, live, original)
	before := snapshotDir(t, root)
	rendered := "# generated\ndo not edit\n\n<!-- sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa generated-at:2026-09-07T00:00:00Z -->\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"targets": []map[string]string{{
				"path":    "~/.claude/CLAUDE.md",
				"content": rendered,
				"sha256":  render.DriftHash(rendered),
			}},
		})
	}))
	t.Cleanup(srv.Close)

	fake := &cutover.FakeInstaller{}
	stdout := &bytes.Buffer{}
	err := importCutover([]string{
		"-root", root, "-server", srv.URL, "-token", "t", "-machine", "wsl",
	}, stdout, &bytes.Buffer{}, fake)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("dry-run printed nothing")
	}
	if !strings.Contains(stdout.String(), live) {
		t.Fatalf("dry-run omitted target:\n%s", stdout)
	}
	if snapshotDir(t, root) != before {
		t.Fatal("import cutover --dry-run mutated the fixture tree")
	}
	if len(fake.Installs) != 0 {
		t.Fatalf("unit was installed while --dry-run was set: %+v", fake.Installs)
	}
}

func TestImportCutoverCommitThenRestoreRoundTrip(t *testing.T) {
	// Skipping restore, or hashing the footer, is the change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	original := "# original\nalways gofmt\n"
	mustWriteCLI(t, live, original)
	mustWriteCLI(t, filepath.Join(root, ".memorix", "memories.json"), `{"keep":true}`+"\n")
	before := snapshotDir(t, root)
	if strings.TrimSpace(before) == "" {
		t.Fatal("pre-cutover snapshot was empty")
	}
	rendered := "# generated\ndo not edit\n\n<!-- sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa generated-at:2026-09-07T00:00:00Z -->\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"targets": []map[string]string{{
				"path":    "~/.claude/CLAUDE.md",
				"content": rendered,
				"sha256":  render.DriftHash(rendered),
			}},
		})
	}))
	t.Cleanup(srv.Close)

	fake := &cutover.FakeInstaller{}
	if err := importCutover([]string{
		"-root", root, "-server", srv.URL, "-token", "t", "-machine", "wsl", "-commit",
	}, &bytes.Buffer{}, &bytes.Buffer{}, fake); err != nil {
		t.Fatal(err)
	}
	if len(fake.Installs) != 1 {
		t.Fatalf("Installs = %d, want 1", len(fake.Installs))
	}
	if fake.Installs[0].Roots[0] != cutover.DefaultMountRoot {
		t.Fatalf("unit roots %v, want [%s]", fake.Installs[0].Roots, cutover.DefaultMountRoot)
	}
	got, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != rendered {
		t.Fatalf("cutover live body %q", got)
	}

	if err := adapterCmd([]string{"uninstall", "-restore", "-root", root, "-commit"}, &bytes.Buffer{}, &bytes.Buffer{}, fake); err != nil {
		t.Fatal(err)
	}
	after := snapshotDir(t, root)
	if after != before {
		t.Fatalf("CLI round trip mutated the tree\n before:\n%s\n after:\n%s", before, after)
	}
	sum := sha256.Sum256([]byte(original))
	restored, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(sum[:]) != sha256HexCLI(restored) {
		t.Fatal("restore did not return original hash")
	}
}

func TestImportCutoverDisplacesMountRootAgents(t *testing.T) {
	// Discovering only the first -root, or omitting /work from Request.Roots
	// so confineToRoots rejects checkout dests, is the change that makes this
	// red. A fake /work/<project>/<repo>/AGENTS.md must be renamed, not left
	// for the adapter to clobber.
	home := t.TempDir()
	work := t.TempDir()
	repo := filepath.Join(work, "acme", "api")
	initGitRepo(t, repo)
	live := filepath.Join(repo, "AGENTS.md")
	original := "# repo agents\n"
	mustWriteCLI(t, live, original)
	rendered := "# generated\ndo not edit\n\n<!-- sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa generated-at:2026-09-07T00:00:00Z -->\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"targets": []map[string]string{{
				"path":    "AGENTS.md",
				"content": rendered,
				"sha256":  render.DriftHash(rendered),
			}},
		})
	}))
	t.Cleanup(srv.Close)

	fake := &cutover.FakeInstaller{}
	if err := importCutover([]string{
		"-root", home, "-root", work, "-server", srv.URL, "-token", "t", "-machine", "wsl", "-commit",
	}, &bytes.Buffer{}, &bytes.Buffer{}, fake); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(live + cutover.BackupSuffix) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatalf("mount-root AGENTS.md was not renamed to *.pre-substrate: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("backup %q, want original", backup)
	}
	got, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != rendered {
		t.Fatalf("live body %q, want rendered (adapter would have clobbered the original)", got)
	}
}

func TestScanRootsIncludesDefaultMountRoot(t *testing.T) {
	// Returning operator -root without appending DefaultMountRoot is the
	// one-line change that makes this red. confineToRoots must permit /work
	// destinations and Discover must see CONT-4 checkouts.
	got := scanRoots([]string{"/tmp/fake-home"})
	if len(got) != 2 || got[0] != "/tmp/fake-home" || got[1] != cutover.DefaultMountRoot {
		t.Fatalf("scanRoots = %v, want [home %s]", got, cutover.DefaultMountRoot)
	}
	dup := scanRoots([]string{"/tmp/fake-home", cutover.DefaultMountRoot})
	if len(dup) != 2 {
		t.Fatalf("scanRoots duplicated mount root: %v", dup)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}
