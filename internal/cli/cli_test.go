package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/cutover"
)

// run drives the real command tree the way main does, with a piped (non-TTY)
// stdin unless the caller replaces it.
func run(t *testing.T, d Deps, args ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	d.Stdout = &out
	d.Stderr = &errb
	if d.Stdin == nil {
		d.Stdin = strings.NewReader("")
	}
	if d.Env == nil {
		d.Env = func(string) string { return "" }
	}
	if d.ConfigDir == "" {
		d.ConfigDir = t.TempDir()
	}
	root := NewRoot(d)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errb.String(), err
}

// The CLI surface must not carry a dry-run flag or banner any more: the whole
// point of the change is that a preview is a verb, not a silent exit-0 flag.
// Mutation that turns this red: re-adding a --dry-run flag to any command.
func TestNoDryRunOnTheCLISurface(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if bytes.Contains(body, []byte("dry-run")) {
			t.Fatalf("%s mentions dry-run; the CLI surface no longer has one", e.Name())
		}
	}
}

// review approve off a TTY without --yes must refuse before the write.
// Mutation that turns this red: dropping the confirm() call from the decide
// command, which makes the server see a decide POST.
func TestReviewApproveOffTTYWithoutYesWritesNothing(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts++
		w.Write([]byte(`{}`)) //nolint:errcheck // test server
	}))
	defer srv.Close()
	_, _, err := run(t, Deps{HTTP: srv.Client()},
		"review", "approve", "5b0d1f4a-0000-0000-0000-000000000000",
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

// With --yes the same command commits, and the wire body still carries the
// dry_run/commit pair the server re-derives from.
// Mutation that turns this red: sending dry_run:true, or dropping the fields.
func TestReviewApproveWithYesCommits(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"id":"x","status":"approved"}`)) //nolint:errcheck // test server
	}))
	defer srv.Close()
	if _, _, err := run(t, Deps{HTTP: srv.Client()},
		"review", "approve", "x", "--server", srv.URL, "--token", "t", "--yes"); err != nil {
		t.Fatalf("approve --yes: %v", err)
	}
	if body["commit"] != true || body["dry_run"] != false {
		t.Fatalf("wire body must keep commit=true dry_run=false, got %v", body)
	}
}

// reject without --reason is refused before anything is sent.
// Mutation that turns this red: making --reason optional on reject.
func TestReviewRejectRequiresReason(t *testing.T) {
	_, _, err := run(t, Deps{}, "review", "reject", "x", "--server", "http://x", "--token", "t", "--yes")
	if err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("want a --reason error, got %v", err)
	}
}

// import apply prints the resolved scope and the source that won before it
// writes, because the server never discloses the chain afterwards.
// Mutation that turns this red: moving the echo after the POST, or dropping
// the source half of the line.
func TestImportApplyEchoesScopeAndSourceBeforeWriting(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen, _ = body["scope"].(string)
		w.Write([]byte(`{}`)) //nolint:errcheck // test server
	}))
	defer srv.Close()
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	out, _, err := run(t, Deps{HTTP: srv.Client()}, "import", "apply", plan,
		"--machine", "m", "--trusted", "m", "--scope", "org:acme",
		"--server", srv.URL, "--token", "t", "--yes")
	if err != nil {
		t.Fatalf("import apply: %v", err)
	}
	if !strings.Contains(out, "scope: org:acme (source: --scope flag)") {
		t.Fatalf("want the scope and its source echoed, got %q", out)
	}
	if seen != "org:acme" {
		t.Fatalf("server saw scope %q", seen)
	}
}

// With no --scope and no context file, the checkout's git origin decides and
// the CLI sends `repo`, never a chain it invented (R18).
// Mutation that turns this red: falling back to global:, or sending scope.
func TestImportApplyFallsBackToGitRemote(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{}`)) //nolint:errcheck // test server
	}))
	defer srv.Close()
	repo := gitCheckout(t, "git@github.com:acme/api.git")
	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(plan, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	out, _, err := run(t, Deps{HTTP: srv.Client(), Getwd: func() (string, error) { return repo, nil }},
		"import", "apply", plan, "--machine", "m", "--trusted", "m",
		"--server", srv.URL, "--token", "t", "--yes")
	if err != nil {
		t.Fatalf("import apply: %v", err)
	}
	// The source label is #105's SourceGit constant, shared with `context show`
	// so the two commands can never name the same resolution differently.
	if !strings.Contains(out, "repo: github.com/acme/api (source: "+SourceGit+")") {
		t.Fatalf("want the inferred repo and its source echoed, got %q", out)
	}
	if body["repo"] != "github.com/acme/api" {
		t.Fatalf("want repo on the wire, got %v", body)
	}
	if _, ok := body["scope"]; ok {
		t.Fatalf("the CLI must not invent a scope from a remote, got %v", body["scope"])
	}
}

// adapter status is the preview verb: it classifies and records no installer
// call at all.
// Mutation that turns this red: passing Commit:true from the status path.
func TestAdapterStatusWritesNothing(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(live), 0o750); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(live, []byte("live\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := renderServer(t)
	defer srv.Close()
	fake := &cutover.FakeInstaller{}
	out, _, err := run(t, Deps{HTTP: srv.Client(), Installer: fake},
		"adapter", "status", "--root", root, "--machine", "m",
		"--server", srv.URL, "--token", "t")
	if err != nil {
		t.Fatalf("adapter status: %v", err)
	}
	if len(fake.Installs) != 0 || fake.Uninstalls != 0 {
		t.Fatalf("status must record zero installer writes, got %+v", fake)
	}
	body, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil || string(body) != "live\n" {
		t.Fatalf("status must not touch the live file, got %q %v", body, err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("status must print the classification")
	}
}

// uninstall restores in one invocation with no --restore flag, and --force is
// still the documented way to discard live edits that no longer match the
// DriftHash install wrote.
// Mutation that turns this red: re-adding --restore, or dropping --force.
func TestAdapterUninstallRestoresAndKeepsForce(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(live), 0o750); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(live, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := renderServer(t)
	defer srv.Close()
	install := func() {
		t.Helper()
		if _, _, err := run(t, Deps{HTTP: srv.Client(), Installer: &cutover.FakeInstaller{}},
			"adapter", "install", "--root", root, "--machine", "m",
			"--server", srv.URL, "--token", "t", "--yes"); err != nil {
			t.Fatalf("adapter install: %v", err)
		}
	}
	install()
	if body, err := os.ReadFile(live); err != nil || string(body) != "rendered\n" { //nolint:gosec // under t.TempDir
		t.Fatalf("install did not write the render: %q %v", body, err)
	}
	if err := os.WriteFile(live, []byte("edited by hand\n"), 0o600); err != nil {
		t.Fatalf("drift: %v", err)
	}
	_, _, err := run(t, Deps{Installer: &cutover.FakeInstaller{}},
		"adapter", "uninstall", "--root", root, "--yes")
	if err == nil || !strings.Contains(err.Error(), "force") {
		t.Fatalf("a drifted file must block uninstall and name --force, got %v", err)
	}
	if _, _, err := run(t, Deps{Installer: &cutover.FakeInstaller{}},
		"adapter", "uninstall", "--root", root, "--force", "--yes"); err != nil {
		t.Fatalf("adapter uninstall --force: %v", err)
	}
	if body, err := os.ReadFile(live); err != nil || string(body) != "original\n" { //nolint:gosec // under t.TempDir
		t.Fatalf("--force must discard the drifted edit and restore the original, got %q %v", body, err)
	}
	if _, _, err := run(t, Deps{Installer: &cutover.FakeInstaller{}},
		"adapter", "uninstall", "--root", root, "--restore", "--yes"); err == nil {
		t.Fatal("--restore must be gone")
	}
}

// renderServer serves one home-scoped render target.
func renderServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"targets":[{"path":"~/.claude/CLAUDE.md","content":"rendered\n"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// gitCheckout makes a real checkout with one origin, because RepoKey shells
// out to git and a fake would test nothing.
func gitCheckout(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", remote},
	} {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed git subcommands in a t.TempDir
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}
