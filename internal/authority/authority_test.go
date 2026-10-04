package authority

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func fixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "repos", "ordinary")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	git(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	store := &Store{Dir: filepath.Join(home, "state")}
	if err := store.Initialize("Local owner"); err != nil {
		t.Fatal(err)
	}
	return store, root, home
}
func TestSessionStaysWithinExplicitBinding(t *testing.T) {
	store, root, home := fixture(t)
	if err := store.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	binding, err := store.Register(root, "Work")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(root, "Work")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := store.Authenticate(token, root)
	if err != nil || ctx.SpaceID != binding.SpaceID || ctx.RepositoryID != binding.RepositoryID {
		t.Fatalf("binding not retained: %#v %v", ctx, err)
	}
	if err := ctx.Authorize(binding.SpaceID, binding.RepositoryID); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Authorize("personal", binding.RepositoryID); err == nil {
		t.Fatal("context authorized another space")
	}
	if _, err := store.CreateSession(root, "Personal"); err == nil {
		t.Fatal("session changed registered space")
	}
	if _, err := store.Authenticate("forged", root); err == nil {
		t.Fatal("forged credential accepted")
	}
	other := filepath.Join(home, "repos", "work-sounding-name")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, other, "init", "-q")
	if _, err := store.Authenticate(token, other); err == nil {
		t.Fatal("tool cwd widened session")
	}
	if _, err := store.Authenticate(token, filepath.Join(home, "repos")); err == nil {
		t.Fatal("ambiguous parent accepted")
	}
	data, err := os.ReadFile(filepath.Join(store.Dir, "authority.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("raw credential persisted in authority")
	}
	if err := store.RevokeSession(token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(token, root); err == nil {
		t.Fatal("revoked credential accepted")
	}
}
func TestManifestCannotGrantMembership(t *testing.T) {
	store, root, home := fixture(t)
	binding, err := store.Register(root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	hint := Manifest{SchemaVersion: 1, RepositoryID: binding.RepositoryID, SpaceID: binding.SpaceID}
	data, _ := json.Marshal(hint)
	clone := filepath.Join(home, "repos", "copy")
	git(t, home, "clone", "-q", root, clone)
	if err := os.WriteFile(filepath.Join(clone, ".substrate.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(clone); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(clone, "Personal"); err == nil {
		t.Fatal("copied manifest registered clone")
	}
	token, err := store.CreateSession(root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	hint.SpaceID = "restricted-space"
	data, _ = json.Marshal(hint)
	if err := os.WriteFile(filepath.Join(root, ".substrate.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, err := store.Authenticate(token, root)
	if err != nil || ctx.SpaceID != binding.SpaceID {
		t.Fatalf("manifest changed accepted grant: %#v %v", ctx, err)
	}
}
func TestWorktreeAndNestedRepositoryNeedExplicitRegistration(t *testing.T) {
	store, root, home := fixture(t)
	binding, err := store.Register(root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(home, "repos", "sibling")
	git(t, root, "worktree", "add", "-q", sibling)
	if _, err := store.CreateSession(sibling, "Personal"); err == nil {
		t.Fatal("unregistered worktree received grant")
	}
	linked, err := store.Register(sibling, "Personal")
	if err != nil || linked.RepositoryID != binding.RepositoryID {
		t.Fatalf("logical repository not reused: %#v %v", linked, err)
	}
	if err := store.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Register(sibling, "Work"); err == nil {
		t.Fatal("same Git repository silently mixed spaces")
	}
	token, err := store.CreateSession(root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q")
	if _, err := store.Authenticate(token, nested); err == nil {
		t.Fatal("nested repository inherited parent grant")
	}
	if _, err := store.Register(nested, "Work"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, ".git"), filepath.Join(home, "old-git")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	if _, err := store.Authenticate(token, root); err == nil {
		t.Fatal("replaced Git binding accepted")
	}
}
func TestUnsafeAndUnavailableAuthorityFailsClosed(t *testing.T) {
	store, root, _ := fixture(t)
	if err := store.Initialize("Replacement"); err == nil {
		t.Fatal("owner replaced")
	}
	if _, err := store.Register(root, "Personal"); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(store.Dir, "authority.json")
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(token, root); err == nil {
		t.Fatal("world-readable authority accepted")
	}
	if err := os.Chmod(file, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(token, root); err == nil {
		t.Fatal("corrupt authority accepted")
	}
}
func TestSessionExpires(t *testing.T) {
	store, root, _ := fixture(t)
	if _, err := store.Register(root, "Personal"); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if _, err := store.Authenticate(token, root); err == nil {
		t.Fatal("expired session accepted")
	}
}
func TestDiscoveryRejectsMalformedOversizedAndSymlinkManifest(t *testing.T) {
	_, root, home := fixture(t)
	file := filepath.Join(root, ".substrate.json")
	for _, data := range []string{`{"schema_version":1,"repo_id":"x","space_id":"y","token":"secret"}`, strings.Repeat("x", 8193), `{"schema_version":99,"repo_id":"x","space_id":"y"}`} {
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Discover(root); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "hint")
	if err := os.WriteFile(target, []byte(`{"schema_version":1,"repo_id":"x","space_id":"y"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, file); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root); err == nil {
		t.Fatal("symlink manifest accepted")
	}
}

func TestConcurrentSessionsSurviveIndependentStoreInstances(t *testing.T) {
	store, root, _ := fixture(t)
	if _, err := store.Register(root, "Personal"); err != nil {
		t.Fatal(err)
	}
	type result struct {
		token string
		err   error
	}
	results := make(chan result, 12)
	for range 12 {
		go func() {
			token, err := (&Store{Dir: store.Dir}).CreateSession(root, "Personal")
			results <- result{token, err}
		}()
	}
	for range 12 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if _, err := store.Authenticate(r.token, root); err != nil {
			t.Fatalf("concurrent grant lost: %v", err)
		}
	}
}
func TestFailedPersistenceDoesNotReturnCredential(t *testing.T) {
	store, root, _ := fixture(t)
	if _, err := store.Register(root, "Personal"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(store.Dir, "authority.json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(root, "Personal")
	if err == nil || token != "" {
		t.Fatalf("failed persistence returned credential: %q %v", token, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCredentialsMustBePrivateAndOutsideCheckout(t *testing.T) {
	_, root, home := fixture(t)
	token := strings.Repeat("a", 64)
	if err := WriteCredential(filepath.Join(root, "secret"), token); err == nil {
		t.Fatal("credential written into checkout")
	}
	file := filepath.Join(home, "credential")
	if err := WriteCredential(file, token); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredential(file, token); err == nil {
		t.Fatal("existing credential overwritten")
	}
	got, err := ReadCredential(file)
	if err != nil || got != token {
		t.Fatalf("credential not readable: %q %v", got, err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(file); err == nil {
		t.Fatal("public credential accepted")
	}
}

func TestOversizedUpdatePreservesReadableAuthority(t *testing.T) {
	store, root, _ := fixture(t)
	if _, err := store.Register(root, "Personal"); err != nil {
		t.Fatal(err)
	}
	err := store.update(false, func(st *state) error { st.OwnerName = strings.Repeat("x", 1024*1024); return nil })
	if err == nil {
		t.Fatal("oversized authority acknowledged")
	}
	if _, err := store.CreateSession(root, "Personal"); err != nil {
		t.Fatalf("failed update damaged authority: %v", err)
	}
}
func TestMovedAuthorityInsideCheckoutIsRejected(t *testing.T) {
	store, root, _ := fixture(t)
	moved := filepath.Join(root, "private-state")
	if err := os.Rename(store.Dir, moved); err != nil {
		t.Fatal(err)
	}
	store.Dir = moved
	if _, err := store.Register(root, "Personal"); err == nil {
		t.Fatal("authority inside Git accepted")
	}
}

func TestAtomicWriteFailureDoesNotReturnGrant(t *testing.T) {
	store, root, _ := fixture(t)
	if _, err := store.Register(root, "Personal"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(store.Dir, 0700)
	token, err := store.CreateSession(root, "Personal")
	if err == nil || token != "" {
		t.Fatalf("failed atomic write returned grant: %q %v", token, err)
	}
	if err := os.Chmod(store.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(root, "Personal"); err != nil {
		t.Fatalf("failure damaged state: %v", err)
	}
}
func TestCopiedCredentialInsideCheckoutIsRejected(t *testing.T) {
	_, root, _ := fixture(t)
	file := filepath.Join(root, "copied-secret")
	if err := os.WriteFile(file, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(file); err == nil {
		t.Fatal("checkout credential accepted")
	}
}

func TestNonregularManifestAndCredentialRejectPromptly(t *testing.T) {
	_, root, home := fixture(t)
	manifest := filepath.Join(root, ".substrate.json")
	credential := filepath.Join(home, "pipe-credential")
	for _, path := range []string{manifest, credential} {
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	checks := []func() error{func() error { _, err := Discover(root); return err }, func() error { _, err := ReadCredential(credential); return err }}
	for _, check := range checks {
		result := make(chan error, 1)
		go func() { result <- check() }()
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("named pipe accepted")
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("named pipe blocked verification")
		}
	}
}
func TestFailedRegistrationReturnsNoBindingReceipt(t *testing.T) {
	store, root, _ := fixture(t)
	if err := os.Chmod(store.Dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(store.Dir, 0700)
	binding, err := store.Register(root, "Personal")
	if err == nil || binding.RepositoryID != "" {
		t.Fatalf("failed registration returned binding: %#v %v", binding, err)
	}
}
func TestOversizedPersistedStateIsUnavailable(t *testing.T) {
	store, _, _ := fixture(t)
	file := filepath.Join(store.Dir, "authority.json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(strings.Repeat(" ", 1024*1024))...)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Inventory(); err == nil {
		t.Fatal("oversized persisted authority accepted")
	}
}

func TestMalformedGitConfigurationCannotPermitPrivatePlacement(t *testing.T) {
	store, root, home := fixture(t)
	file := filepath.Join(root, "copied-credential")
	if err := os.WriteFile(file, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredential(filepath.Join(root, "new-credential"), strings.Repeat("a", 64)); err == nil {
		t.Fatal("malformed Git config allowed credential creation")
	}
	if _, err := ReadCredential(file); err == nil {
		t.Fatal("malformed Git config allowed checkout credential read")
	}
	inside := filepath.Join(root, "new-parent", "state")
	if err := (&Store{Dir: inside}).Initialize("Owner"); err == nil {
		t.Fatal("malformed Git config allowed authority creation")
	}
	if _, err := os.Stat(filepath.Join(root, "new-parent")); !os.IsNotExist(err) {
		t.Fatalf("denied setup created parent: %v", err)
	}
	moved := filepath.Join(root, "existing-state")
	if err := os.Rename(store.Dir, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Store{Dir: moved}).Inventory(); err == nil {
		t.Fatal("malformed Git config allowed moved authority read")
	}
	outside := filepath.Join(home, "outside-credential")
	if err := WriteCredential(outside, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("separate placement rejected: %v", err)
	}
}
func TestMissingGitRejectsPrivatePlacementWithoutCreatingFiles(t *testing.T) {
	store, _, home := fixture(t)
	credential := filepath.Join(home, "existing-credential")
	if err := WriteCredential(credential, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	outside := filepath.Join(home, "new-parent", "state")
	checks := []func() error{
		func() error { return (&Store{Dir: outside}).Initialize("Owner") },
		func() error { return WriteCredential(filepath.Join(home, "new-credential"), strings.Repeat("a", 64)) },
		func() error { _, err := ReadCredential(credential); return err },
		func() error { _, err := store.Inventory(); return err },
	}
	for _, check := range checks {
		if err := check(); err == nil || !strings.Contains(err.Error(), "Git") {
			t.Fatalf("missing Git not actionable: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "new-parent")); !os.IsNotExist(err) {
		t.Fatalf("missing prerequisite created state parent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "new-credential")); !os.IsNotExist(err) {
		t.Fatalf("missing prerequisite created credential: %v", err)
	}
}

func TestCheckoutPreservesTrailingPathWhitespace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project ")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	binding, err := checkout(root)
	if err != nil || binding.Checkout != root {
		t.Fatalf("trailing-space checkout lost pathname bytes: %#v %v", binding, err)
	}
}
