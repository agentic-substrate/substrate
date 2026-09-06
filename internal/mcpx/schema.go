package mcpx

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolNamePattern is the SDK-permitted set. Dots are required by the EDD;
// falling back to underscores is a one-line change here (EDD R12).
var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

func checkToolNames(tools []*mcp.Tool) error {
	for _, t := range tools {
		if t == nil || t.Name == "" {
			return fmt.Errorf("tool with empty name")
		}
		if !toolNamePattern.MatchString(t.Name) {
			return fmt.Errorf("schema for tool %q: name is outside [a-zA-Z0-9_.-]", t.Name)
		}
		if !strings.Contains(t.Name, ".") {
			return fmt.Errorf("schema for tool %q: names use dots (memory.write)", t.Name)
		}
	}
	return nil
}

func toolSchemas(tools []*mcp.Tool) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		b, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("schema for tool %q: %w", t.Name, err)
		}
		canon, err := canonicalJSON(b)
		if err != nil {
			return nil, fmt.Errorf("schema for tool %q: %w", t.Name, err)
		}
		out[t.Name] = canon
	}
	return out, nil
}

func diffToolSchemas(got, want map[string]json.RawMessage) error {
	seen := make(map[string]struct{}, len(got)+len(want))
	for name := range got {
		seen[name] = struct{}{}
	}
	for name := range want {
		seen[name] = struct{}{}
	}
	for name := range seen {
		g, gok := got[name]
		w, wok := want[name]
		switch {
		case !gok:
			return fmt.Errorf("schema for tool %q: missing from server", name)
		case !wok:
			return fmt.Errorf("schema for tool %q: missing from snapshot", name)
		case string(g) != string(w):
			return fmt.Errorf("schema for tool %q changed", name)
		}
	}
	return nil
}

func canonicalJSON(b []byte) (json.RawMessage, error) {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}
