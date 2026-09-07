package scope

import "testing"

func mustParse(t *testing.T, s string) Path {
	t.Helper()
	p, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return p
}

func TestParseRoundTrip(t *testing.T) {
	// Repo names contain "/", which must survive the wire form.
	const in = "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi"
	p := mustParse(t, in)
	if got := p.Leaf(); got.Kind != Repo || got.Name != "plotlens/api" {
		t.Fatalf("Leaf() = %+v, want repo plotlens/api", got)
	}
	if got := p.String(); got != in {
		t.Fatalf("String() = %q, want %q", got, in)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]string{
		"unrooted":         "project:plotlens",
		"decreasing":       "global:/repo:a%2Fb/project:plotlens",
		"repeated kind":    "global:/team:a/team:b",
		"user in chain":    "global:/user:jeremy",
		"unnamed non-root": "global:/project:",
		"missing kind":     "global:/plotlens",
		"unknown kind":     "global:/planet:earth",
		"skipped level":    "global:/project:plotlens",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(in); err == nil {
				t.Fatalf("Parse(%q) succeeded; want an error", in)
			}
		})
	}
}

// PRD SCOPE-1: an item at repo:plotlens/api is a candidate when compiling for a
// branch under that repo, and is not a candidate for a different repo.
func TestContainsIsTheApplicabilityRule(t *testing.T) {
	item := mustParse(t, "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi")
	branch := mustParse(t, "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi/branch:feature-x")
	other := mustParse(t, "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fweb")

	if !item.Contains(branch) {
		t.Error("repo-scoped item should be a candidate for a branch under that repo")
	}
	if item.Contains(other) {
		t.Error("repo-scoped item leaked into a sibling repo")
	}
	if branch.Contains(item) {
		t.Error("a branch-scoped item must not apply to the whole repo")
	}
}

// The compiler takes the FIRST active instruction per key, so Ancestors must
// yield the most specific scope first. PRD INST-2 depends on this order.
func TestAncestorsAreMostSpecificFirst(t *testing.T) {
	p := mustParse(t, "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi")
	got := p.Ancestors()
	if len(got) != 5 {
		t.Fatalf("len(Ancestors()) = %d, want 5", len(got))
	}
	want := []Kind{Repo, Project, Team, Org, Global}
	for i, k := range want {
		if got[i].Leaf().Kind != k {
			t.Errorf("Ancestors()[%d] leaf = %q, want %q", i, got[i].Leaf().Kind, k)
		}
	}
}

func TestUserIsNotInTheChain(t *testing.T) {
	if InChain(User) {
		t.Fatal("user must stay an orthogonal overlay root, not a chain level")
	}
	for _, k := range chain {
		if !InChain(k) {
			t.Errorf("%q should be in the chain", k)
		}
	}
}

func TestDepthOrdersSpecificity(t *testing.T) {
	if Depth(Global) >= Depth(Session) {
		t.Fatal("global must be less specific than session")
	}
}
