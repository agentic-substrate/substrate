package rest

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// ErrGitUnavailable reports that this build has no git binary to shell out to.
//
// .ko.yaml builds substrate-server on gcr.io/distroless/static-debian12, which
// contains no shell and no git. That is deliberate — it is the smallest attack
// surface available — but it means SkillsRepo.Check can never succeed in the
// shipped image. Today SUBSTRATE_SKILLS_REPO is empty and the check
// short-circuits before reaching exec, so nothing is broken; the moment an
// operator fills it, /v1/health/git must say why it cannot work rather than
// report a bare `exec: "git": executable file not found in $PATH`, which reads
// like a transient failure of the remote.
//
// This never reaches /readyz (EDD R27): a skills-repo failure is not a
// readiness failure.
var ErrGitUnavailable = errors.New("git binary not present in this build (distroless static base): the skills-repo health check cannot run here. Either build substrate-server on a base image that contains git, or leave SUBSTRATE_SKILLS_REPO empty")

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
	if _, lookErr := exec.LookPath("git"); lookErr != nil {
		return ErrGitUnavailable
	}
	//nolint:gosec // URL is operator config (-skills-repo), not request input
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", "--exit-code", s.URL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ErrGitUnavailable
		}
		if len(out) == 0 {
			return fmt.Errorf("skills repo unreachable: %w", err)
		}
		return fmt.Errorf("skills repo unreachable: %w: %s", err, out)
	}
	return nil
}
