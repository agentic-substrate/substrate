package cutover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettingsFile(t *testing.T, home, body string) string {
	t.Helper()
	path := ClaudeSettingsPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s is not JSON after the merge: %v\n%s", path, err, raw)
	}
	return out
}

// countOurEntries walks settings.json the way Claude Code would and counts the
// hook entries naming the shim, per event.
func countOurEntries(t *testing.T, path string) map[string]int {
	t.Helper()
	doc := readJSON(t, path)
	out := map[string]int{}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return out
	}
	for ev, raw := range hooks {
		groups, ok := raw.([]any)
		if !ok {
			continue
		}
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			entries, ok := gm["hooks"].([]any)
			if !ok {
				continue
			}
			for _, e := range entries {
				em, ok := e.(map[string]any)
				if !ok {
					continue
				}
				if cmd, ok := em["command"].(string); ok && isOurs(cmd) {
					out[ev]++
				}
			}
		}
	}
	return out
}

const userSettings = `{
  "env": {"FOO": "bar"},
  "permissions": {"defaultMode": "auto"},
  "hooks": {
    "SessionStart": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "bash /home/u/mine.sh", "timeout": 10}]}
    ]
  }
}
`

// settings.json is a user-owned file: installing must merge, installing twice
// must not duplicate, and every key Substrate does not own must survive.
func TestInstallClaudeHooksIsIdempotentAndPreservesUnrelatedKeys(t *testing.T) {
	home := t.TempDir()
	path := writeSettingsFile(t, home, userSettings)

	if err := InstallClaudeHooks(home, "/opt/substrate-adapter"); err != nil {
		t.Fatalf("install: %v", err)
	}
	first, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := InstallClaudeHooks(home, "/opt/substrate-adapter"); err != nil {
		t.Fatalf("second install: %v", err)
	}
	second, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("second install changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	counts := countOurEntries(t, path)
	for _, ev := range HookEvents() {
		if counts[ev] != 1 {
			t.Fatalf("hooks.%s has %d entries naming the shim, want exactly 1:\n%s", ev, counts[ev], second)
		}
	}

	doc := readJSON(t, path)
	env, ok := doc["env"].(map[string]any)
	if !ok || env["FOO"] != "bar" {
		t.Fatalf("unrelated key env was not preserved: %v", doc["env"])
	}
	perms, ok := doc["permissions"].(map[string]any)
	if !ok || perms["defaultMode"] != "auto" {
		t.Fatalf("unrelated key permissions was not preserved: %v", doc["permissions"])
	}
	hooks := doc["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"]; !ok {
		t.Fatalf("the user's own SessionStart hook was dropped: %v", hooks)
	}
	if !ClaudeHooksInstalled(home) {
		t.Fatal("ClaudeHooksInstalled is false right after a successful install")
	}
}

// The backup is the one copy of the user's pre-Substrate settings, so it is
// taken before the first modification and never overwritten afterwards.
func TestInstallClaudeHooksBacksUpOnceAndNeverOverwrites(t *testing.T) {
	home := t.TempDir()
	path := writeSettingsFile(t, home, userSettings)
	backup := path + BackupSuffix

	if err := InstallClaudeHooks(home, "adapter"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(backup) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatalf("no backup was taken: %v", err)
	}
	if string(got) != userSettings {
		t.Fatalf("backup is not the pre-install file:\n%s", got)
	}

	// Simulate a later edit + reinstall: the backup must not be clobbered.
	if err := os.WriteFile(path, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaudeHooks(home, "adapter"); err != nil {
		t.Fatal(err)
	}
	got2, err := os.ReadFile(backup) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != userSettings {
		t.Fatalf("a second install overwrote the backup:\n%s", got2)
	}
}

// Restore must put the user's original file back byte-for-byte, which also
// removes the hook. A settings.json still naming an uninstalled binary would
// make every tool call in every session pay a failed exec.
func TestRemoveClaudeHooksRestoresOriginal(t *testing.T) {
	home := t.TempDir()
	path := writeSettingsFile(t, home, userSettings)

	if err := InstallClaudeHooks(home, "adapter"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveClaudeHooks(home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != userSettings {
		t.Fatalf("restore did not put the original back:\ngot:\n%s\nwant:\n%s", got, userSettings)
	}
	if strings.Contains(string(got), HookSubcommand) {
		t.Fatalf("the shim is still referenced after restore:\n%s", got)
	}
	if ClaudeHooksInstalled(home) {
		t.Fatal("ClaudeHooksInstalled is true after restore")
	}
	if _, err := os.Lstat(path + BackupSuffix); !os.IsNotExist(err) {
		t.Fatalf("the backup was left behind after restore: %v", err)
	}
}

// With no backup present (nothing to restore), removal is surgical: our entry
// goes, the user's stays.
func TestRemoveClaudeHooksSurgicalWithoutBackup(t *testing.T) {
	home := t.TempDir()
	path := writeSettingsFile(t, home, userSettings)
	if err := InstallClaudeHooks(home, "adapter"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + BackupSuffix); err != nil {
		t.Fatal(err)
	}
	if err := RemoveClaudeHooks(home); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), HookSubcommand) {
		t.Fatalf("the shim survived a surgical removal:\n%s", body)
	}
	if !strings.Contains(string(body), "mine.sh") {
		t.Fatalf("the user's own hook was removed too:\n%s", body)
	}
	doc := readJSON(t, path)
	if _, ok := doc["env"]; !ok {
		t.Fatalf("unrelated keys were dropped by removal: %v", doc)
	}
}

// A settings.json that did not exist before install is created, and removal
// takes it away again rather than leaving an empty husk.
func TestInstallAndRemoveWhenSettingsAbsent(t *testing.T) {
	home := t.TempDir()
	path := ClaudeSettingsPath(home)
	if err := InstallClaudeHooks(home, "adapter"); err != nil {
		t.Fatal(err)
	}
	if !ClaudeHooksInstalled(home) {
		t.Fatal("hook not installed into a fresh settings.json")
	}
	if _, err := os.Lstat(path + BackupSuffix); !os.IsNotExist(err) {
		t.Fatal("a backup was written for a file that did not exist")
	}
	if err := RemoveClaudeHooks(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("settings.json we created was left behind: %v", err)
	}
}

// A settings.json we cannot parse is not rewritten: it holds settings the user
// owns and a template rewrite would destroy them.
func TestInstallClaudeHooksRefusesUnparseableSettings(t *testing.T) {
	home := t.TempDir()
	path := writeSettingsFile(t, home, "{ not json")
	if err := InstallClaudeHooks(home, "adapter"); err == nil {
		t.Fatal("install rewrote a settings.json it could not parse")
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "{ not json" {
		t.Fatalf("the unparseable file was modified:\n%s", body)
	}
}

// The single worst outcome in this feature: uninstall leaves settings.json
// pointing at a binary that is no longer installed, so every tool call in every
// session pays a failed exec. Cutover must merge the hook and Restore must take
// it back out. Dropping the RemoveClaudeHooks call from applyRestore is the
// one-line production change that turns this red.
func TestCutoverInstallsHookAndRestoreRemovesIt(t *testing.T) {
	root := t.TempDir()
	path := writeSettingsFile(t, root, userSettings)
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	if err := os.WriteFile(live, []byte("# original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &FakeInstaller{}

	rep, err := Cutover(Request{
		Roots:     []string{root},
		Home:      root,
		Commit:    true,
		Installer: inst,
		Binary:    "/opt/bin/substrate-adapter",
		Files:     []Replacement{{Path: live, Content: "# generated\n"}},
	})
	if err != nil {
		t.Fatalf("cutover: %v", err)
	}
	if !strings.Contains(rep.Format(), ClaudeSettingsPath(root)) {
		t.Fatalf("the plan never mentions settings.json:\n%s", rep.Format())
	}
	if !ClaudeHooksInstalled(root) {
		body, _ := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
		t.Fatalf("cutover did not install the hook:\n%s", body)
	}

	if _, err := Restore(Request{Roots: []string{root}, Home: root, Commit: true, Installer: inst}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir()
	if err != nil {
		t.Fatalf("settings.json is gone after restore: %v", err)
	}
	if strings.Contains(string(body), HookSubcommand) {
		t.Fatalf("restore left a hook pointing at an uninstalled binary:\n%s", body)
	}
	if string(body) != userSettings {
		t.Fatalf("restore did not put the user's settings back:\ngot:\n%s\nwant:\n%s", body, userSettings)
	}
	if ClaudeHooksInstalled(root) {
		t.Fatal("ClaudeHooksInstalled is true after restore")
	}
}
