package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/cutover"
)

// import apply off a TTY without --yes must refuse before the POST. review
// approve had the only refusal test; a writing verb that silently skipped
// confirm() would have passed CI.
// Mutation that turns this red: dropping the confirm() call from import apply.
func TestImportApplyOffTTYWithoutYesWritesNothing(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	_, _, err := run(t, Deps{HTTP: srv.Client()}, "import", "apply", plan,
		"--machine", "m", "--trusted", "m", "--scope", "org:acme",
		"--server", srv.URL, "--token", "t")
	if err == nil {
		t.Fatal("want an error when stdin is not a terminal and --yes is absent")
	}
	if posts != 0 {
		t.Fatalf("want zero writes, got %d POSTs", posts)
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("error must name the fix, got %q", err)
	}
}

// adapter install off a TTY without --yes must refuse before it displaces
// anything and before it installs a unit.
// Mutation that turns this red: dropping the confirm() call from runCutover.
func TestAdapterInstallOffTTYWithoutYesWritesNothing(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	mustWriteCLI(t, live, "live\n")
	srv := renderServer(t)
	fake := &cutover.FakeInstaller{}

	out, _, err := run(t, Deps{HTTP: srv.Client(), Installer: fake},
		"adapter", "install", "--root", root, "--machine", "m",
		"--server", srv.URL, "--token", "t")
	if err == nil {
		t.Fatal("want an error when stdin is not a terminal and --yes is absent")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("error must name the fix, got %q", err)
	}
	if len(fake.Installs) != 0 || fake.Uninstalls != 0 {
		t.Fatalf("a refused install must record zero installer calls, got %+v", fake)
	}
	body, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil || string(body) != "live\n" {
		t.Fatalf("a refused install must not touch the live file, got %q %v", body, err)
	}
	if _, err := os.Stat(live + ".pre-substrate"); !os.IsNotExist(err) {
		t.Fatalf("a refused install must not displace the live file (stat err %v)", err)
	}
	// The refusal is only reviewable if the operator saw what was at stake.
	if !strings.Contains(out, root) || !strings.Contains(out, live) {
		t.Fatalf("install must echo the roots and the files before prompting, got:\n%s", out)
	}
}

// adapter uninstall off a TTY without --yes must refuse before restoring
// anything: --root defaults to the mount root silently, so an unconfirmed
// uninstall in the wrong shell restores over a live tree.
// Mutation that turns this red: dropping the confirm() call from uninstall.
func TestAdapterUninstallOffTTYWithoutYesWritesNothing(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	mustWriteCLI(t, live, "original\n")
	srv := renderServer(t)
	if _, _, err := run(t, Deps{HTTP: srv.Client(), Installer: &cutover.FakeInstaller{}},
		"adapter", "install", "--root", root, "--machine", "m",
		"--server", srv.URL, "--token", "t", "--yes"); err != nil {
		t.Fatalf("adapter install: %v", err)
	}

	fake := &cutover.FakeInstaller{}
	out, _, err := run(t, Deps{Installer: fake}, "adapter", "uninstall", "--root", root)
	if err == nil {
		t.Fatal("want an error when stdin is not a terminal and --yes is absent")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("error must name the fix, got %q", err)
	}
	if fake.Uninstalls != 0 {
		t.Fatalf("a refused uninstall must record zero installer calls, got %+v", fake)
	}
	if body, err := os.ReadFile(live); err != nil || string(body) != "rendered\n" { //nolint:gosec // under t.TempDir
		t.Fatalf("a refused uninstall must leave the rendered file in place, got %q %v", body, err)
	}
	// The operator must see the roots and the paths before the prompt, or
	// "type 'yes' to continue:" names nothing they can check.
	if !strings.Contains(out, root) || !strings.Contains(out, live) {
		t.Fatalf("uninstall must echo the roots and the files before prompting, got:\n%s", out)
	}
}

// A world-readable config.json is refused by loadConfig, and the refusal must
// reach the operator: told "no bearer token, run auth login" they would never
// learn that a never-expiring credential is readable by every other user.
// Mutation that turns this red: restoring `if cfg, _, err := loadConfig(d);
// err == nil` in newAPIClient, which discards the refusal.
func TestWorldReadableConfigReportsTheModeNotAMissingToken(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, ConfigFileName)
	raw, err := json.Marshal(Config{Server: "https://substrate.example", Token: "secret"})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(cfg, raw, 0o644); err != nil { //nolint:gosec // the fixture must be world-readable
		t.Fatalf("seed a 0644 config: %v", err)
	}

	_, _, err = run(t, Deps{ConfigDir: dir}, "review", "list")
	if err == nil {
		t.Fatal("want a refusal for a world-readable config.json")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("the operator must be told to chmod 600, got %q", err)
	}
	if strings.Contains(err.Error(), "no bearer token") {
		t.Fatalf("a leaked credential must not be reported as a missing one, got %q", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("the refusal must not echo the token, got %q", err)
	}
}

// A missing config file is still not an error: commands that work without one
// must keep working, so only the permission refusal propagates.
// Mutation that turns this red: propagating ErrNoConfig out of newAPIClient.
func TestMissingConfigStillReportsTheMissingServer(t *testing.T) {
	_, _, err := run(t, Deps{ConfigDir: t.TempDir()}, "review", "list")
	if err == nil || !strings.Contains(err.Error(), "no control plane URL") {
		t.Fatalf("got %v, want the no-server error", err)
	}
}

// --team with no --org renders the wire string `team:eng`, which names no
// chain. That is a local mistake and must be answered locally rather than as
// a 4xx round trip that reads like a server fault.
// Mutation that turns this red: dropping requireWholeChain from resolveTarget.
func TestPartialScopeChainIsRefusedLocally(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	_, _, err := run(t, Deps{HTTP: srv.Client()}, "import", "apply", plan,
		"--machine", "m", "--trusted", "m", "--team", "eng",
		"--server", srv.URL, "--token", "t", "--yes")
	if err == nil {
		t.Fatal("want an error for a headless chain")
	}
	if requests != 0 {
		t.Fatalf("a headless chain must not reach the server, got %d requests", requests)
	}
	if !strings.Contains(err.Error(), "--org") {
		t.Fatalf("the error must name the missing level, got %q", err)
	}
}
