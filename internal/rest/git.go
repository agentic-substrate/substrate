package rest

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// SkillsRepo checks skills-repo reachability via git ls-remote. Failures
// surface on /v1/health/git and as the substrate_git_health metric, never
// on /readyz (EDD R27).
type SkillsRepo struct {
	URL string
}

// Check reports whether the configured skills repo is reachable.
func (s SkillsRepo) Check(ctx context.Context) error {
	if s.URL == "" {
		return fmt.Errorf("skills repo not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	//nolint:gosec // URL is operator config (-skills-repo), not request input
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", "--exit-code", s.URL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) == 0 {
			return fmt.Errorf("skills repo unreachable: %w", err)
		}
		return fmt.Errorf("skills repo unreachable: %w: %s", err, out)
	}
	return nil
}
