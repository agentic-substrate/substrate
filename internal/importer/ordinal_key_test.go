package importer

import (
	"strings"
	"testing"
)

// #80: import keys embedded a content hash, so editing a bullet minted a new
// instruction and nothing could ever be superseded. Keys now carry the bullet's
// ordinal within its (rel, heading) slot: stable under body edits, distinct
// between siblings.

func planFor(t *testing.T, rel, content string) *Plan {
	t.Helper()
	p, err := BuildPlan([]Inventory{{
		Hostname: "wsl",
		Files:    []File{{Rel: rel, Path: "/home/u/" + rel, Content: content}},
	}}, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return p
}

func keyOf(t *testing.T, p *Plan, body string) string {
	t.Helper()
	for _, b := range p.Blocks {
		if strings.Contains(b.Body, body) {
			return rowKey(b.Kind, b.Rel, b.Heading, b.Ordinal)
		}
	}
	t.Fatalf("no block with body %q in %d blocks", body, len(p.Blocks))
	return ""
}

func TestRowKeySiblingsDiffer(t *testing.T) {
	p := planFor(t, "CLAUDE.md", "## Gotchas\n\n- first rule\n- second rule\n")
	a, b := keyOf(t, p, "first rule"), keyOf(t, p, "second rule")
	if a == b {
		t.Fatalf("two bullets under one heading share key %q", a)
	}
}

func TestRowKeyStableUnderBodyEdit(t *testing.T) {
	before := keyOf(t, planFor(t, "CLAUDE.md", "## Gotchas\n\n- first rule\n- second rule\n"), "second rule")
	after := keyOf(t, planFor(t, "CLAUDE.md", "## Gotchas\n\n- first rule\n- second rule, reworded\n"), "second rule")
	if before != after {
		t.Fatalf("rewording a rule minted a new key: %q -> %q", before, after)
	}
}
