// Package scope implements the canonical scope chain that every other domain
// package and the context compiler resolve against.
//
// The chain is fixed and ordered from least to most specific:
//
//	global → org → team → project → repo → branch → task → session
//
// `user` is deliberately NOT part of this chain. It is an orthogonal overlay
// root: preferences resolve user > team > org, while applicability resolves
// along the chain above. Conflating the two is the mistake this package exists
// to prevent.
package scope

import (
	"fmt"
	"strings"
)

// Kind identifies a level of the canonical chain.
type Kind string

// The canonical chain levels, least specific first.
const (
	Global  Kind = "global"
	Org     Kind = "org"
	Team    Kind = "team"
	Project Kind = "project"
	Repo    Kind = "repo"
	Branch  Kind = "branch"
	Task    Kind = "task"
	Session Kind = "session"

	// User is the overlay root. It never appears inside a chain.
	User Kind = "user"
)

// chain is the canonical order, least specific first.
var chain = []Kind{Global, Org, Team, Project, Repo, Branch, Task, Session}

// depth maps a Kind to its position in the chain; -1 for Kinds outside it.
func depth(k Kind) int {
	for i, c := range chain {
		if c == k {
			return i
		}
	}
	return -1
}

// InChain reports whether k is part of the canonical chain (i.e. not User).
func InChain(k Kind) bool { return depth(k) >= 0 }

// Depth returns the specificity depth of k, used by the compiler's
// specificity multiplier. It panics for Kinds outside the chain, which is a
// programming error rather than a runtime condition.
func Depth(k Kind) int {
	d := depth(k)
	if d < 0 {
		panic(fmt.Sprintf("scope: %q is not part of the canonical chain", k))
	}
	return d
}

// Segment is one level of a concrete path, e.g. {Repo, "plotlens/api"}.
type Segment struct {
	Kind Kind
	Name string
}

// Path is a concrete scope location, ordered least specific first. A valid Path
// starts at Global and each segment's kind is the immediate predecessor of the
// next (EDD §3.1): global → project is invalid, project → org is not.
type Path []Segment

// Parse reads the wire form "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi".
// Names are percent-decoded for "/" only, since repo names contain slashes.
func Parse(s string) (Path, error) {
	if s == "" {
		return nil, fmt.Errorf("scope: empty path")
	}
	parts := strings.Split(s, "/")
	p := make(Path, 0, len(parts))
	for _, part := range parts {
		kind, name, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("scope: segment %q is missing its kind prefix", part)
		}
		p = append(p, Segment{Kind: Kind(kind), Name: strings.ReplaceAll(name, "%2F", "/")})
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// String renders the wire form Parse accepts.
func (p Path) String() string {
	parts := make([]string, len(p))
	for i, s := range p {
		parts[i] = string(s.Kind) + ":" + strings.ReplaceAll(s.Name, "/", "%2F")
	}
	return strings.Join(parts, "/")
}

// Validate reports whether p is a well-formed chain: rooted at global, each
// kind the immediate predecessor of the next, every Kind in the chain, and
// every non-global segment named.
func (p Path) Validate() error {
	if len(p) == 0 {
		return fmt.Errorf("scope: empty path")
	}
	if p[0].Kind != Global {
		return fmt.Errorf("scope: path must be rooted at %q, got %q", Global, p[0].Kind)
	}
	last := -1
	for i, s := range p {
		d := depth(s.Kind)
		if d < 0 {
			return fmt.Errorf("scope: %q is not part of the canonical chain", s.Kind)
		}
		if d != last+1 {
			return fmt.Errorf("scope: segment %d (%q) is not the immediate predecessor of the previous kind", i, s.Kind)
		}
		if s.Kind != Global && s.Name == "" {
			return fmt.Errorf("scope: segment %d (%q) has an empty name", i, s.Kind)
		}
		last = d
	}
	return nil
}

// Ancestors returns p and every prefix of it, MOST specific first. This is the
// order the compiler walks for instruction resolution: the first active
// instruction seen for a key wins, so the most specific scope must come first.
func (p Path) Ancestors() []Path {
	out := make([]Path, 0, len(p))
	for i := len(p); i > 0; i-- {
		out = append(out, p[:i])
	}
	return out
}

// Contains reports whether q is at or below p, i.e. whether an item applicable
// at p is a candidate when compiling for q.
func (p Path) Contains(q Path) bool {
	if len(p) > len(q) {
		return false
	}
	for i := range p {
		if p[i] != q[i] {
			return false
		}
	}
	return true
}

// Leaf returns the most specific segment of p.
func (p Path) Leaf() Segment { return p[len(p)-1] }
