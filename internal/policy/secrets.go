package policy

import (
	"fmt"
	"regexp"
)

var (
	awsKeyRe      = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	pemRe         = regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	jwtRe         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	githubTokenRe = regexp.MustCompile(`\b(?:ghp_|gho_|ghu_|ghs_|ghr_|github_pat_)[A-Za-z0-9_]{20,}\b`)
)

// ScanSecrets returns ErrSecretDetected when s looks like an AWS key, PEM
// block, JWT, or GitHub token. The match itself is never included in the error.
func ScanSecrets(s string) error {
	if awsKeyRe.MatchString(s) || pemRe.MatchString(s) || jwtRe.MatchString(s) || githubTokenRe.MatchString(s) {
		return fmt.Errorf("%w", ErrSecretDetected)
	}
	return nil
}
