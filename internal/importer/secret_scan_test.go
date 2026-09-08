package importer

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
)

// One-line change that makes this red: delete the policy.ScanSecrets call on
// the instruction body before InsertInstruction in commitWrites / planWrites.
func TestApplyRejectsSecretShapesInInstructionBody(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	cases := []struct {
		name string
		body string
	}{
		{"aws", "deploy with " + generatedAWSAccessKey(t)},
		{"pem", generatedPEM(t)},
		{"jwt", "bearer " + generatedJWT(t)},
		{"github", "token " + generatedGitHubToken(t)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := Plan{
				Blocks: []Block{{
					Hash: sha256Hex([]byte(tc.body)), Heading: "Creds-" + tc.name, Body: tc.body,
					Kind: "instruction", Rel: ".claude/CLAUDE.md",
					Sources: []Source{{Hostname: "mac"}},
				}},
			}
			res, err := Apply(ctx, st, ApplyRequest{
				Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Active) != 0 {
				t.Fatalf("Active=%#v, want secret instruction blocked from plan", res.Active)
			}
			if !skippedSecret(res.Skipped, tc.body) {
				t.Fatalf("Skipped=%#v, want an entry naming the secret block", res.Skipped)
			}
			var n int
			if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM instruction WHERE body = $1`, tc.body).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("instruction with %s secret reached the database (%d rows)", tc.name, n)
			}
		})
	}
}

// One-line change that makes this red: delete the ScanSecrets call on memory
// title/body before InsertMemory.
func TestApplyRejectsSecretShapesInMemoryBody(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	body := "note " + generatedAWSAccessKey(t)
	plan := Plan{
		Memories: []MemoryItem{{
			Hash: sha256Hex([]byte(body)), Title: "leak", Body: body,
			Kind: "observation", Hostname: "mac",
		}},
	}
	res, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Memory) != 0 {
		t.Fatalf("Memory=%#v, want secret memory blocked", res.Memory)
	}
	if !skippedSecret(res.Skipped, body) {
		t.Fatalf("Skipped=%#v, want secret memory reported", res.Skipped)
	}
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE body = $1`, body).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("memory with secret reached the database (%d rows)", n)
	}
}

// One-line change that makes this red: abort the whole Apply transaction on
// the first secret instead of skipping that row.
func TestApplyImportsCleanRowsWhenOneHasSecret(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	clean := "Always run gofmt."
	tainted := "token " + generatedGitHubToken(t)
	other := "Use modules."
	plan := Plan{
		Blocks: []Block{
			{
				Hash: sha256Hex([]byte(clean)), Heading: "Shared", Body: clean,
				Kind: "instruction", Rel: ".claude/CLAUDE.md",
				Sources: []Source{{Hostname: "mac"}},
			},
			{
				Hash: sha256Hex([]byte(tainted)), Heading: "Token", Body: tainted,
				Kind: "instruction", Rel: ".claude/CLAUDE.md",
				Sources: []Source{{Hostname: "mac"}},
			},
			{
				Hash: sha256Hex([]byte(other)), Heading: "Modules", Body: other,
				Kind: "instruction", Rel: ".claude/CLAUDE.md",
				Sources: []Source{{Hostname: "mac"}},
			},
		},
	}
	res, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := countByBodyStatus(t, conn, "instruction", w.project)
	if got[clean] != "active" || got[other] != "active" {
		t.Fatalf("clean rows missing: %#v", got)
	}
	if _, ok := got[tainted]; ok {
		t.Fatalf("tainted instruction was stored: %#v", got)
	}
	if !skippedSecret(res.Skipped, "Token") {
		t.Fatalf("Skipped=%#v, want tainted source naming Token", res.Skipped)
	}
	if len(res.Active) != 2 {
		t.Fatalf("Active count=%d, want 2 clean rows; %#v", len(res.Active), res.Active)
	}
}

// One-line change that makes this red: scan the raw body before stripControls,
// then store the stripped value (the control byte reconstitutes the key).
func TestApplyScansPostTransformBytes(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	awsRest := generatedAWSAccessKey(t)[4:]
	raw := "key AKIA\x00" + awsRest
	stored := "key AKIA" + awsRest

	plan := Plan{
		Blocks: []Block{{
			Hash: sha256Hex([]byte(raw)), Heading: "Split", Body: raw,
			Kind: "instruction", Rel: ".claude/CLAUDE.md",
			Sources: []Source{{Hostname: "mac"}},
		}},
	}
	res, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	// Query only the post-strip form: Postgres rejects U+0000 in text parameters,
	// so comparing against raw would error before we learn whether a row landed.
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM instruction WHERE body = $1`, stored).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("control-byte-split secret reached the database (%d rows); scanner ran on pre-transform bytes", n)
	}
	if len(res.Skipped) == 0 {
		t.Fatal("expected Skipped entry for post-transform secret; scan likely ran on raw input only")
	}
	for _, s := range res.Skipped {
		if s.Reason == policy.CodeSecretDetected || strings.Contains(s.Reason, policy.CodeSecretDetected) {
			return
		}
	}
	t.Fatalf("Skipped=%#v, want reason %s", res.Skipped, policy.CodeSecretDetected)
}

// Title defaults to Body when empty. Scanning only the empty Title before that
// default, then storing Body as Title, is the one-line change that makes this red.
func TestApplyScansDefaultedMemoryTitle(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	// Body alone is innocuous to a scanner that only checks title before
	// defaulting; after title := body the stored title carries the JWT.
	jwt := generatedJWT(t)
	body := jwt
	plan := Plan{
		Memories: []MemoryItem{{
			Hash: sha256Hex([]byte(body)), Title: "", Body: body,
			Kind: "observation", Hostname: "mac",
		}},
	}
	res, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE body = $1 OR title = $1`, body).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("defaulted-title secret reached the database (%d rows)", n)
	}
	if !skippedSecret(res.Skipped, body) {
		t.Fatalf("Skipped=%#v, want defaulted-title secret reported", res.Skipped)
	}
}

func skippedSecret(skipped []Skipped, sourceFragment string) bool {
	for _, s := range skipped {
		if s.Reason != policy.CodeSecretDetected && !strings.Contains(s.Reason, policy.CodeSecretDetected) {
			continue
		}
		if s.Source == "" {
			continue
		}
		if sourceFragment == "" || strings.Contains(s.Source, sourceFragment) {
			return true
		}
		// Source is kind:slot/hash — never the secret body. Any coded skip counts
		// when the caller only needs to know a secret was blocked.
		if sourceFragment != "" && (strings.HasPrefix(s.Source, "instruction:") ||
			strings.HasPrefix(s.Source, "preference:") ||
			strings.HasPrefix(s.Source, "memory:")) {
			return true
		}
	}
	return false
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
