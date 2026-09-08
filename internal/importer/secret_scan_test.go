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
			heading := "Creds-" + tc.name
			plan := Plan{
				Blocks: []Block{{
					Hash: sha256Hex([]byte(tc.body)), Heading: heading, Body: tc.body,
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
			assertSkippedSecret(t, res.Skipped, "instruction:", plan.Blocks[0].Hash, tc.body)
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

// One-line change that makes this red: delete ScanSecrets on preference bodies
// before the InsertPreference branch in commitWrites.
func TestApplyRejectsSecretInPreferenceBody(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	body := "I keep token " + generatedGitHubToken(t)
	plan := Plan{
		Blocks: []Block{{
			Hash: sha256Hex([]byte(body)), Heading: "Voice", Body: body,
			Kind: "preference", Rel: ".claude/CLAUDE.md",
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
		t.Fatalf("Active=%#v, want secret preference blocked", res.Active)
	}
	assertSkippedSecret(t, res.Skipped, "preference:", plan.Blocks[0].Hash, body)
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM preference WHERE body = $1`, body).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("preference with secret reached the database (%d rows)", n)
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
	assertSkippedSecret(t, res.Skipped, "memory:", plan.Memories[0].Hash, body)
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE body = $1`, body).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("memory with secret reached the database (%d rows)", n)
	}
}

// Machine lands in memory.source jsonb. Scanning only title/body, then storing
// an unscanned Hostname, is the one-line change that makes this red.
// Kind is not a leak path: invalid kinds fall back to "observation" before insert.
func TestApplyRejectsSecretInMemoryHostname(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	host := generatedGitHubToken(t)
	body := "hostname-secret fixture " + host[:8]
	plan := Plan{
		Memories: []MemoryItem{{
			Hash: sha256Hex([]byte(body)), Title: "cluster", Body: body,
			Kind: "fact", Hostname: host,
		}},
	}
	res, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Machine: host, TrustedMachine: host, Scope: w.pathStr, Commit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Memory) != 0 {
		t.Fatalf("Memory=%#v, want hostname-secret memory blocked", res.Memory)
	}
	assertSkippedSecret(t, res.Skipped, "memory:", plan.Memories[0].Hash, host)
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE body = $1`, body).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("memory whose source.machine is a secret reached the database (%d rows)", n)
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
	taintedHash := sha256Hex([]byte(tainted))
	plan := Plan{
		Blocks: []Block{
			{
				Hash: sha256Hex([]byte(clean)), Heading: "Shared", Body: clean,
				Kind: "instruction", Rel: ".claude/CLAUDE.md",
				Sources: []Source{{Hostname: "mac"}},
			},
			{
				Hash: taintedHash, Heading: "Token", Body: tainted,
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
	assertSkippedSecret(t, res.Skipped, "instruction:", taintedHash, tainted)
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
	assertSkippedSecret(t, res.Skipped, "instruction:", plan.Blocks[0].Hash, stored)
}

// Title defaults to Body when empty. Scanning only the empty Title before that
// default, then storing Body as Title, is the one-line change that makes this red.
func TestApplyScansDefaultedMemoryTitle(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

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
	assertSkippedSecret(t, res.Skipped, "memory:", plan.Memories[0].Hash, body)
}

// Slot carries Rel#Heading into review_item.payload. Skipping only Body, then
// writing a heading that is itself a token into the payload, is the one-line
// change that makes this red.
func TestApplyRejectsSecretInHeadingSlot(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	tok := generatedGitHubToken(t)
	body := "Always run gofmt."
	// Seed an active instruction at the same slot so Apply opens a conflict
	// and would marshal Slot into review_item.payload.
	keyPrefix := "import.instruction." + slug(".claude/CLAUDE.md") + "." + slug(tok) + "."
	if _, err := conn.Exec(t.Context(), `
		INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'rule', $3, $4, 'active', $2)`,
		w.project, w.actor, keyPrefix+"seed", "Use goimports instead.",
	); err != nil {
		t.Fatal(err)
	}

	plan := Plan{
		Blocks: []Block{{
			Hash: sha256Hex([]byte(body)), Heading: tok, Body: body,
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
	if len(res.Conflict) != 0 || len(res.Proposed) != 0 || len(res.Active) != 0 {
		t.Fatalf("secret heading still planned: conflict=%d proposed=%d active=%d",
			len(res.Conflict), len(res.Proposed), len(res.Active))
	}
	assertSkippedSecret(t, res.Skipped, "instruction:", plan.Blocks[0].Hash, tok)

	var n int
	if err := conn.QueryRow(t.Context(), `
		SELECT count(*) FROM review_item
		WHERE kind = 'import_conflict' AND payload::text LIKE '%' || $1 || '%'`, tok).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("secret heading reached review_item.payload (%d rows)", n)
	}
}

// A NUL in Heading used to abort the whole import (jsonb rejects escaped NUL).
// Sanitizing Slot before the conflict payload is the fix; leaving Heading raw
// is the one-line change that makes this red.
func TestApplySanitizesNULInHeadingSlot(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	heading := "Indent\x00ation"
	body := "Prefer spaces."
	// After sanitizeStored, heading is "Indentation"; seed must match that slot's key prefix.
	keyPrefix := "import.instruction." + slug(".claude/CLAUDE.md") + "." + slug("Indentation") + "."
	if _, err := conn.Exec(t.Context(), `
		INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'team', $2, 'rule', $3, $4, 'active', $2)`,
		w.project, w.actor, keyPrefix+"seed", "Prefer tabs.",
	); err != nil {
		t.Fatal(err)
	}

	plan := Plan{
		Blocks: []Block{{
			Hash: sha256Hex([]byte(body)), Heading: heading, Body: body,
			Kind: "instruction", Rel: ".claude/CLAUDE.md",
			Sources: []Source{{Hostname: "mac"}},
		}},
	}
	res, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})
	if err != nil {
		t.Fatalf("NUL in heading aborted import: %v", err)
	}
	for _, row := range res.Conflict {
		if strings.Contains(row.Slot, "\x00") {
			t.Fatalf("Conflict.Slot still contains NUL: %q", row.Slot)
		}
	}
	var payload string
	err = conn.QueryRow(t.Context(), `
		SELECT payload::text FROM review_item WHERE kind = 'import_conflict' ORDER BY created_at DESC LIMIT 1`).Scan(&payload)
	if err != nil {
		t.Fatalf("expected a conflict review_item after sanitized heading: %v", err)
	}
	if strings.Contains(payload, "\\u0000") || strings.Contains(payload, "\x00") {
		t.Fatalf("review payload still carries NUL: %s", payload)
	}
}

// assertSkippedSecret requires a live Source match (hash prefix) and that
// neither Source nor Reason echo the secret. A fallback that accepts any
// instruction:/memory: prefix would make this pass even if skipSource returned
// kind+":x" — that is the defect class this helper must not have.
func assertSkippedSecret(t *testing.T, skipped []Skipped, kindPrefix, hash, secret string) {
	t.Helper()
	if len(hash) > 12 {
		hash = hash[:12]
	}
	want := kindPrefix + hash
	for _, s := range skipped {
		if s.Reason != policy.CodeSecretDetected && !strings.Contains(s.Reason, policy.CodeSecretDetected) {
			continue
		}
		if s.Source != want {
			continue
		}
		if strings.Contains(s.Source, secret) || strings.Contains(s.Reason, secret) {
			t.Fatalf("secret leaked into skip report: Source=%q Reason=%q", s.Source, s.Reason)
		}
		out := (&ApplyResult{Skipped: skipped}).Format()
		if strings.Contains(out, secret) {
			t.Fatalf("secret leaked into Format() output:\n%s", out)
		}
		return
	}
	t.Fatalf("Skipped=%#v, want Source %q reason %s", skipped, want, policy.CodeSecretDetected)
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
