package preference

import (
	"strings"
	"testing"
)

func TestUserBeatsTeamBeatsOrg(t *testing.T) {
	got, notes := Walk([]Record{
		{Scope: "user", Key: "indent", Body: "tabs"},
		{Scope: "team", Key: "indent", Body: "spaces"},
		{Scope: "org", Key: "indent", Body: "mixed"},
		{Scope: "org", Key: "color", Body: "auto"},
	}, nil)
	if len(notes) != 0 {
		t.Fatalf("no instruction claimed these keys, got suppressions %v", notes)
	}
	by := map[string]string{}
	for _, r := range got {
		by[r.Key] = r.Body
	}
	if by["indent"] != "tabs" {
		t.Fatalf("indent=%q, want tabs (user > team > org)", by["indent"])
	}
	if by["color"] != "auto" {
		t.Fatalf("color=%q, want auto", by["color"])
	}
}

func TestInstructionClaimSuppressesOnce(t *testing.T) {
	got, notes := Walk([]Record{
		{Scope: "user", Key: "indent", Body: "tabs"},
		{Scope: "team", Key: "indent", Body: "spaces"},
	}, map[string]struct{}{"indent": {}})
	if len(got) != 0 {
		t.Fatalf("claimed key leaked into preferences: %v", got)
	}
	if len(notes) != 1 {
		t.Fatalf("want exactly one suppression line, got %d: %v", len(notes), notes)
	}
	if !strings.Contains(notes[0].Line(), "indent=tabs") {
		t.Fatalf("suppression line %q does not name the user preference", notes[0].Line())
	}
	if strings.Count(notes[0].Line(), "\n") != 0 {
		t.Fatalf("suppression must be one line, got %q", notes[0].Line())
	}
}
