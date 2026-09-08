package cli

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestCompletionRejectsUnknownShell verifies a typo'd shell name fails loudly
// rather than silently emitting an empty script.
func TestCompletionRejectsUnknownShell(t *testing.T) {
	_, _, err := run(t, Deps{}, "completion", "powershell")
	if err == nil {
		t.Fatalf("want an error for an unsupported shell, got nil")
	}
	if !strings.Contains(err.Error(), "powershell") {
		t.Fatalf("error %q does not name the rejected shell", err.Error())
	}
}

// TestCompletionDemandsAShell verifies the command asks for a shell rather
// than guessing one.
func TestCompletionDemandsAShell(t *testing.T) {
	if _, _, err := run(t, Deps{}, "completion"); err == nil {
		t.Fatalf("want an error when no shell is given, got nil")
	}
}

// TestCompletionIsGeneratedForThisBinary asserts the scripts are generated
// from this root rather than hand-written: cobra's completions are dynamic, so
// each one must call back into `substrate __complete` for its candidates. That
// callback is what makes the completions incapable of drifting from the real
// command tree, which is the whole reason this command generates.
func TestCompletionIsGeneratedForThisBinary(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		out, _, err := run(t, Deps{}, "completion", shell)
		if err != nil {
			t.Fatalf("completion %s: %v", shell, err)
		}
		if !strings.Contains(out, "substrate") {
			t.Fatalf("%s completion script never names the substrate binary", shell)
		}
		if !strings.Contains(out, ShellCompRequestCmd) {
			t.Fatalf("%s completion script does not call back into %q; it is not driven by the real command tree:\n%s", shell, ShellCompRequestCmd, out)
		}
	}
}

// TestCompletionFishIsWellFormed asserts the generated fish script structurally
// rather than by invoking fish. fish is NOT part of the ubuntu-latest runner
// image (bash and zsh are), so a `fish --no-execute` check would fail CI on
// every run, and installing a shell just to lint a generated file is a worse
// trade than asserting the generator's contract directly. The generator is
// cobra's, so the risk being covered here is "did we call it and get a real
// script", which these assertions cover exactly.
func TestCompletionFishIsWellFormed(t *testing.T) {
	out, _, err := run(t, Deps{}, "completion", "fish")
	if err != nil {
		t.Fatalf("completion fish: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("fish completion script is empty")
	}
	if !strings.Contains(out, "complete -c substrate") {
		t.Fatalf("fish completion script has no `complete -c substrate` directive:\n%s", out)
	}
	if strings.Count(out, "function ") == 0 {
		t.Fatalf("fish completion script defines no functions; it is not a usable script:\n%s", out)
	}
}

// TestCompletionBashParses asserts the generated bash script parses under
// `bash -n`, so a broken script is caught here and not in a user's shell.
func TestCompletionBashParses(t *testing.T) {
	out, _, err := run(t, Deps{}, "completion", "bash")
	if err != nil {
		t.Fatalf("completion bash: %v", err)
	}
	parseWith(t, "bash", out)
}

// TestCompletionZshParses asserts the generated zsh script parses under
// `zsh -n`. zsh ships on the ubuntu-latest runner, so this is a real gate.
func TestCompletionZshParses(t *testing.T) {
	out, _, err := run(t, Deps{}, "completion", "zsh")
	if err != nil {
		t.Fatalf("completion zsh: %v", err)
	}
	parseWith(t, "zsh", out)
}

// TestCompletionNoDescriptions verifies --no-descriptions still produces a
// script, so the flag cannot silently become a no-op that emits nothing.
func TestCompletionNoDescriptions(t *testing.T) {
	out, _, err := run(t, Deps{}, "completion", "zsh", "--no-descriptions")
	if err != nil {
		t.Fatalf("completion zsh --no-descriptions: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("--no-descriptions produced an empty script")
	}
}

// parseWith runs `<shell> -n -` over script. A missing shell is a failure
// naming the fix, never a skip: a skipped test is a silently missing check.
func parseWith(t *testing.T, shell, script string) {
	t.Helper()
	if _, err := exec.LookPath(shell); err != nil {
		t.Fatalf("%s is not installed, so the generated completion script is unverified; install it (e.g. `apt-get install -y %s`) — this check is not optional", shell, shell)
	}
	cmd := exec.Command(shell, "-n", "-") //nolint:gosec // test-only: shell is a fixed literal (bash/zsh) from this file's call sites, never external input
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s -n: %v\n%s", shell, err, stderr.String())
	}
}
