package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeToken = "7f" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"

func okServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"targets":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Turn red by writing config.json with os.WriteFile's default mode: the file
// holds a token that never expires, so a group-readable copy is a leak.
func TestAuthLoginWritesConfig0600(t *testing.T) {
	dir := t.TempDir()
	srv := okServer(t)
	out, err := runRoot(t, Deps{
		ConfigDir: dir,
		Stdin:     strings.NewReader(fakeToken + "\n"),
		HTTP:      srv.Client(),
	}, "auth", "login", "--server", srv.URL)
	if err != nil {
		t.Fatalf("auth login: %v\n%s", err, out)
	}
	info, err := os.Stat(filepath.Join(dir, ConfigFileName))
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", info.Mode().Perm())
	}
	if strings.Contains(out, fakeToken) {
		t.Fatalf("auth login echoed the token back:\n%s", out)
	}
}

// Turn red by downgrading the mode check in loadConfig to a warning: ssh
// refuses a world-readable key for the same reason, and so must this.
func TestLoadConfigRefuses0644(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	//nolint:gosec // a world-readable credential file is precisely what this test seeds
	if err := os.WriteFile(path, []byte(`{"server":"https://x","token":"`+fakeToken+`"}`), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	out, err := runRoot(t, Deps{ConfigDir: dir}, "auth", "status")
	if err == nil {
		t.Fatalf("auth status accepted a 0644 credential file:\n%s", out)
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("error does not name the fix: %v", err)
	}
	if strings.Contains(err.Error(), fakeToken) {
		t.Fatal("the refusal message leaked the token")
	}
}

// Turn red by adding a Token field to authStatus: no command may print the
// stored token, in text or in --json.
func TestAuthStatusNeverPrintsTheToken(t *testing.T) {
	dir := t.TempDir()
	if err := writeJSON0600(filepath.Join(dir, ConfigFileName), Config{Server: "https://x", Token: fakeToken}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	for _, args := range [][]string{
		{"auth", "status"},
		{"auth", "status", "--json"},
	} {
		out, err := runRoot(t, Deps{ConfigDir: dir}, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if strings.Contains(out, fakeToken) {
			t.Fatalf("%v printed the token:\n%s", args, out)
		}
		if !strings.Contains(out, "https://x") {
			t.Fatalf("%v did not report the server:\n%s", args, out)
		}
	}
}

// Turn red by accepting the token as an argument: an argv token is visible in
// ps output and recorded by the shell.
func TestAuthLoginTakesNoTokenArgument(t *testing.T) {
	out, err := runRoot(t, Deps{ConfigDir: t.TempDir(), Stdin: strings.NewReader("")}, "auth", "login", "--server", "https://x", fakeToken)
	if err == nil {
		t.Fatalf("auth login accepted a positional token:\n%s", out)
	}
}

// Turn red by dropping fs.SetOutput-equivalent help wiring from the subcommand:
// "-h" printing nothing is the bug the legacy flagsets have at 10 sites.
func TestAuthLoginHelpPrintsUsage(t *testing.T) {
	out, err := runRoot(t, Deps{ConfigDir: t.TempDir()}, "auth", "login", "-h")
	if err != nil {
		t.Fatalf("auth login -h: %v", err)
	}
	for _, want := range []string{"Usage:", "--server", "stdin", "Examples:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("auth login -h missing %q:\n%s", want, out)
		}
	}
}

// Turn red by storing the token before validating it: a typo then becomes a
// 0600 file full of a credential that never worked.
func TestAuthLoginRejectsAnUnauthorizedToken(t *testing.T) {
	dir := t.TempDir()
	srv := okServer(t)
	other := "aa" + fakeToken[2:]
	out, err := runRoot(t, Deps{
		ConfigDir: dir,
		Stdin:     strings.NewReader(other + "\n"),
		HTTP:      srv.Client(),
	}, "auth", "login", "--server", srv.URL)
	if err == nil {
		t.Fatalf("auth login stored a token the server rejected:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ConfigFileName)); statErr == nil {
		t.Fatal("auth login wrote config.json for a rejected token")
	}
}
