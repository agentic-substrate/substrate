package cutover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ClaudeSettingsRel is the user-owned Claude Code settings file the adapter
// merges its hook entry into. Unlike every other path in this package it is
// not a file Substrate generates: it is edited in place, its unrelated keys
// are preserved, and it is copied — never renamed — to *.pre-substrate so the
// user's harness keeps working while Substrate is installed.
const ClaudeSettingsRel = ".claude/settings.json"

// HookSubcommand is the adapter subcommand the installed hook invokes.
const HookSubcommand = "hook posttooluse"

// HookTimeoutSeconds is the harness-side timeout on the entry, matching
// adapter.HookDeadline. A shim that overran it would stall the tool call.
const HookTimeoutSeconds = 2

// HookMatcher matches every tool. Capture is not tool-specific.
const HookMatcher = "*"

// HookEvents is the single source of truth for which Claude Code hook events
// the installer actually installs. render's claim about hooks is asserted
// against this list, so a rendered CLAUDE.md cannot name a hook nobody
// installs (issue #61 AC4). SessionStart is deliberately absent: it needs a
// client path to context.get and an offline instruction-pack fallback, neither
// of which exists, and is Phase 2 (CAP-1a, second half).
func HookEvents() []string { return []string{"PostToolUse"} }

// ClaudeSettingsPath is <home>/.claude/settings.json.
func ClaudeSettingsPath(home string) string {
	return filepath.Join(home, filepath.FromSlash(ClaudeSettingsRel))
}

// hookCommand is the command string written into settings.json. It doubles as
// the idempotency marker: an entry containing HookSubcommand is ours.
func hookCommand(binary string) string {
	if strings.TrimSpace(binary) == "" {
		binary = "substrate-adapter"
	}
	return binary + " " + HookSubcommand
}

func isOurs(cmd string) bool { return strings.Contains(cmd, HookSubcommand) }

// InstallClaudeHooks merges the PostToolUse entry into <home>/.claude/settings.json.
// It is idempotent: an entry already naming the shim is left exactly as it is,
// so installing twice does not duplicate the hook. Before the first
// modification the existing file is copied to *.pre-substrate; an existing
// backup is never overwritten, because that is the one copy of the user's
// pre-Substrate settings.
func InstallClaudeHooks(home, binary string) error {
	if home == "" {
		return fmt.Errorf("cutover: hook install requires Home")
	}
	path := ClaudeSettingsPath(home)
	doc, existed, err := readSettings(path)
	if err != nil {
		return err
	}
	changed, err := mergeHookEntry(doc, hookCommand(binary))
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if existed {
		if err := backupOnce(path); err != nil {
			return err
		}
	}
	return writeSettings(path, doc)
}

// RemoveClaudeHooks undoes InstallClaudeHooks. If the *.pre-substrate copy is
// present the user's original file is put back verbatim; otherwise the entry is
// removed surgically and a file that only ever held our hook is deleted. This
// is the half that must not be skipped: a settings.json left pointing at an
// uninstalled binary makes every tool call in every session pay a failed exec.
func RemoveClaudeHooks(home string) error {
	if home == "" {
		return nil
	}
	path := ClaudeSettingsPath(home)
	backup := path + BackupSuffix
	if _, err := os.Lstat(backup); err == nil {
		body, rerr := os.ReadFile(backup) //nolint:gosec // path is <home>/.claude/settings.json.pre-substrate
		if rerr != nil {
			return fmt.Errorf("cutover: read %s: %w", backup, rerr)
		}
		if werr := atomicWrite(path, body, 0o600); werr != nil {
			return werr
		}
		if rmErr := os.Remove(backup); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("cutover: remove %s: %w", backup, rmErr)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cutover: stat %s: %w", backup, err)
	}

	doc, existed, err := readSettings(path)
	if err != nil || !existed {
		return err
	}
	changed, err := dropHookEntry(doc)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if len(doc) == 0 {
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("cutover: remove %s: %w", path, rmErr)
		}
		return nil
	}
	return writeSettings(path, doc)
}

// ClaudeHooksInstalled reports whether settings.json currently names the shim.
func ClaudeHooksInstalled(home string) bool {
	doc, existed, err := readSettings(ClaudeSettingsPath(home))
	if err != nil || !existed {
		return false
	}
	groups, err := hookGroups(doc)
	if err != nil {
		return false
	}
	for _, g := range groups {
		for _, e := range g.Hooks {
			if isOurs(e.Command) {
				return true
			}
		}
	}
	return false
}

// settings is the top level of settings.json held as raw values so every key
// Substrate does not touch survives the round trip byte-for-byte. Go maps have
// no order, so keys come back sorted; the values themselves are unchanged.
type settings map[string]json.RawMessage

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type hookGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

func readSettings(path string) (settings, bool, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is <home>/.claude/settings.json
	if os.IsNotExist(err) {
		return settings{}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cutover: read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return settings{}, true, nil
	}
	var doc settings
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Refusing is the only safe move: rewriting a file we cannot parse
		// would destroy settings the user owns.
		return nil, true, fmt.Errorf("cutover: %s is not a JSON object; refusing to edit it: %w", path, err)
	}
	if doc == nil {
		doc = settings{}
	}
	return doc, true, nil
}

func writeSettings(path string, doc settings) error {
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range keys {
		key, err := json.Marshal(k)
		if err != nil {
			return err
		}
		var val strings.Builder
		if err := indentInto(&val, doc[k]); err != nil {
			return err
		}
		fmt.Fprintf(&b, "  %s: %s", key, val.String())
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return atomicWrite(path, []byte(b.String()), 0o600)
}

func indentInto(b *strings.Builder, raw json.RawMessage) error {
	var buf strings.Builder
	if err := jsonIndent(&buf, raw); err != nil {
		return err
	}
	b.WriteString(strings.ReplaceAll(buf.String(), "\n", "\n  "))
	return nil
}

func hookGroups(doc settings) ([]hookGroup, error) {
	hooksRaw, ok := doc["hooks"]
	if !ok || len(hooksRaw) == 0 {
		return nil, nil
	}
	var byEvent map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &byEvent); err != nil {
		return nil, fmt.Errorf("cutover: settings.json hooks is not an object: %w", err)
	}
	var out []hookGroup
	for _, ev := range HookEvents() {
		raw, ok := byEvent[ev]
		if !ok {
			continue
		}
		var groups []hookGroup
		if err := json.Unmarshal(raw, &groups); err != nil {
			return nil, fmt.Errorf("cutover: settings.json hooks.%s: %w", ev, err)
		}
		out = append(out, groups...)
	}
	return out, nil
}

func mergeHookEntry(doc settings, command string) (bool, error) {
	byEvent, err := hooksObject(doc)
	if err != nil {
		return false, err
	}
	changed := false
	for _, ev := range HookEvents() {
		groups, err := eventGroups(byEvent, ev)
		if err != nil {
			return false, err
		}
		found := false
		for _, g := range groups {
			for _, e := range g.Hooks {
				if isOurs(e.Command) {
					found = true
				}
			}
		}
		if found {
			continue
		}
		groups = append(groups, hookGroup{
			Matcher: HookMatcher,
			Hooks:   []hookEntry{{Type: "command", Command: command, Timeout: HookTimeoutSeconds}},
		})
		raw, err := json.Marshal(groups)
		if err != nil {
			return false, err
		}
		byEvent[ev] = raw
		changed = true
	}
	if !changed {
		return false, nil
	}
	raw, err := json.Marshal(byEvent)
	if err != nil {
		return false, err
	}
	doc["hooks"] = raw
	return true, nil
}

func dropHookEntry(doc settings) (bool, error) {
	byEvent, err := hooksObject(doc)
	if err != nil {
		return false, err
	}
	changed := false
	for ev, raw := range byEvent {
		var groups []hookGroup
		if err := json.Unmarshal(raw, &groups); err != nil {
			// An event shaped differently is not ours; leave it alone.
			continue
		}
		kept := make([]hookGroup, 0, len(groups))
		for _, g := range groups {
			entries := make([]hookEntry, 0, len(g.Hooks))
			for _, e := range g.Hooks {
				if isOurs(e.Command) {
					changed = true
					continue
				}
				entries = append(entries, e)
			}
			if len(entries) == 0 {
				continue
			}
			g.Hooks = entries
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(byEvent, ev)
			continue
		}
		next, err := json.Marshal(kept)
		if err != nil {
			return false, err
		}
		byEvent[ev] = next
	}
	if !changed {
		return false, nil
	}
	if len(byEvent) == 0 {
		delete(doc, "hooks")
		return true, nil
	}
	raw, err := json.Marshal(byEvent)
	if err != nil {
		return false, err
	}
	doc["hooks"] = raw
	return true, nil
}

func hooksObject(doc settings) (map[string]json.RawMessage, error) {
	byEvent := map[string]json.RawMessage{}
	if raw, ok := doc["hooks"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &byEvent); err != nil {
			return nil, fmt.Errorf("cutover: settings.json hooks is not an object: %w", err)
		}
	}
	return byEvent, nil
}

func eventGroups(byEvent map[string]json.RawMessage, ev string) ([]hookGroup, error) {
	raw, ok := byEvent[ev]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var groups []hookGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("cutover: settings.json hooks.%s: %w", ev, err)
	}
	return groups, nil
}

// backupOnce copies path to path+BackupSuffix. It copies rather than renames
// because settings.json must stay live, and it never overwrites an existing
// backup — that is the one copy of an earlier cutover (Gotcha 6).
func backupOnce(path string) error {
	backup := path + BackupSuffix
	if _, err := os.Lstat(backup); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cutover: stat %s: %w", backup, err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is <home>/.claude/settings.json
	if err != nil {
		return fmt.Errorf("cutover: read %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("cutover: stat %s: %w", path, err)
	}
	return atomicWrite(backup, body, info.Mode().Perm())
}

func jsonIndent(b *strings.Builder, raw json.RawMessage) error {
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	b.Write(out)
	return nil
}
