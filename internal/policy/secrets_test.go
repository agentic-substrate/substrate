package policy

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestScanSecretsRejectsKnownShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    string
	}{
		{"aws", "deploy " + generatedAWSAccessKey(t)},
		{"pem", generatedPEM()},
		{"jwt", "tok=" + generatedJWT()},
		{"github", "export " + generatedGitHubToken(t)},
		{"github_pat", generatedGitHubPAT(t)},
		{"pem_lower", "-----begin rsa private key-----\n" + strings.Repeat("A", 64) + "\n-----end rsa private key-----"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ScanSecrets(tc.s)
			if !errors.Is(err, ErrSecretDetected) {
				t.Fatalf("ScanSecrets(%q-ish): %v, want %s", tc.name, err, CodeSecretDetected)
			}
			if err != nil && strings.Contains(err.Error(), tc.s) {
				t.Fatalf("secret appeared in error text: %v", err)
			}
		})
	}
}

func TestScanSecretsAllowsCleanText(t *testing.T) {
	t.Parallel()
	if err := ScanSecrets("Always run gofmt and prefer tabs."); err != nil {
		t.Fatalf("clean text: %v", err)
	}
}

// Near-misses share a detector prefix but miss the length/shape bound. Loosening
// {16} to {16,} or dropping a \b would make these start matching while positives
// stay green — that is the one-line change that makes each case red.
func TestScanSecretsAllowsNearMisses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    string
	}{
		{"aws_one_short", "AKIA" + strings.Repeat("A", 15)},
		{"ghp_below_min", "ghp_" + strings.Repeat("a", 10)},
		{"jwt_two_segments", "eyJa.b"},
		{"pem_public", "-----BEGIN PUBLIC KEY-----\n" + strings.Repeat("A", 64) + "\n-----END PUBLIC KEY-----"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ScanSecrets(tc.s); err != nil {
				t.Fatalf("near-miss %s rejected (%v); detector shape drifted", tc.name, err)
			}
		})
	}
}

func generatedAWSAccessKey(t *testing.T) string {
	t.Helper()
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "AKIA" + string(b)
}

func generatedGitHubToken(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 36)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "ghp_" + string(b)
}

func generatedGitHubPAT(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"
	b := make([]byte, 40)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "github_pat_" + string(b)
}

func generatedPEM() string {
	return "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("A", 64) + "\n-----END RSA PRIVATE KEY-----"
}

func generatedJWT() string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"fixture"}`))
	sig := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	return header + "." + payload + "." + sig
}
