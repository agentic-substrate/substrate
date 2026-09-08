package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/adapter"
)

func statePath(home string) string {
	return filepath.Join(home, ".substrate", "adapter.sqlite")
}

func outboxDepth(t *testing.T, home string) int {
	t.Helper()
	db, err := adapter.OpenHook(statePath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	n, err := db.OutboxDepth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func outboxPayloads(t *testing.T, home string) []map[string]any {
	t.Helper()
	db, err := adapter.OpenHook(statePath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	raws, err := db.OutboxPayloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := make([]map[string]any, 0, len(raws))
	for _, raw := range raws {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // argv is test literals
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func checkout(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "remote", "add", "origin", remote)
	return dir
}

func hookJSON(t *testing.T, cwd string) string {
	t.Helper()
	body := map[string]any{
		"session_id":      "s1",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             cwd,
		"hook_event_name": "PostToolUse",
		"tool_name":       "Edit",
		"tool_input":      map[string]any{"file_path": "/repo/a.go", "old_string": "x"},
		"tool_response":   map[string]any{"stdout": "ok", "stderr": ""},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A cwd whose git remote the adapter cannot parse is skipped and logged.
// Nothing is enqueued, and in particular nothing is filed at `global:` — a
// scope no agent token may write (#63, R18). A guess here would be worse than
// no capture at all.
func TestPostToolUseSkipsUnboundCwd(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir() // not a git checkout: no remote to parse
	var stderr strings.Builder

	code := hookCmd([]string{"posttooluse", "-home", home},
		strings.NewReader(hookJSON(t, cwd)), &stderr)

	if code != 0 {
		t.Fatalf("hook exited %d; a capture failure must never fail the tool call", code)
	}
	if n := outboxDepth(t, home); n != 0 {
		t.Fatalf("enqueued %d observations for an unbound cwd; want 0", n)
	}
	if !strings.Contains(stderr.String(), "skipping observation") {
		t.Fatalf("the skip was not logged: %q", stderr.String())
	}
	for _, p := range outboxPayloads(t, home) {
		if p["scope"] == "global:" {
			t.Fatal("an observation was filed at global:")
		}
	}
}

// A remote that parses yields a repo key on the wire and no scope: the server
// owns the repo-key-to-chain binding (R18), so the shim never names a chain.
func TestPostToolUseEnqueuesRepoKeyedObservation(t *testing.T) {
	home := t.TempDir()
	cwd := checkout(t, "git@github.com:unrounded/api.git")
	var stderr strings.Builder

	if code := hookCmd([]string{"posttooluse", "-home", home},
		strings.NewReader(hookJSON(t, cwd)), &stderr); code != 0 {
		t.Fatalf("hook exited %d: %s", code, stderr.String())
	}
	payloads := outboxPayloads(t, home)
	if len(payloads) != 1 {
		t.Fatalf("enqueued %d observations, want 1 (stderr: %s)", len(payloads), stderr.String())
	}
	p := payloads[0]
	if got := p["repo"]; got != "github.com/unrounded/api" {
		t.Fatalf("repo key on the wire is %v, want github.com/unrounded/api", got)
	}
	if _, ok := p["scope"]; ok {
		t.Fatalf("the shim named a scope as well as a repo: %v", p["scope"])
	}
	if p["title"] != "Edit" {
		t.Fatalf("title is %v, want Edit", p["title"])
	}
	ids, _ := p["identifiers"].([]any)
	if len(ids) != 1 || ids[0] != "/repo/a.go" {
		t.Fatalf("identifiers are %v, want the edited file", p["identifiers"])
	}
}

// Every failure mode exits 0. Anything else fails the user's tool call.
func TestPostToolUseAlwaysExitsZero(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"no event", nil, ""},
		{"unknown event", []string{"sessionstart"}, ""},
		{"no home", []string{"posttooluse"}, "{}"},
		{"garbage stdin", []string{"posttooluse", "-home", t.TempDir()}, "not json"},
		{"bad flag", []string{"posttooluse", "-nope"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr strings.Builder
			if code := hookCmd(tc.args, strings.NewReader(tc.stdin), &stderr); code != 0 {
				t.Fatalf("exit %d for %q", code, tc.name)
			}
		})
	}
}
