package importer

import "testing"

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

// keyOf matches the body exactly. plan.Blocks is sorted by content hash, so a
// substring match would return whichever near-duplicate sorted first — a decoy
// bullet silently answering for the one under test.
func keyOf(t *testing.T, p *Plan, body string) string {
	t.Helper()
	var out string
	found := 0
	for _, b := range p.Blocks {
		if b.Body == body {
			found++
			out = rowKey(b.Kind, b.Rel, b.Heading, b.Ordinal)
		}
	}
	if found != 1 {
		t.Fatalf("want exactly 1 block with body %q, got %d of %d blocks", body, found, len(p.Blocks))
	}
	return out
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
	after := keyOf(t, planFor(t, "CLAUDE.md", "## Gotchas\n\n- first rule\n- second rule, reworded\n"), "second rule, reworded")
	if before != after {
		t.Fatalf("rewording a rule minted a new key: %q -> %q", before, after)
	}
}

// ConflictSide.Ordinal is what the conflict-review path feeds to rowKey. Without
// it every side of a slot collapses onto ordinal 0, so two proposed siblings
// claim one key.
func TestConflictSideProposedRowsHaveDistinctKeys(t *testing.T) {
	p, err := BuildPlan([]Inventory{
		{Hostname: "host-a", Files: []File{{Rel: "CLAUDE.md", Path: "/a/CLAUDE.md", Content: "## Gotchas\n\n- alpha rule\n- beta rule\n"}}},
		{Hostname: "host-b", Files: []File{{Rel: "CLAUDE.md", Path: "/b/CLAUDE.md", Content: "## Gotchas\n\n- alpha rule changed\n- beta rule changed\n"}}},
	}, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(p.Conflicts) != 1 {
		t.Fatalf("want 1 conflict slot, got %d: %+v", len(p.Conflicts), p.Conflicts)
	}
	c := p.Conflicts[0]
	if len(c.Pair) < 4 {
		t.Fatalf("want at least 4 conflict sides, got %d", len(c.Pair))
	}
	seen := map[string]bool{}
	for _, side := range c.Pair {
		key := rowKey("instruction", "CLAUDE.md", "Gotchas", side.Ordinal)
		if seen[key] {
			t.Fatalf("conflict side ordinal produced a duplicate row key %q: %+v", key, c.Pair)
		}
		seen[key] = true
	}
}
