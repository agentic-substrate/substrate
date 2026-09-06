package memory

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/google/uuid"
)

func TestWriteRejectsMissingScopeSourceVerification(t *testing.T) {
	t.Parallel()
	svc := New(nil)
	ctx := identity.WithPrincipal(t.Context(), humanPrincipal())

	base := validWriteIn()
	cases := []struct {
		name string
		mut  func(*WriteIn)
		want string
	}{
		{"missing scope", func(in *WriteIn) { in.Scope = "" }, "scope"},
		{"missing source", func(in *WriteIn) { in.Source = nil }, "source"},
		{"missing source.machine", func(in *WriteIn) { in.Source.Machine = "" }, "source"},
		{"missing verification.type", func(in *WriteIn) { in.Verification.Type = "" }, "verification"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			src := *base.Source
			in.Source = &src
			tc.mut(&in)
			_, err := svc.Write(ctx, in)
			if err == nil {
				t.Fatalf("write succeeded; want rejection naming %s", tc.want)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("error %q does not name missing %s", err, tc.want)
			}
		})
	}
}

func TestScanSecretsRejectsGeneratedAWSKeyAndPEM(t *testing.T) {
	t.Parallel()
	aws := generatedAWSAccessKey(t)
	if err := scanSecrets("deploy key " + aws); !errors.Is(err, policy.ErrSecretDetected) {
		t.Fatalf("AWS key: %v, want %s", err, policy.CodeSecretDetected)
	}
	pem := generatedPEM(t)
	if err := scanSecrets(pem); !errors.Is(err, policy.ErrSecretDetected) {
		t.Fatalf("PEM: %v, want %s", err, policy.CodeSecretDetected)
	}
	jwt := generatedJWT(t)
	if err := scanSecrets("token=" + jwt); !errors.Is(err, policy.ErrSecretDetected) {
		t.Fatalf("JWT: %v, want %s", err, policy.CodeSecretDetected)
	}
}

func TestScanSecretsDoesNotTreatDocumentationAWSKeyAsTheTest(t *testing.T) {
	t.Parallel()
	key := generatedAWSAccessKey(t)
	if key == strings.Join([]string{"AKIA", "IOSFODNN7", "EXAMPLE"}, "") {
		t.Fatal("generated the published documentation key; scanners exclude it")
	}
}

func TestWriteSecretDoesNotAppearInLogsOrError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	svc := New(nil)
	ctx := identity.WithPrincipal(t.Context(), humanPrincipal())
	in := validWriteIn()
	key := generatedAWSAccessKey(t)
	in.Body = "leaked credential " + key

	_, err := svc.Write(ctx, in)
	if !errors.Is(err, policy.ErrSecretDetected) {
		t.Fatalf("err=%v, want %s", err, policy.CodeSecretDetected)
	}
	if err != nil && strings.Contains(err.Error(), key) {
		t.Fatalf("secret appeared in error text: %v", err)
	}
	if strings.Contains(buf.String(), key) {
		t.Fatal("secret appeared in a log line")
	}
}

func TestStripControlsAndCap(t *testing.T) {
	t.Parallel()
	got := stripControls("ok\x00\x01\n\tkeep\x1f")
	if got != "ok\n\tkeep" {
		t.Fatalf("stripControls = %q", got)
	}
	long := strings.Repeat("a", 5000)
	capped := capBody(long)
	if len(capped) > 4096 {
		t.Fatalf("capBody length %d, want <= 4096", len(capped))
	}
}

func TestSupersedeRequiresReason(t *testing.T) {
	t.Parallel()
	svc := New(nil)
	ctx := identity.WithPrincipal(t.Context(), humanPrincipal())
	_, err := svc.Supersede(ctx, SupersedeIn{OldID: uuid.Must(uuid.NewV7()).String(), Body: "new", Reason: ""})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "reason") {
		t.Fatalf("empty reason: %v, want error naming reason", err)
	}
}

func validWriteIn() WriteIn {
	in := WriteIn{
		Kind:   "fact",
		Title:  "title",
		Body:   "the validator lives in validation.py",
		Scope:  "global:/org:acme/team:core/project:plotlens",
		Status: "confirmed",
		Tier:   "semantic",
	}
	in.Verification.Type = "human"
	in.Source = &SourceIn{Machine: "wsl"}
	return in
}

func humanPrincipal() *identity.Principal {
	return &identity.Principal{
		ID:           uuid.Must(uuid.NewV7()),
		Kind:         identity.KindUser,
		Trust:        identity.TrustHuman,
		TeamIDs:      []uuid.UUID{uuid.Must(uuid.NewV7())},
		OrgID:        uuid.Must(uuid.NewV7()),
		Capabilities: []string{"memory:write"},
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
	key := "AKIA" + string(b)
	if key == strings.Join([]string{"AKIA", "IOSFODNN7", "EXAMPLE"}, "") {
		t.Fatal("generated the published documentation key, which scanners exclude")
	}
	return key
}

func generatedPEM(t *testing.T) string {
	t.Helper()
	return "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("A", 64) + "\n-----END RSA PRIVATE KEY-----"
}

func generatedJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"fixture"}`))
	sig := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	return header + "." + payload + "." + sig
}
