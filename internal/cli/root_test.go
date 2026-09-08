package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Turn red by dropping Long/Example from newRootCmd: -h then prints no usage,
// which is the bug this issue exists to fix (every legacy flagset discards it).
func TestRootHelpPrintsUsage(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(Deps{Stdout: &out, Stderr: &out, ConfigDir: t.TempDir()})
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"-h"})
	if err := root.Execute(); err != nil {
		t.Fatalf("substrate -h: %v", err)
	}
	for _, want := range []string{"Usage:", "auth", "context", "doctor", "Examples:"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("substrate -h output missing %q:\n%s", want, out.String())
		}
	}
}
