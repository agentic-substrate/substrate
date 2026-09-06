package render

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/instruction"
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

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := goldens.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("golden %s: %v", name, err)
	}
	return string(b)
}

func fixtureConfig(t *testing.T) EffectiveConfig {
	t.Helper()
	p := mustParse(t, "global:/org:acme/team:core/project:plotlens")
	ins := instruction.Walk(ancestorKeys(p), []instruction.Record{
		{Scope: "global:", Kind: "constraint", Key: "python.version", Body: "3.13"},
		{Scope: p.String(), Kind: "constraint", Key: "python.version", Body: "3.12"},
		{Scope: p.String(), Kind: "rule", Key: "python.formatter", Body: "ruff"},
		{Scope: "global:", Kind: "rule", Key: "deploy.policy", Body: "protected"},
		{Scope: p.String(), Kind: "rule", Key: "indent", Body: "spaces"},
		{Scope: p.String(), Kind: "convention", Key: "go.module", Body: "github.com/acme/plotlens"},
		{Scope: p.String(), Kind: "convention", Key: "errors", Body: "wrap with %w"},
	})
	prefs, notes := preference.Walk([]preference.Record{
		{Scope: "user", Key: "indent", Body: "tabs"},
		{Scope: "team", Key: "indent", Body: "spaces"},
		{Scope: "user", Key: "editor", Body: "vim"},
		{Scope: "org", Key: "color", Body: "auto"},
	}, instruction.Keys(ins))
	return EffectiveConfig{
		Instructions: ins,
		Preferences:  prefs,
		Suppressed:   notes,
		Skills:       []Skill{{Name: "team/plotlens/validation", Description: "regression checks"}},
		GeneratedAt:  "2026-01-02T15:04:05Z",
	}
}

func mustRender(t *testing.T, target Target, cfg EffectiveConfig) string {
	t.Helper()
	got, err := Render(target, cfg)
	if err != nil {
		t.Fatalf("Render(%s): %v", target, err)
	}
	return got
}

func TestGoldenPerTarget(t *testing.T) {
	cfg := fixtureConfig(t)
	for _, tc := range []struct {
		target Target
		file   string
	}{
		{TargetAgents, "agents.golden"},
		{TargetClaude, "claude.golden"},
		{TargetCursor, "cursor.golden"},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			got := mustRender(t, tc.target, cfg)
			again := mustRender(t, tc.target, cfg)
			if got != again {
				t.Fatalf("identical inputs produced different bytes")
			}
			want := loadGolden(t, tc.file)
			if got != want {
				t.Fatalf("%s:\n got %q\nwant %q", tc.file, got, want)
			}
		})
	}
}

func TestRenderTwiceByteIdentical(t *testing.T) {
	cfg := fixtureConfig(t)
	for _, target := range []Target{TargetAgents, TargetClaude, TargetCursor} {
		a := mustRender(t, target, cfg)
		b := mustRender(t, target, cfg)
		if a != b {
			t.Fatalf("%s: second render differed", target)
		}
	}
}

func TestGeneratedDoNotEditHeader(t *testing.T) {
	cfg := fixtureConfig(t)
	for _, target := range []Target{TargetAgents, TargetClaude, TargetCursor} {
		got := mustRender(t, target, cfg)
		if !strings.Contains(got, "generated — do not edit") {
			t.Fatalf("%s: missing generated-do-not-edit header:\n%s", target, got)
		}
	}
}

func TestLFEndingsNoCR(t *testing.T) {
	cfg := fixtureConfig(t)
	cfg.Instructions = append(append([]instruction.Record(nil), cfg.Instructions...), instruction.Record{
		Kind: "rule", Key: "line.ending", Body: "windows\r\npath",
	})
	got := mustRender(t, TargetAgents, cfg)
	if strings.Contains(got, "\r") {
		t.Fatalf("rendered file contains CR:\n%q", got)
	}
}

func TestDriftHashUnchangedWhenOnlyGeneratedAtChanges(t *testing.T) {
	cfg := fixtureConfig(t)
	got := mustRender(t, TargetAgents, cfg)
	stamp := "generated-at:" + cfg.GeneratedAt
	if !strings.Contains(got, stamp) {
		t.Fatalf("footer missing generated-at:\n%s", got)
	}
	altered := strings.Replace(got, stamp, "generated-at:2099-12-31T23:59:59Z", 1)
	if altered == got {
		t.Fatal("timestamp replace did nothing")
	}
	if DriftHash(got) != DriftHash(altered) {
		t.Fatalf("drift hash changed when only the footer's generated-at changed\n orig %s\naltered %s", DriftHash(got), DriftHash(altered))
	}
	if DriftHash(got) == "" {
		t.Fatal("drift hash is empty")
	}
}

func TestDriftHashChangesWhenContentChanges(t *testing.T) {
	cfg := fixtureConfig(t)
	base := mustRender(t, TargetAgents, cfg)
	baseHash := DriftHash(base)

	t.Run("rule", func(t *testing.T) {
		next := cfg
		next.Instructions = copyRecords(cfg.Instructions)
		changed := false
		for i, r := range next.Instructions {
			if r.Key == "python.version" {
				next.Instructions[i].Body = "3.11"
				changed = true
			}
		}
		if !changed {
			t.Fatal("fixture missing python.version")
		}
		got := mustRender(t, TargetAgents, next)
		if DriftHash(got) == baseHash {
			t.Fatal("drift hash unchanged after python.version changed")
		}
	})
	t.Run("convention", func(t *testing.T) {
		next := cfg
		next.Instructions = copyRecords(cfg.Instructions)
		for i, r := range next.Instructions {
			if r.Key == "go.module" {
				next.Instructions[i].Body = "github.com/acme/other"
			}
		}
		got := mustRender(t, TargetAgents, next)
		if DriftHash(got) == baseHash {
			t.Fatal("drift hash unchanged after convention changed")
		}
	})
	t.Run("preference", func(t *testing.T) {
		next := cfg
		next.Preferences = append([]preference.Record(nil), cfg.Preferences...)
		for i, p := range next.Preferences {
			if p.Key == "editor" {
				next.Preferences[i].Body = "emacs"
			}
		}
		got := mustRender(t, TargetAgents, next)
		if DriftHash(got) == baseHash {
			t.Fatal("drift hash unchanged after preference changed")
		}
	})
}

func copyRecords(in []instruction.Record) []instruction.Record {
	out := make([]instruction.Record, len(in))
	copy(out, in)
	return out
}

func TestClaudeImportsAgentsDoesNotInlineRules(t *testing.T) {
	cfg := fixtureConfig(t)
	claude := mustRender(t, TargetClaude, cfg)
	if !strings.Contains(claude, "@AGENTS.md") {
		t.Fatalf("CLAUDE.md missing @AGENTS.md import:\n%s", claude)
	}
	if !strings.Contains(claude, "## Claude-specific") {
		t.Fatalf("CLAUDE.md missing Claude-specific block:\n%s", claude)
	}
	if !strings.Contains(claude, ClaudeBlock) {
		t.Fatalf("CLAUDE.md missing hooks reminder:\n%s", claude)
	}
	for _, leak := range []string{"python.version", "3.12", "deploy.policy", "go.module", "indent: spaces"} {
		if strings.Contains(claude, leak) {
			t.Fatalf("CLAUDE.md inlined %q; it must import AGENTS.md instead:\n%s", leak, claude)
		}
	}
	agents := mustRender(t, TargetAgents, cfg)
	if strings.Contains(agents, "@AGENTS.md") {
		t.Fatal("AGENTS.md must not import itself")
	}
	if strings.Contains(agents, "## Claude-specific") {
		t.Fatal("AGENTS.md must not carry the Claude-specific block")
	}
}

func TestCursorFrontmatterAndSameBody(t *testing.T) {
	cfg := fixtureConfig(t)
	agents := mustRender(t, TargetAgents, cfg)
	cursor := mustRender(t, TargetCursor, cfg)
	const front = "---\nalwaysApply: true\n---\n"
	if !strings.HasPrefix(cursor, front) {
		t.Fatalf("cursor rules missing alwaysApply frontmatter:\n%s", cursor)
	}
	agentsBody := stripFooter(agents)
	cursorBody := stripFooter(cursor)
	if !strings.HasPrefix(cursorBody, front) {
		t.Fatal("frontmatter was inside the footer")
	}
	if strings.TrimPrefix(cursorBody, front) != agentsBody {
		t.Fatalf("cursor body after frontmatter != AGENTS.md body\ncursor %q\nagents %q", strings.TrimPrefix(cursorBody, front), agentsBody)
	}
}

func TestCursorPathIsSubstrateNotACP(t *testing.T) {
	var found bool
	for _, s := range Specs() {
		if s.Target != TargetCursor {
			continue
		}
		found = true
		for _, p := range s.Paths {
			if strings.Contains(p, "acp") {
				t.Fatalf("cursor path %q contains acp; must be substrate.mdc", p)
			}
			if p != CursorRules {
				t.Fatalf("cursor path %q, want %q", p, CursorRules)
			}
		}
	}
	if !found {
		t.Fatal("Specs() omitted the cursor target")
	}
	if strings.Contains(CursorRules, "acp") {
		t.Fatal("CursorRules contains acp")
	}
}

func TestMostSpecificKeyAppearsOnce(t *testing.T) {
	cfg := fixtureConfig(t)
	got := mustRender(t, TargetAgents, cfg)
	if strings.Count(got, "python.version") != 1 {
		t.Fatalf("python.version appeared %d times, want 1:\n%s", strings.Count(got, "python.version"), got)
	}
	if !strings.Contains(got, "python.version: 3.12") {
		t.Fatalf("missing most-specific python.version=3.12:\n%s", got)
	}
	if strings.Contains(got, "3.13") {
		t.Fatalf("global python.version=3.13 leaked:\n%s", got)
	}
}

func TestSuppressedPreferenceOneLine(t *testing.T) {
	cfg := fixtureConfig(t)
	got := mustRender(t, TargetAgents, cfg)
	line := preference.Suppression{Key: "indent", Body: "tabs"}.Line()
	if strings.Count(got, line) != 1 {
		t.Fatalf("want exactly one %q, got %d:\n%s", line, strings.Count(got, line), got)
	}
	if strings.Contains(got, "indent: tabs") {
		t.Fatalf("suppressed preference leaked into preferences:\n%s", got)
	}
	if strings.Count(got, "suppressed by instruction") != 1 {
		t.Fatalf("want one suppression line, got %d:\n%s", strings.Count(got, "suppressed by instruction"), got)
	}
	if strings.Contains(got, "\n"+line+"\n"+line) {
		t.Fatal("suppression line duplicated")
	}
}

func TestMapOrderDeterminism(t *testing.T) {
	// Keys inserted in reverse-alpha order, and grouped prefixes that a map
	// range would shuffle. Output must be sorted; two renders must match.
	recs := []instruction.Record{
		{Kind: "rule", Key: "zebra.stripe", Body: "z"},
		{Kind: "rule", Key: "alpha.one", Body: "a1"},
		{Kind: "rule", Key: "mid.k", Body: "m"},
		{Kind: "rule", Key: "alpha.two", Body: "a2"},
		{Kind: "rule", Key: "zzz", Body: "tail"},
		{Kind: "rule", Key: "aaa", Body: "head"},
	}
	cfg := EffectiveConfig{Instructions: recs, GeneratedAt: "2026-01-02T15:04:05Z"}
	var last string
	for i := 0; i < 50; i++ {
		got := mustRender(t, TargetAgents, cfg)
		if last != "" && got != last {
			t.Fatalf("render %d differed from render 0 (unsorted map iteration)", i)
		}
		last = got
	}
	alpha := strings.Index(last, "### alpha")
	mid := strings.Index(last, "### mid")
	zebra := strings.Index(last, "### zebra")
	aaa := strings.Index(last, "- aaa:")
	zzz := strings.Index(last, "- zzz:")
	a1 := strings.Index(last, "- alpha.one:")
	a2 := strings.Index(last, "- alpha.two:")
	if alpha < 0 || mid < 0 || zebra < 0 || aaa < 0 || zzz < 0 || a1 < 0 || a2 < 0 {
		t.Fatalf("missing grouped keys:\n%s", last)
	}
	if aaa >= zzz || alpha >= mid || mid >= zebra || a1 >= a2 {
		t.Fatalf("keys not sorted (would pass on unsorted iteration):\n%s", last)
	}
}

func TestSkillsIndexNeverInlinesBody(t *testing.T) {
	const planted = "SKILL_BODY_MUST_NOT_INLINE"
	cfg := EffectiveConfig{
		Skills: []Skill{{
			Name:        "team/plotlens/validation",
			Description: "regression checks",
			Body:        planted,
		}},
		GeneratedAt: "2026-01-02T15:04:05Z",
	}
	got := mustRender(t, TargetAgents, cfg)
	if !strings.Contains(got, "## Skills") {
		t.Fatalf("missing skills index:\n%s", got)
	}
	if !strings.Contains(got, "team/plotlens/validation") {
		t.Fatalf("skills index missing name:\n%s", got)
	}
	if !strings.Contains(got, "regression checks") {
		t.Fatalf("skills index missing description:\n%s", got)
	}
	if strings.Contains(got, planted) {
		t.Fatalf("skill body was inlined:\n%s", got)
	}
	if strings.Contains(got, "```") {
		t.Fatalf("skills section looks like an inlined file:\n%s", got)
	}
}

func TestFooterHashMatchesDriftHash(t *testing.T) {
	got := mustRender(t, TargetAgents, fixtureConfig(t))
	want := DriftHash(got)
	marker := "<!-- sha256:"
	i := strings.LastIndex(got, marker)
	if i < 0 {
		t.Fatalf("missing sha256 footer:\n%s", got)
	}
	rest := got[i+len(marker):]
	hexPart, _, ok := strings.Cut(rest, " ")
	if !ok {
		t.Fatalf("footer not parseable: %q", rest)
	}
	if hexPart != want {
		t.Fatalf("footer sha256 %q != DriftHash %q", hexPart, want)
	}
}

func TestUnknownTargetErrors(t *testing.T) {
	_, err := Render(Target("nope"), EffectiveConfig{})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown target err %v", err)
	}
}

func TestAgentsSectionOrder(t *testing.T) {
	got := mustRender(t, TargetAgents, fixtureConfig(t))
	rules := strings.Index(got, "## Rules")
	conv := strings.Index(got, "## Conventions")
	prefs := strings.Index(got, "## Preferences")
	skills := strings.Index(got, "## Skills")
	if rules < 0 || conv < 0 || prefs < 0 || skills < 0 {
		t.Fatalf("missing section:\n%s", got)
	}
	if rules >= conv || conv >= prefs || prefs >= skills {
		t.Fatalf("section order want Rules, Conventions, Preferences, Skills:\n%s", got)
	}
}

func stripFooter(s string) string {
	body, ok := dropTrailingFooterLine(s)
	if !ok {
		return s
	}
	return body
}

func TestDriftHashIsSha256OfBody(t *testing.T) {
	got := mustRender(t, TargetAgents, fixtureConfig(t))
	body := stripFooter(got)
	if body == got {
		t.Fatal("no footer to exclude")
	}
	sum := sha256.Sum256([]byte(body))
	want := hex.EncodeToString(sum[:])
	if DriftHash(got) != want {
		t.Fatalf("DriftHash %q, want sha256(body) %q", DriftHash(got), want)
	}
}

func TestDriftHashTrailingFooterOnly(t *testing.T) {
	const planted = "<!-- sha256:deadbeef -->"
	cfg := EffectiveConfig{
		Instructions: []instruction.Record{
			{Kind: "rule", Key: "trap.marker", Body: "\n" + planted},
			{Kind: "rule", Key: "zzz.after", Body: "must-be-hashed"},
		},
		GeneratedAt: "2026-01-02T15:04:05Z",
	}
	full := mustRender(t, TargetAgents, cfg)
	if !strings.Contains(full, "\n"+planted+"\n") {
		t.Fatalf("planted marker is not on its own line:\n%s", full)
	}
	body, ok := dropTrailingFooterLine(full)
	if !ok {
		t.Fatal("rendered output missing trailing footer")
	}
	if !strings.Contains(body, planted) {
		t.Fatal("interior marker was dropped with the footer")
	}
	if !strings.Contains(body, "must-be-hashed") {
		t.Fatal("content after interior marker missing from body")
	}

	t.Run("interior marker does not change coverage", func(t *testing.T) {
		if DriftHash(full) != sha256Hex(body) {
			t.Fatalf("DriftHash cut at interior %q\n got %s\nwant %s", planted, DriftHash(full), sha256Hex(body))
		}
		edited := strings.Replace(full, "must-be-hashed", "TAMPERED", 1)
		if DriftHash(full) == DriftHash(edited) {
			t.Fatal("edit after interior <!-- sha256: did not change DriftHash")
		}
	})

	t.Run("footer removed hashes all remaining bytes", func(t *testing.T) {
		if DriftHash(body) != sha256Hex(body) {
			t.Fatalf("footer removed: DriftHash cut at an interior marker\n got %s\nwant %s", DriftHash(body), sha256Hex(body))
		}
		tampered := strings.Replace(body, "must-be-hashed", "TAMPERED", 1)
		if DriftHash(body) == DriftHash(tampered) {
			t.Fatal("edit after interior <!-- sha256: was invisible once the real footer was gone")
		}
	})

	t.Run("mangled trailing footer is not stripped", func(t *testing.T) {
		mangled := body + planted + "\n"
		if DriftHash(mangled) == DriftHash(full) {
			t.Fatal("replacing the real footer with <!-- sha256:deadbeef --> left DriftHash unchanged")
		}
		if DriftHash(mangled) != sha256Hex(mangled) {
			t.Fatal("mangled footer was still excluded from the hash")
		}
	})
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// dropTrailingFooterLine removes the last line of a freshly rendered file when
// that line is an appendFooter comment. It finds the last newline, not a
// substring a body can contain.
func dropTrailingFooterLine(s string) (string, bool) {
	if !strings.HasSuffix(s, " -->\n") {
		return "", false
	}
	i := strings.LastIndex(s[:len(s)-1], "\n")
	if i < 0 {
		return "", false
	}
	line := s[i+1:]
	if !strings.HasPrefix(line, "<!-- sha256:") || !strings.Contains(line, " generated-at:") {
		return "", false
	}
	return s[:i+1], true
}

func TestNoTimestampOutsideFooter(t *testing.T) {
	cfg := fixtureConfig(t)
	got := mustRender(t, TargetAgents, cfg)
	body := stripFooter(got)
	if strings.Contains(body, cfg.GeneratedAt) {
		t.Fatalf("generated-at leaked outside the footer:\n%s", body)
	}
	if bytes.Contains([]byte(body), []byte("2026-")) {
		t.Fatalf("timestamp-like text outside footer:\n%s", body)
	}
}
