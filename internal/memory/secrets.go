package memory

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/agentic-substrate/substrate/internal/policy"
)

var (
	awsKeyRe = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	pemRe    = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	jwtRe    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	identRe  = regexp.MustCompile(`[A-Za-z0-9_./-]+\.[A-Za-z][A-Za-z0-9]+`)
)

// scanSecrets returns SUBSTRATE_SECRET_DETECTED when s looks like an AWS key,
// PEM block, or JWT. The match itself is never included in the error.
func scanSecrets(s string) error {
	if awsKeyRe.MatchString(s) || pemRe.MatchString(s) || jwtRe.MatchString(s) {
		return fmt.Errorf("%w", policy.ErrSecretDetected)
	}
	return nil
}

func scanAll(parts ...string) error {
	for _, p := range parts {
		if err := scanSecrets(p); err != nil {
			return err
		}
	}
	return nil
}

func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func capBody(s string) string {
	if len(s) <= 4096 {
		return s
	}
	s = s[:4096]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func extractIdentifiers(body string, existing []string) []string {
	seen := make(map[string]struct{}, len(existing)+4)
	out := make([]string, 0, len(existing)+4)
	for _, id := range existing {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, m := range identRe.FindAllString(body, -1) {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	if out == nil {
		out = []string{}
	}
	return out
}
