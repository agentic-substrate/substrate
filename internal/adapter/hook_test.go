package adapter

import (
	"reflect"
	"strings"
	"testing"
)

// The harness owns the PostToolUse schema and it is not versioned, so the shim
// parses defensively: a body that is JSON but shaped differently must yield a
// thinner observation, never an error that fails the tool call.
func TestObservationFromToleratesSchemaVariation(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantTool   string
		wantFiles  []string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "tool_response object",
			body:       `{"tool_name":"Bash","tool_input":{"command":"ls"},"tool_response":{"stdout":"a\nb","stderr":""}}`,
			wantTool:   "Bash",
			wantStatus: 0,
			wantBody:   "a\nb",
		},
		{
			name:       "tool_result string (the shape the hook docs show)",
			body:       `{"tool_name":"Bash","tool_result":"done"}`,
			wantTool:   "Bash",
			wantStatus: 0,
			wantBody:   "done",
		},
		{
			name:       "interrupted stands in for a missing exit_code",
			body:       `{"tool_name":"Bash","tool_response":{"stderr":"boom","interrupted":true}}`,
			wantTool:   "Bash",
			wantStatus: 1,
			wantBody:   "boom",
		},
		{
			name:       "explicit exit_code wins",
			body:       `{"tool_name":"Bash","tool_response":{"exit_code":127,"stderr":"nope"}}`,
			wantTool:   "Bash",
			wantStatus: 127,
			wantBody:   "nope",
		},
		{
			name:      "nested file paths from a multi-edit input",
			body:      `{"tool_name":"MultiEdit","tool_input":{"edits":[{"file_path":"/a.go"},{"file_path":"/b.go"}]}}`,
			wantTool:  "MultiEdit",
			wantFiles: []string{"/a.go", "/b.go"},
		},
		{
			name:     "no tool name at all",
			body:     `{"cwd":"/w"}`,
			wantTool: "observation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := DecodePostToolUse(strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			obs := ObservationFrom(in)
			if obs.Tool != tc.wantTool {
				t.Errorf("tool = %q, want %q", obs.Tool, tc.wantTool)
			}
			if tc.wantFiles != nil && !reflect.DeepEqual(obs.Files, tc.wantFiles) {
				t.Errorf("files = %v, want %v", obs.Files, tc.wantFiles)
			}
			if obs.Status != tc.wantStatus {
				t.Errorf("status = %d, want %d", obs.Status, tc.wantStatus)
			}
			if tc.wantBody != "" && obs.Body != tc.wantBody {
				t.Errorf("body = %q, want %q", obs.Body, tc.wantBody)
			}
		})
	}
}

// A body that is not JSON at all is reported, so the caller can log it, but the
// caller still exits 0.
func TestDecodePostToolUseRejectsNonJSON(t *testing.T) {
	if _, err := DecodePostToolUse(strings.NewReader("not json")); err == nil {
		t.Fatal("decode accepted a non-JSON body")
	}
}

// RepoKey never guesses. A directory that is not a checkout has no repo key,
// and the caller must skip rather than fall back to a scope (#63, R18).
func TestRepoKeyRefusesNonCheckout(t *testing.T) {
	if key, ok := RepoKey(t.TempDir()); ok {
		t.Fatalf("RepoKey invented %q for a directory with no git remote", key)
	}
	if key, ok := RepoKey(""); ok {
		t.Fatalf("RepoKey invented %q for an empty cwd", key)
	}
}

// HookObservation refuses an observation with no repo rather than falling back
// to the configured scope: the hook path derives its scope from the checkout,
// and a silent fallback would file a repo's observations somewhere else.
func TestHookObservationRefusesWithoutRepo(t *testing.T) {
	home := t.TempDir()
	db := openDB(t, home+"/adapter.sqlite")
	cfg := testConfig(home, home+"/adapter.sqlite", "")
	if err := HookObservation(t.Context(), db, cfg, Observation{Tool: "Bash"}); err == nil {
		t.Fatal("HookObservation accepted an observation with no repo key")
	}
}
