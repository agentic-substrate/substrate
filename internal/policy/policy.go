// Package policy is the Go-side authorization gate that runs before SQL.
// RLS is the backstop; Check exists so denials have a machine-readable code
// instead of an empty result set (EDD §4.3).
package policy

import (
	"errors"
	"fmt"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/scope"
)

// Machine-readable denial codes. Hooks branch on these, never on prose.
const (
	CodeDeniedScope      = "SUBSTRATE_DENIED_SCOPE"
	CodeDeniedVisibility = "SUBSTRATE_DENIED_VISIBILITY"
	CodeNeedsReview      = "SUBSTRATE_NEEDS_REVIEW"
)

// ErrDeniedScope is returned when the principal may not act at this scope.
var ErrDeniedScope = errors.New(CodeDeniedScope)

// ErrDeniedVisibility is returned when visibility rules refuse the action.
var ErrDeniedVisibility = errors.New(CodeDeniedVisibility)

// ErrNeedsReview is returned when the write must go through a review item.
var ErrNeedsReview = errors.New(CodeNeedsReview)

// Check is the primary authorization gate and runs before SQL. RLS is the
// backstop so a missing filter cannot leak a row; Check exists so the caller
// gets a named denial instead of an empty result set (EDD §4.3).
func Check(action string, sc scope.Path, p identity.Principal) error {
	if err := sc.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrDeniedScope, err)
	}
	if p.Disabled {
		return fmt.Errorf("%w: principal disabled", ErrDeniedScope)
	}
	if req, ok := requiredCap(action); ok && !hasCap(p, req) {
		return fmt.Errorf("%w: missing %s", ErrDeniedScope, req)
	}
	if p.IsAdmin() {
		return nil
	}
	switch sc.Leaf().Kind {
	case scope.Global, scope.Org:
		if isWrite(action) {
			return fmt.Errorf("%w: org/global writes require human_admin", ErrDeniedScope)
		}
	case scope.Team, scope.Project, scope.Repo, scope.Branch, scope.Task, scope.Session:
		if len(p.TeamIDs) == 0 {
			return fmt.Errorf("%w: not a member of a team at this scope", ErrDeniedScope)
		}
	}
	return nil
}

func isWrite(action string) bool {
	switch action {
	case "memory.write", "memory.supersede", "instruction.propose", "instruction.write", "preference.write", "skill.propose":
		return true
	default:
		return false
	}
}

func requiredCap(action string) (string, bool) {
	switch action {
	case "memory.write", "memory.supersede":
		return "memory:write", true
	case "instruction.propose", "instruction.write":
		return "instruction:propose", true
	default:
		return "", false
	}
}

func hasCap(p identity.Principal, need string) bool {
	for _, c := range p.Capabilities {
		if c == need {
			return true
		}
	}
	return false
}
