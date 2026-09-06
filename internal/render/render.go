// Package render turns an EffectiveConfig into per-target files (EDD §6, INST-4).
// Rendering is a pure function of EffectiveConfig: no clock, no environment,
// no filesystem reads. The footer is excluded from the drift hash (Gotcha 9, R14).
package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/agentic-substrate/substrate/internal/instruction"
	"github.com/agentic-substrate/substrate/internal/preference"
)

// Target is one generated-file shape from EDD §6.
type Target string

const (
	// TargetAgents is Codex / Cursor / generic AGENTS.md (EDD §6).
	TargetAgents Target = "agents"
	// TargetClaude is CLAUDE.md: @AGENTS.md plus a Claude-specific block.
	TargetClaude Target = "claude"
	// TargetCursor is ~/.cursor/rules/substrate.mdc.
	TargetCursor Target = "cursor"
)

// Paths named in EDD §6. The Cursor file is substrate.mdc, not acp.mdc.
const (
	RepoAgents  = "AGENTS.md"
	HomeAgents  = "~/.codex/AGENTS.md"
	RepoClaude  = "CLAUDE.md"
	HomeClaude  = "~/.claude/CLAUDE.md"
	CursorRules = "~/.cursor/rules/substrate.mdc"
)

const cursorFrontmatter = "---\nalwaysApply: true\n---\n"

// Skill is an index entry. Body, if present, is the SKILL.md contents and
// must never be inlined (EDD §5.2, CTX-2).
type Skill struct {
	Name        string
	Description string
	Body        string
}

// EffectiveConfig is the canonical intermediate rendered per target (EDD §6).
// GeneratedAt is an input so the renderer does not read the clock.
type EffectiveConfig struct {
	Instructions []instruction.Record
	Preferences  []preference.Record
	Suppressed   []preference.Suppression
	Skills       []Skill
	GeneratedAt  string
}

// ClaudeBlock is the hooks reminder under ## Claude-specific (EDD §6).
const ClaudeBlock = "SessionStart and PostToolUse hooks are installed by the Substrate adapter. They must exit in under 2 seconds; if the server is unreachable, continue from the local cache. Do not skip them."

// Spec is one target and the paths the adapter writes it to.
type Spec struct {
	Target Target
	Paths  []string
}

// Specs returns the EDD §6 targets in stable order.
func Specs() []Spec {
	return []Spec{
		{Target: TargetAgents, Paths: []string{RepoAgents, HomeAgents}},
		{Target: TargetClaude, Paths: []string{RepoClaude, HomeClaude}},
		{Target: TargetCursor, Paths: []string{CursorRules}},
	}
}

type item struct {
	Key, Body string
}

type ruleGroup struct {
	Prefix string
	Items  []item
}

type skillItem struct {
	Name, Description string
}

type view struct {
	RuleGroups  []ruleGroup
	Conventions []item
	Preferences []item
	Suppressed  []string
	Skills      []skillItem
	ClaudeBlock string
}

var (
	agentsTmpl = template.Must(template.New("agents").Parse(agentsBody))
	claudeTmpl = template.Must(template.New("claude").Parse(claudeBody))
)

const agentsBody = `<!-- generated — do not edit -->

## Rules
{{range .RuleGroups}}
### {{.Prefix}}
{{range .Items}}- {{.Key}}: {{.Body}}
{{end}}{{end}}
## Conventions
{{if .Conventions}}
{{range .Conventions}}- {{.Key}}: {{.Body}}
{{end}}{{end}}
## Preferences
{{if .Preferences}}
{{range .Preferences}}- {{.Key}}: {{.Body}}
{{end}}{{end}}{{range .Suppressed}}
{{.}}
{{end}}
## Skills
{{if .Skills}}
{{range .Skills}}- {{.Name}}: {{.Description}}
{{end}}{{end}}`

const claudeBody = `<!-- generated — do not edit -->

@AGENTS.md

## Claude-specific

{{.ClaudeBlock}}
`

// Render returns the generated file for target. Identical EffectiveConfig
// values produce identical bytes (INST-4).
func Render(target Target, cfg EffectiveConfig) (string, error) {
	v := buildView(cfg)
	var body string
	var err error
	switch target {
	case TargetAgents:
		body, err = execTmpl(agentsTmpl, v)
	case TargetClaude:
		body, err = execTmpl(claudeTmpl, v)
	case TargetCursor:
		body, err = execTmpl(agentsTmpl, v)
		if err == nil {
			body = cursorFrontmatter + body
		}
	default:
		return "", fmt.Errorf("render: unknown target %q", target)
	}
	if err != nil {
		return "", err
	}
	return appendFooter(body, cfg.GeneratedAt), nil
}

func execTmpl(t *template.Template, v view) (string, error) {
	var b strings.Builder
	if err := t.Execute(&b, v); err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	return lf(b.String()), nil
}

func buildView(cfg EffectiveConfig) view {
	groups := make(map[string][]item)
	var conventions []item
	for _, r := range cfg.Instructions {
		it := item{Key: lf(r.Key), Body: lf(r.Body)}
		// Conventions are their own section; rules and constraints share ## Rules.
		if r.Kind == "convention" {
			conventions = append(conventions, it)
			continue
		}
		p := keyPrefix(it.Key)
		groups[p] = append(groups[p], it)
	}
	prefixes := make([]string, 0, len(groups))
	for p := range groups {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	ruleGroups := make([]ruleGroup, 0, len(prefixes))
	for _, p := range prefixes {
		items := groups[p]
		sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
		ruleGroups = append(ruleGroups, ruleGroup{Prefix: p, Items: items})
	}
	sort.Slice(conventions, func(i, j int) bool { return conventions[i].Key < conventions[j].Key })

	prefs := make([]item, 0, len(cfg.Preferences))
	for _, p := range cfg.Preferences {
		prefs = append(prefs, item{Key: lf(p.Key), Body: lf(p.Body)})
	}
	sort.Slice(prefs, func(i, j int) bool { return prefs[i].Key < prefs[j].Key })

	notes := append([]preference.Suppression(nil), cfg.Suppressed...)
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].Key != notes[j].Key {
			return notes[i].Key < notes[j].Key
		}
		return notes[i].Body < notes[j].Body
	})
	suppressed := make([]string, 0, len(notes))
	for _, n := range notes {
		suppressed = append(suppressed, lf(n.Line()))
	}

	skills := append([]Skill(nil), cfg.Skills...)
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name != skills[j].Name {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Description < skills[j].Description
	})
	skillItems := make([]skillItem, 0, len(skills))
	for _, s := range skills {
		// Body stays off the view so a template change alone cannot inline it (CTX-2).
		skillItems = append(skillItems, skillItem{Name: lf(s.Name), Description: lf(s.Description)})
	}

	return view{
		RuleGroups:  ruleGroups,
		Conventions: conventions,
		Preferences: prefs,
		Suppressed:  suppressed,
		Skills:      skillItems,
		ClaudeBlock: ClaudeBlock,
	}
}

func keyPrefix(key string) string {
	if i := strings.IndexByte(key, '.'); i > 0 {
		return key[:i]
	}
	return key
}

func lf(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func appendFooter(body, generatedAt string) string {
	body = strings.TrimRight(body, "\n") + "\n\n"
	sum := sha256.Sum256([]byte(body))
	return body + fmt.Sprintf("<!-- sha256:%s generated-at:%s -->\n", hex.EncodeToString(sum[:]), generatedAt)
}

// DriftHash is the sha256 of content with a trailing appendFooter comment
// excluded (R14). Including the footer would make generated-at look like
// drift on every cycle. The adapter hashes files read from disk, so the cut
// must be a suffix of the exact footer shape — not a substring search a
// hand-edit can plant in the body.
func DriftHash(content string) string {
	sum := sha256.Sum256([]byte(stripTrailingFooter(content)))
	return hex.EncodeToString(sum[:])
}

func stripTrailingFooter(content string) string {
	const suffix = " -->\n"
	if !strings.HasSuffix(content, suffix) {
		return content
	}
	i := strings.LastIndex(content[:len(content)-1], "\n")
	if i < 0 {
		return content
	}
	line := content[i+1:]
	const head = "<!-- sha256:"
	const mid = " generated-at:"
	if !strings.HasPrefix(line, head) {
		return content
	}
	hexPart, after, ok := strings.Cut(line[len(head):], mid)
	if !ok || len(hexPart) != 64 || !lowerHex(hexPart) || !strings.HasSuffix(after, suffix) {
		return content
	}
	return content[:i+1]
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
