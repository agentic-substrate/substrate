package rest

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// .ko.yaml builds substrate-server on gcr.io/distroless/static-debian12, which
// contains no git binary, and SkillsRepo.Check shells out to `git ls-remote`.
// Today SUBSTRATE_SKILLS_REPO is empty so the check short-circuits and nothing
// is broken — but the moment an operator fills it, /v1/health/git reports
// `exec: "git": executable file not found in $PATH` forever, which reads like a
// transient failure of the remote rather than a permanent property of the
// build. Say which it is.
//
// Red when: Check stops distinguishing "no git in this image" from "the remote
// is unreachable".
func TestSkillsRepoCheckSaysWhenTheImageHasNoGit(t *testing.T) {
	// An empty PATH is exactly what distroless-static looks like to exec.
	t.Setenv("PATH", t.TempDir())

	err := SkillsRepo{URL: "https://example.invalid/skills.git"}.Check(context.Background())
	if err == nil {
		t.Fatal("Check succeeded with no git binary on PATH")
	}
	if !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("want ErrGitUnavailable, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"git binary not present", "SUBSTRATE_SKILLS_REPO"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message does not mention %q, so the operator cannot tell this from an unreachable remote:\n%s", want, msg)
		}
	}
}

// An unconfigured skills repo is a supported state, not a build problem, and
// must not be reported as one.
func TestSkillsRepoCheckStillShortCircuitsWhenUnconfigured(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := SkillsRepo{}.Check(context.Background())
	if err == nil {
		t.Fatal("an unconfigured skills repo must still report as not configured")
	}
	if errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("empty URL must not be reported as a missing git binary: %v", err)
	}
}
