package cutover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCutoverRefusesDestDirSymlinkEscape(t *testing.T) {
	// Using filepath.Rel without EvalSymlinks is the one-line change that
	// makes this red. A dest-dir symlink must not let atomicWrite follow
	// the parent out of -root (same fixture as adapter.TestSyncRefusesDestDirSymlinkEscape).
	stage := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(stage, ".claude")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(stage, ".claude", "CLAUDE.md")
	_, err := Cutover(Request{
		Roots:     []string{stage},
		Home:      stage,
		Commit:    true,
		Installer: &FakeInstaller{},
		Files:     []Replacement{{Path: dest, Content: renderedClaude("2026-09-07T00:00:00Z")}},
	})
	if err == nil {
		t.Fatal("cutover followed a dest-dir symlink out of -root")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error = %v, want it to name the confinement refusal", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "CLAUDE.md")); err == nil {
		t.Fatal("wrote through a directory symlink that escaped -root")
	}
}

func TestDestinationsRefusesDestDirSymlinkEscape(t *testing.T) {
	// Lexical filepath.Rel in Destinations is the change that makes this red.
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	_, err := Destinations(home, nil, "~/.claude/CLAUDE.md")
	if err == nil {
		t.Fatal("Destinations accepted a dest-dir symlink out of home")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error = %v, want it to name the confinement refusal", err)
	}
}
