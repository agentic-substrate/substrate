package main

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/pgtest"
)

// rowKeyForBody reads a row's key as the migration role, so an owner-visible
// row the deciding lead cannot see is still readable by the assertion.
func rowKeyForBody(t *testing.T, conn *pgx.Conn, table, body, scopeID string) string {
	t.Helper()
	var q string
	switch table {
	case "instruction":
		q = `SELECT key FROM instruction WHERE body = $1 AND scope_id = $2`
	case "preference":
		q = `SELECT key FROM preference WHERE body = $1 AND scope_id = $2`
	default:
		t.Fatalf("unknown table %s", table)
	}
	var key string
	if err := conn.QueryRow(t.Context(), q, body, scopeID).Scan(&key); err != nil {
		t.Fatalf("%s key for %q: %v", table, body, err)
	}
	return key
}

func rowVisibilityForBody(t *testing.T, conn *pgx.Conn, table, body, scopeID string) string {
	t.Helper()
	var q string
	switch table {
	case "instruction":
		q = `SELECT visibility::text FROM instruction WHERE body = $1 AND scope_id = $2`
	case "preference":
		q = `SELECT visibility::text FROM preference WHERE body = $1 AND scope_id = $2`
	default:
		t.Fatalf("unknown table %s", table)
	}
	var vis string
	if err := conn.QueryRow(t.Context(), q, body, scopeID).Scan(&vis); err != nil {
		t.Fatalf("%s visibility for %q: %v", table, body, err)
	}
	return vis
}

// TestReviewKindFlipKeepsTheImportedSlotKey is #80 restated for the review
// path. The lead flipping alice's imported preference cannot see it under RLS
// (it is owner-visible, #86), so the key had to come from somewhere other than
// the blocked lookup. A content hash is the wrong answer: a hash-keyed row can
// never be superseded, corrected, or matched by a future import.
//
// Restoring decideKey's `"import." + asKind + ".review." + hash` fallback is
// the one-line production change that makes this red.
func TestReviewKindFlipKeepsTheImportedSlotKey(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	indentID := itemsBySlot(listed)[".claude/CLAUDE.md#Indent"]
	if indentID == "" {
		t.Fatalf("no Indent conflict in listing:\n%s", listed)
	}

	decideReview(t, srv, indentID,
		"-decision", "approved", "-reason", "tabs, but it is an instruction",
		"-hostname", "wsl", "-as-kind", "instruction", "-commit")

	prefKey := rowKeyForBody(t, conn, "preference", "Prefer tabs.", w.teamA)
	insKey := rowKeyForBody(t, conn, "instruction", "Prefer tabs.", w.projectA)
	if !strings.HasPrefix(prefKey, "import.preference.") {
		t.Fatalf("imported preference key %q is not a slot key; the comparison below would be vacuous", prefKey)
	}
	want := "import.instruction." + strings.TrimPrefix(prefKey, "import.preference.")
	if insKey != want {
		t.Fatalf("kind flip keyed the instruction %q, want the imported slot key %q", insKey, want)
	}
	if strings.Contains(insKey, ".review.") {
		t.Fatalf("kind flip minted a content-hash key %q (#80)", insKey)
	}
}

// TestReviewConflictPreferenceInsertIsOwnerVisible covers the other half of
// the flip: an instruction flipped to a preference is a personal row and must
// not be filed team-readable, which is the invariant #86 exists to establish.
//
// Restoring store.VisibilityTeam on commitReviewDecision's InsertPreference is
// the one-line production change that makes this red.
func TestReviewConflictPreferenceInsertIsOwnerVisible(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	deployID := itemsBySlot(listed)[".claude/CLAUDE.md#Deploy"]
	if deployID == "" {
		t.Fatalf("no Deploy conflict in listing:\n%s", listed)
	}

	decideReview(t, srv, deployID,
		"-decision", "approved", "-reason", "staging, and it is a preference",
		"-hostname", "mac", "-as-kind", "preference", "-commit")

	body := "Use the staging cluster."
	if got := countBodyStatus(t, conn, "preference", body, w.teamA); got["active"] != 1 {
		t.Fatalf("flip to preference did not activate %q; %#v", body, got)
	}
	if vis := rowVisibilityForBody(t, conn, "preference", body, w.teamA); vis != "owner" {
		t.Fatalf("review conflict wrote preference %q %s-visible, want owner", body, vis)
	}
}
