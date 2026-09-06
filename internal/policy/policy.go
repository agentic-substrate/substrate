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
// The EDD spells them ACP_*; AGENTS.md translates every one to SUBSTRATE_*.
const (
	CodeDeniedScope      = "SUBSTRATE_DENIED_SCOPE"
	CodeDeniedVisibility = "SUBSTRATE_DENIED_VISIBILITY"
	CodeNeedsReview      = "SUBSTRATE_NEEDS_REVIEW"
	CodeBudgetTooSmall   = "SUBSTRATE_BUDGET_TOO_SMALL"
	CodeSecretDetected   = "SUBSTRATE_SECRET_DETECTED"
)

// ErrDeniedScope is returned when the principal may not act at this scope.
var ErrDeniedScope = errors.New(CodeDeniedScope)

// ErrDeniedVisibility is returned when the principal may not see this item.
var ErrDeniedVisibility = errors.New(CodeDeniedVisibility)

// ErrNeedsReview is returned when the write must go through a review item.
var ErrNeedsReview = errors.New(CodeNeedsReview)

// ErrBudgetTooSmall is returned when instructions alone exceed the token budget.
var ErrBudgetTooSmall = errors.New(CodeBudgetTooSmall)

// ErrSecretDetected is returned when a write is rejected by the secret scanner.
var ErrSecretDetected = errors.New(CodeSecretDetected)

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
			return fmt.Errorf("%w: org/global writes require human_admin", ErrNeedsReview)
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
