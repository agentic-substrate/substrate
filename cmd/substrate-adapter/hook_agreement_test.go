package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/cutover"
)

// splitHookCommand splits the command string the way the harness does: with a
// real shell. A hand-rolled splitter here would encode the same quoting
// assumption the production shellQuote does, so a bug in one would be hidden
// by the matching bug in the other -- and a naive one is measurably wrong on
// the `'\”` escape shellQuote emits for an apostrophe.
func splitHookCommand(t *testing.T, cmd string) []string {
	t.Helper()
	//nolint:gosec // cmd is the command this test just had the installer write
	out, err := exec.Command("sh", "-c", "printf '%s\\0' "+cmd).Output()
	if err != nil {
		t.Fatalf("splitting %q with sh: %v", cmd, err)
	}
	fields := strings.Split(string(out), "\x00")
	return fields[:len(fields)-1] // trailing empty after the final NUL
}

// installedHookArgv returns the argv the installer actually wrote, minus the
// binary. It reads settings.json rather than reconstructing the command, so the
// test sees exactly what the harness would exec.
func installedHookArgv(t *testing.T, home string) []string {
	t.Helper()
	raw, err := os.ReadFile(cutover.ClaudeSettingsPath(home)) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var cmd string
	for _, ev := range cutover.HookEvents() {
		for _, g := range doc.Hooks[ev] {
			for _, h := range g.Hooks {
				if cmd == "" {
					cmd = h.Command
				}
			}
		}
	}
	if cmd == "" {
		t.Fatalf("the installer wrote no hook entry for %v: %s", cutover.HookEvents(), raw)
	}
	argv := splitHookCommand(t, cmd)[1:] // drop the binary
	// main.go dispatches on argv[0] == "hook" and hands the rest to hookCmd.
	// Assert that word here so the installer cannot drift from the dispatcher.
	if len(argv) == 0 || argv[0] != "hook" {
		t.Fatalf("installed argv %q does not start with the subcommand main dispatches on", argv)
	}
	return argv[1:]
}

// The installer and the shim must agree on argv. Every other test in this
// package hands hookCmd a correct `-home` that the installer does not
// necessarily produce, and the installer's own tests only grep the command
// string for a substring — so each side stubs the other and neither sees a
// mismatch. This drives the shim with the exact argv the installer wrote and
// asserts an observation actually lands in the outbox.
//
// Deleting `-home` from hookCommand turns this red; nothing else in either
// package notices, which is the whole reason it exists (#61, and the guard-one-
// layer-below class that produced #58, #62 and #63).
func TestInstalledHookCommandDrivesTheShim(t *testing.T) {
	home := t.TempDir()
	cwd := checkout(t, "git@github.com:unrounded/api.git")

	if err := cutover.InstallClaudeHooks(home, "/opt/substrate-adapter"); err != nil {
		t.Fatal(err)
	}
	argv := installedHookArgv(t, home)

	var stderr strings.Builder
	if code := hookCmd(argv, strings.NewReader(hookJSON(t, cwd)), &stderr); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if n := outboxDepth(t, home); n != 1 {
		t.Fatalf("the installed argv %q enqueued %d observations, want 1; stderr: %s",
			argv, n, stderr.String())
	}
}

// The same agreement where home is awkward: the command string is shell-quoted,
// so a home with a space in it must still reach the shim as one argument.
func TestInstalledHookCommandSurvivesAwkwardHome(t *testing.T) {
	home := t.TempDir() + "/two words"
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	cwd := checkout(t, "git@github.com:unrounded/api.git")

	if err := cutover.InstallClaudeHooks(home, "/opt/substrate-adapter"); err != nil {
		t.Fatal(err)
	}
	argv := installedHookArgv(t, home)

	var stderr strings.Builder
	if code := hookCmd(argv, strings.NewReader(hookJSON(t, cwd)), &stderr); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if n := outboxDepth(t, home); n != 1 {
		t.Fatalf("argv %q enqueued %d, want 1; stderr: %s", argv, n, stderr.String())
	}
}

// The quoting case a hand-rolled splitter gets wrong: an apostrophe in the home
// path. shellQuote emits the four-character `'\”` escape for it, and only a
// real shell reassembles that into a single argument.
func TestInstalledHookCommandSurvivesAnApostropheHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "o'brien")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	cwd := checkout(t, "git@github.com:unrounded/api.git")

	if err := cutover.InstallClaudeHooks(home, "/opt/substrate-adapter"); err != nil {
		t.Fatal(err)
	}
	argv := installedHookArgv(t, home)
	if argv[len(argv)-1] != home {
		t.Fatalf("home reached the shim as %q, want %q", argv[len(argv)-1], home)
	}

	var stderr strings.Builder
	if code := hookCmd(argv, strings.NewReader(hookJSON(t, cwd)), &stderr); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if n := outboxDepth(t, home); n != 1 {
		t.Fatalf("argv %q enqueued %d, want 1; stderr: %s", argv, n, stderr.String())
	}
}
