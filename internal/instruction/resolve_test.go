package instruction

import (
	"bytes"
	"embed"
	"sort"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/preference"
	"github.com/agentic-substrate/substrate/internal/scope"
)

//go:embed testdata/*.golden
var goldens embed.FS

func mustParse(t *testing.T, s string) scope.Path {
	t.Helper()
	p, err := scope.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return p
}

func ancestorKeys(p scope.Path) []string {
	anc := p.Ancestors()
	out := make([]string, len(anc))
	for i, a := range anc {
		out[i] = a.String()
	}
	return out
}

func reverseKeys(in []string) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func renderInstructions(recs []Record) string {
	lines := make([]string, 0, len(recs))
	for _, r := range recs {
		lines = append(lines, r.Key+"="+r.Body)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := goldens.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("golden %s: %v", name, err)
	}
	return string(b)
}

func TestProjectPythonVersionWins(t *testing.T) {
	// INST-2: 3.13 at global and 3.12 at project → 3.12 only.
	p := mustParse(t, "global:/org:acme/team:core/project:plotlens")
	recs := []Record{
		{Scope: "global:", Key: "python.version", Body: "3.13"},
		{Scope: p.String(), Key: "python.version", Body: "3.12"},
	}
	got := renderInstructions(Walk(ancestorKeys(p), recs))
	want := loadGolden(t, "python-version.golden")
	if got != want {
		t.Fatalf("effective set:\n got %q\nwant %q", got, want)
	}
}

func TestDistinctKeysAccumulate(t *testing.T) {
	p := mustParse(t, "global:/org:acme/team:core/project:plotlens")
	recs := []Record{
		{Scope: "global:", Key: "python.version", Body: "3.13"},
		{Scope: "global:/org:acme", Key: "style.indent", Body: "spaces"},
		{Scope: p.String(), Key: "python.version", Body: "3.12"},
		{Scope: p.String(), Key: "go.module", Body: "github.com/acme/plotlens"},
	}
	got := renderInstructions(Walk(ancestorKeys(p), recs))
	want := loadGolden(t, "accumulate.golden")
	if got != want {
		t.Fatalf("effective set:\n got %q\nwant %q", got, want)
	}
}

func TestDeepChainFirstActiveWins(t *testing.T) {
	p := mustParse(t, "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi/branch:main")
	recs := []Record{
		{Scope: "global:", Key: "python.version", Body: "3.13"},
		{Scope: "global:/org:acme", Key: "lint", Body: "flake8"},
		{Scope: "global:/org:acme/team:core/project:plotlens", Key: "python.version", Body: "3.12"},
		{Scope: "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi", Key: "lint", Body: "ruff"},
		{Scope: p.String(), Key: "timeout", Body: "30s"},
	}
	got := renderInstructions(Walk(ancestorKeys(p), recs))
	want := loadGolden(t, "deep-chain.golden")
	if got != want {
		t.Fatalf("effective set:\n got %q\nwant %q", got, want)
	}
}

func TestAncestorOrderIsLoadBearing(t *testing.T) {
	p := mustParse(t, "global:/org:acme/team:core/project:plotlens")
	recs := []Record{
		{Scope: "global:", Key: "python.version", Body: "3.13"},
		{Scope: p.String(), Key: "python.version", Body: "3.12"},
	}
	want := loadGolden(t, "python-version.golden")
	if got := renderInstructions(Walk(ancestorKeys(p), recs)); got != want {
		t.Fatalf("most-specific-first:\n got %q\nwant %q", got, want)
	}
	reversed := reverseKeys(ancestorKeys(p))
	gotRev := renderInstructions(Walk(reversed, recs))
	if gotRev == want {
		t.Fatal("reversing Ancestors() still matched the golden; first-active-wins is not load-bearing on most-specific-first")
	}
	if !strings.Contains(gotRev, "python.version=3.13") {
		t.Fatalf("reversed walk should surface the global value, got %q", gotRev)
	}
}

func renderEffective(ins []Record, prefs []preference.Record, notes []preference.Suppression) string {
	var b bytes.Buffer
	b.WriteString("instructions:\n")
	b.WriteString(renderInstructions(ins))
	prefLines := make([]string, 0, len(prefs))
	for _, p := range prefs {
		prefLines = append(prefLines, p.Key+"="+p.Body)
	}
	sort.Strings(prefLines)
	b.WriteString("preferences:\n")
	if len(prefLines) > 0 {
		b.WriteString(strings.Join(prefLines, "\n"))
		b.WriteByte('\n')
	}
	noteLines := make([]string, 0, len(notes))
	for _, n := range notes {
		noteLines = append(noteLines, n.Line())
	}
	sort.Strings(noteLines)
	b.WriteString("suppressed:\n")
	if len(noteLines) > 0 {
		b.WriteString(strings.Join(noteLines, "\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestUserPreferenceSuppressedByInstruction(t *testing.T) {
	// INST-3: user indent=tabs vs project indent=spaces → spaces + one suppression line.
	p := mustParse(t, "global:/org:acme/team:core/project:plotlens")
	ins := Walk(ancestorKeys(p), []Record{
		{Scope: p.String(), Key: "indent", Body: "spaces"},
	})
	prefs, notes := preference.Walk([]preference.Record{
		{Scope: "user", Key: "indent", Body: "tabs"},
	}, Keys(ins))
	got := renderEffective(ins, prefs, notes)
	want := loadGolden(t, "indent-suppressed.golden")
	if got != want {
		t.Fatalf("effective set:\n got %q\nwant %q", got, want)
	}
}

func TestPreferenceEmittedWhenKeyUnclaimed(t *testing.T) {
	p := mustParse(t, "global:/org:acme/team:core/project:plotlens")
	ins := Walk(ancestorKeys(p), []Record{
		{Scope: p.String(), Key: "python.version", Body: "3.12"},
	})
	ordered := []preference.Record{
		{Scope: "user", Key: "editor", Body: "vim"},
		{Scope: "team", Key: "editor", Body: "emacs"},
		{Scope: "org", Key: "color", Body: "auto"},
	}
	prefs, notes := preference.Walk(ordered, Keys(ins))
	got := renderEffective(ins, prefs, notes)
	want := loadGolden(t, "unclaimed-prefs.golden")
	if got != want {
		t.Fatalf("effective set:\n got %q\nwant %q", got, want)
	}
}
