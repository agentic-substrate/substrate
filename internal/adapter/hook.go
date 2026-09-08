package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// MaxHookInputBytes bounds the JSON the harness pipes in. A tool response can
// be megabytes; the observation body is clipped to MaxObservationBytes anyway,
// so reading more than this only costs the tool call time it does not have.
const MaxHookInputBytes = 1 << 20

// PostToolUseInput is the PostToolUse hook JSON Claude Code writes to the
// shim's stdin. Only the fields capture needs are named, and every one of them
// is optional: the harness owns this schema, so an unknown or renamed field
// must degrade to a thinner observation rather than fail the tool call.
//
// The result field is read from both `tool_response` (what shipped hooks
// actually read) and `tool_result` (what the hook-authoring docs show). They
// have never been observed together; whichever is present is used.
type PostToolUseInput struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	CWD           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	ToolResult    json.RawMessage `json:"tool_result"`
}

// DecodePostToolUse parses hook JSON from r. A body that is not JSON at all is
// an error the caller logs and drops; a body that is JSON but shaped
// differently yields whatever fields did match.
func DecodePostToolUse(r io.Reader) (PostToolUseInput, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxHookInputBytes))
	if err != nil {
		return PostToolUseInput{}, fmt.Errorf("adapter: read hook input: %w", err)
	}
	var in PostToolUseInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return PostToolUseInput{}, fmt.Errorf("adapter: decode hook input: %w", err)
	}
	return in, nil
}

// ObservationFrom maps hook input to the outbox observation shape. Repo is not
// set here: it comes from the checkout's remote, which is a filesystem read the
// caller does.
func ObservationFrom(in PostToolUseInput) Observation {
	obs := Observation{Tool: in.ToolName}
	if obs.Tool == "" {
		obs.Tool = "observation"
	}
	obs.Files = observationFiles(in.ToolInput)
	result := in.ToolResponse
	if len(result) == 0 {
		result = in.ToolResult
	}
	obs.Status, obs.Body = observationResult(result)
	return obs
}

// observationResult extracts an exit status and a compact body. Claude Code's
// Bash tool response carries no exit_code, so a non-zero status is inferred
// from the fields that do exist (`interrupted`, `is_error`, `error`) and any
// other shape yields status 0 with the response rendered as the body.
func observationResult(raw json.RawMessage) (int, string) {
	if len(raw) == 0 {
		return 0, ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return 0, s
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0, strings.TrimSpace(string(raw))
	}
	status := 0
	if code, ok := m["exit_code"].(float64); ok {
		status = int(code)
	} else if truthy(m["interrupted"]) || truthy(m["is_error"]) || truthy(m["error"]) {
		status = 1
	}
	var parts []string
	for _, key := range []string{"stdout", "stderr", "error"} {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			parts = append(parts, strings.TrimSpace(v))
		}
	}
	if len(parts) == 0 {
		return status, strings.TrimSpace(string(raw))
	}
	return status, strings.Join(parts, "\n")
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.TrimSpace(t) != ""
	default:
		return false
	}
}

// observationFiles pulls file paths out of a tool input of unknown shape. It
// walks the object rather than matching one tool's schema so a new tool with a
// `file_path` still records the file it touched.
func observationFiles(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	collectPaths(v, seen)
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

var pathKeys = map[string]struct{}{
	"file_path":     {},
	"path":          {},
	"notebook_path": {},
	"filePath":      {},
}

func collectPaths(v any, out map[string]struct{}) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if _, ok := pathKeys[k]; ok {
				if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
					out[s] = struct{}{}
					continue
				}
			}
			collectPaths(val, out)
		}
	case []any:
		for _, item := range t {
			collectPaths(item, out)
		}
	}
}

// RepoKey is the repo scope key for the checkout dir belongs to: the checkout's
// normalized `origin` remote. It reports false for a directory that is not a
// checkout, has no origin, or whose remote does not parse. The caller must then
// skip the observation. A repo key is never derived from the filesystem path
// (#55) and a remote is never turned into a scope here (R18): a directory the
// server has not bound has no scope, and `global:` is not a fallback (#63).
func RepoKey(dir string) (string, bool) {
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	raw, err := gitOutput(dir, "remote", "get-url", "origin")
	if err != nil {
		return "", false
	}
	id, ok := ParseRemote(NormalizeRemote(strings.TrimSpace(raw)))
	if !ok {
		return "", false
	}
	return id.Key, true
}

// ErrNoRepo is returned when a hook's cwd cannot be resolved to a bound repo
// key. The caller logs it and exits 0: capture is best-effort, and filing the
// observation somewhere else would be worse than not filing it.
var ErrNoRepo = fmt.Errorf("adapter: hook cwd has no parseable git remote; skipping observation")

// HookObservation enqueues obs against the repo key on the hook path, then
// best-effort drains inside the hook budget. The payload carries `repo`, not a
// scope: the server owns the repo-key-to-chain binding (R18), so the shim can
// neither invent a chain nor be handed one.
func HookObservation(ctx context.Context, db *DB, cfg Config, obs Observation) error {
	if obs.Repo == "" {
		return ErrNoRepo
	}
	payload, err := observationPayload(cfg, obs)
	if err != nil {
		return err
	}
	return HookWrite(ctx, db, cfg, payload)
}
