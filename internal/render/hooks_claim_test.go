package render_test

import (
	"regexp"
	"testing"

	"github.com/agentic-substrate/substrate/internal/cutover"
	"github.com/agentic-substrate/substrate/internal/render"
)

// knownHookEvents is the Claude Code hook-event vocabulary. It exists only so
// the assertion below can spot a name in the rendered block; the *installed*
// set is never listed here, it is read from the installer.
var knownHookEvents = []string{
	"SessionStart",
	"SessionEnd",
	"PreToolUse",
	"PostToolUse",
	"UserPromptSubmit",
	"Notification",
	"PreCompact",
	"Stop",
	"SubagentStop",
}

// TestClaudeBlockNamesOnlyInstalledHooks is the assertion issue #61 AC4 asks
// for: the rendered CLAUDE.md must not claim a hook the installer does not
// install, and must not stay silent about one it does. Both sides are derived
// — the claim from render.ClaudeBlock, the truth from cutover.HookEvents() —
// so adding a hook to the installer or a name to the sentence without the
// other turns this red.
func TestClaudeBlockNamesOnlyInstalledHooks(t *testing.T) {
	installed := map[string]bool{}
	for _, ev := range cutover.HookEvents() {
		installed[ev] = true
	}
	if len(installed) == 0 {
		t.Fatal("cutover.HookEvents() is empty; this test would prove nothing")
	}

	claimed := map[string]bool{}
	for _, ev := range knownHookEvents {
		if namesHook(render.ClaudeBlock, ev) {
			claimed[ev] = true
		}
	}

	for ev := range claimed {
		if !installed[ev] {
			t.Errorf("ClaudeBlock claims the %s hook, but the installer does not install it (installed: %v)", ev, cutover.HookEvents())
		}
	}
	for ev := range installed {
		if !claimed[ev] {
			t.Errorf("the installer installs the %s hook, but ClaudeBlock does not mention it", ev)
		}
	}
}

// namesHook reports whether text names this hook event as a whole word. A
// plain substring match would read "SubagentStop" as a claim about "Stop".
func namesHook(text, event string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(event) + `\b`).MatchString(text)
}

// The vocabulary contains names that are substrings of each other, so the
// matcher above must be word-boundary aware or the test it backs is unsound.
func TestKnownHookEventsAreDistinguishable(t *testing.T) {
	for _, a := range knownHookEvents {
		for _, b := range knownHookEvents {
			if a == b {
				continue
			}
			if namesHook(a, b) {
				t.Fatalf("event name %q reads as a claim about %q; the matcher cannot tell them apart", a, b)
			}
		}
	}
}
