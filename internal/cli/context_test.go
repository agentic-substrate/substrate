package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runRoot(t *testing.T, d Deps, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	d.Stdout = &out
	d.Stderr = &out
	root := NewRoot(d)
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// Turn red by moving the flag branch below the context-file branch in
// resolveScope: an explicit --org then loses to a stale saved context.
func TestContextShowSourceFlags(t *testing.T) {
	dir := t.TempDir()
	if err := writeJSON0600(filepath.Join(dir, ContextFileName), Scope{Org: "saved"}); err != nil {
		t.Fatalf("seed context: %v", err)
	}
	out, err := runRoot(t, Deps{ConfigDir: dir}, "context", "show", "--org", "flagged")
	if err != nil {
		t.Fatalf("context show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "flagged") || !strings.Contains(out, "source:  "+SourceFlags) {
		t.Fatalf("want the flag value and source %q, got:\n%s", SourceFlags, out)
	}
}

// Turn red by having resolveScope ignore ErrNoConfig and fall through to git:
// a saved context then stops being honoured inside any checkout.
func TestContextShowSourceFile(t *testing.T) {
	dir := t.TempDir()
	if err := writeJSON0600(filepath.Join(dir, ContextFileName), Scope{Org: "acme", Team: "platform"}); err != nil {
		t.Fatalf("seed context: %v", err)
	}
	out, err := runRoot(t, Deps{ConfigDir: dir}, "context", "show")
	if err != nil {
		t.Fatalf("context show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "source:  "+SourceFile) {
		t.Fatalf("want the saved org and source %q, got:\n%s", SourceFile, out)
	}
}

// Turn red by rendering the repo key as a scope string: a repo key is sent as
// the `repo` field and the server owns the remote-to-chain binding (R18).
func TestContextShowSourceGitRemote(t *testing.T) {
	repo := gitRepoWithOrigin(t, "git@github.com:acme/api.git")
	out, err := runRoot(t, Deps{
		ConfigDir: t.TempDir(),
		Getwd:     func() (string, error) { return repo, nil },
	}, "context", "show")
	if err != nil {
		t.Fatalf("context show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "repo:    github.com/acme/api") || !strings.Contains(out, "source:  "+SourceGit) {
		t.Fatalf("want the repo key and source %q, got:\n%s", SourceGit, out)
	}
	if strings.Contains(out, "org:") {
		t.Fatalf("a git remote must not be turned into an org scope:\n%s", out)
	}
}

// Turn red by defaulting resolveScope to global: work then files silently at a
// scope the caller may not write (#63) instead of asking for one.
func TestContextShowUnresolvedIsAnActionableError(t *testing.T) {
	out, err := runRoot(t, Deps{
		ConfigDir: t.TempDir(),
		Getwd:     func() (string, error) { return t.TempDir(), nil },
	}, "context", "show")
	if err == nil {
		t.Fatalf("context show resolved a scope out of nothing:\n%s", out)
	}
	if !strings.Contains(err.Error(), "next: ") || !strings.Contains(err.Error(), "context use") {
		t.Fatalf("error names no next step: %v", err)
	}
}

// Turn red by making context show load config.json: a user who has not logged
// in yet can still ask where they are pointed.
func TestContextShowDoesNotNeedAConfigFile(t *testing.T) {
	dir := t.TempDir()
	out, err := runRoot(t, Deps{ConfigDir: dir}, "context", "show", "--org", "acme")
	if err != nil {
		t.Fatalf("context show with no config.json: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ConfigFileName)); statErr == nil {
		t.Fatal("context show created a config file; it must not")
	}
}

// Turn red by writing context.json with os.WriteFile at 0644: the file sits
// beside a credential file in a directory the mode check governs.
func TestContextUseWritesTheFileItThenReads(t *testing.T) {
	dir := t.TempDir()
	if _, err := runRoot(t, Deps{ConfigDir: dir}, "context", "use", "--org", "acme", "--project", "api"); err != nil {
		t.Fatalf("context use: %v", err)
	}
	out, err := runRoot(t, Deps{ConfigDir: dir}, "context", "show")
	if err != nil {
		t.Fatalf("context show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "api") {
		t.Fatalf("context use did not round-trip:\n%s", out)
	}
}

func gitRepoWithOrigin(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", remote},
	} {
		cmd := exec.Command("git", args...) //nolint:gosec // fixed arguments
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s\nFix: install git.", args, err, out)
		}
	}
	return dir
}
