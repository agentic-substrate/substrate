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

// TestCompletionFishParses asserts the generated fish script parses under
// `fish --no-execute`. fish is not in the ubuntu-latest runner image, so CI
// installs it explicitly (see .github/workflows/ci.yml) rather than dropping
// the check -- a completion script that does not parse is a broken script, and
// a check that silently does not run is worse than no check.
func TestCompletionFishParses(t *testing.T) {
	out, _, err := run(t, Deps{}, "completion", "fish")
	if err != nil {
		t.Fatalf("completion fish: %v", err)
	}
	if !strings.Contains(out, "complete -c substrate") {
		t.Fatalf("fish completion script has no `complete -c substrate` directive:\n%s", out)
	}
	parseWith(t, "fish", out)
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

// parseWith syntax-checks script with the real shell. A missing shell is a
// failure naming the fix, never a skip: a skipped test is a silently missing
// check, and CI installs every shell named here.
func parseWith(t *testing.T, shell, script string) {
	t.Helper()
	if _, err := exec.LookPath(shell); err != nil {
		t.Fatalf("%s is not installed, so the generated completion script is unverified; install it (e.g. `apt-get install -y %s`) — this check is not optional", shell, shell)
	}
	args := []string{"-n", "-"}
	if shell == "fish" {
		// fish has no `-n`; --no-execute parses without running.
		args = []string{"--no-execute"}
	}
	cmd := exec.Command(shell, args...) //nolint:gosec // test-only: shell is a fixed literal (bash/zsh/fish) from this file's call sites, never external input
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v\n%s", shell, args, err, stderr.String())
	}
}
