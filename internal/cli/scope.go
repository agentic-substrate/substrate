package cli

import (
	"errors"
	"fmt"
	"strings"
)

// resolvedTarget is where a write will land and which of the three sources
// won. The source is echoed with the target because the two are only useful
// together: "repo github.com/acme/api" is not reviewable until you know it
// came from the checkout you happen to be standing in.
type resolvedTarget struct {
	// Scope is an explicit scope path. Empty when the target is a repo key.
	Scope string
	// Repo is a normalized git remote the server binds to a chain itself
	// (R18). The CLI never turns a remote into a chain.
	Repo   string
	Source string
}

// String is the echo line printed before any write.
func (t resolvedTarget) String() string {
	if t.Scope != "" {
		return fmt.Sprintf("scope: %s (source: %s)", t.Scope, t.Source)
	}
	return fmt.Sprintf("repo: %s (source: %s)", t.Repo, t.Source)
}

// scopePath renders a Scope's chain as the `org:x/team:y/project:z` string the
// import and review wire bodies carry. Repo is deliberately excluded: it
// travels in its own field, because binding a remote to a chain is the
// server's job (R18).
func scopePath(s Scope) string {
	var parts []string
	for _, kv := range [][2]string{{"org", s.Org}, {"team", s.Team}, {"project", s.Project}} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+":"+kv[1])
		}
	}
	return strings.Join(parts, "/")
}

// resolveTarget picks the write target. An explicit --scope short-circuits the
// chain; otherwise it defers to resolveScope, which is the single ordering
// every command shares (flags, then context.json, then the git remote) so that
// `context show` and `import apply` can never disagree about where a write
// lands.
func resolveTarget(d Deps, op, scopeFlag string, f scopeFlags) (resolvedTarget, error) {
	if scopeFlag != "" {
		return resolvedTarget{Scope: scopeFlag, Source: "--scope flag"}, nil
	}
	s, err := resolveScope(d, f)
	if err != nil {
		var ue *UserError
		if errors.As(err, &ue) {
			return resolvedTarget{}, &UserError{
				What: op + ": no scope",
				Why:  ue.Why,
				Next: "pass --scope org:acme, run: substrate context use --org <org>, or run from a checkout whose origin the control plane has bound",
			}
		}
		return resolvedTarget{}, err
	}
	if err := requireWholeChain(op, s); err != nil {
		return resolvedTarget{}, err
	}
	if path := scopePath(s); path != "" {
		return resolvedTarget{Scope: path, Source: s.Source}, nil
	}
	if s.Repo != "" {
		return resolvedTarget{Repo: s.Repo, Source: s.Source}, nil
	}
	return resolvedTarget{}, &UserError{
		What: op + ": no scope",
		Why:  "the resolved scope named neither a chain nor a repo",
		Next: "pass --scope org:acme, or run: substrate context use --org <org>",
	}
}

// requireWholeChain refuses a headless chain. scopePath joins whatever is
// non-empty, so --team eng with no --org renders the wire string `team:eng`,
// which names no chain the server can resolve: the round trip is a 4xx that
// reads as a server problem rather than the missing flag it is. Answering
// locally with what/why/next costs no request and names the fix.
func requireWholeChain(op string, s Scope) error {
	missing := ""
	switch {
	case s.Org == "" && (s.Team != "" || s.Project != ""):
		missing = "--org"
	case s.Team == "" && s.Project != "":
		missing = "--team"
	default:
		return nil
	}
	return &UserError{
		What: op + ": incomplete scope chain",
		Why: "the resolved scope is " + scopePath(s) + ", but a chain resolves " +
			"org before team before project, so " + missing + " cannot be skipped",
		Next: "re-run with " + missing + " <name>, or run: substrate context use " + missing + " <name>",
	}
}
