package cli

import (
	"strings"
	"testing"
)

// token mint talks to Postgres directly, so it must refuse before opening
// anything when no DSN is available rather than dialling a default.
// Mutation that turns this red: falling back to a hardcoded localhost DSN in
// requireDSN, or returning nil when both --dsn and SUBSTRATE_DSN are empty.
func TestTokenMintRequiresDSN(t *testing.T) {
	_, _, err := run(t, Deps{}, "token", "mint",
		"--for", "agent",
		"--parent", "00000000-0000-0000-0000-000000000000",
		"--machine", "wsl")
	if err == nil || !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("got %v, want a DSN-required error", err)
	}
}

// The same refusal guards revoke, which is the other command that writes to
// the token table without going through the control plane.
// Mutation that turns this red: dropping the requireDSN call from revoke.
func TestTokenRevokeRequiresDSN(t *testing.T) {
	_, _, err := run(t, Deps{}, "token", "revoke", "sk-whatever")
	if err == nil || !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("got %v, want a DSN-required error", err)
	}
}
