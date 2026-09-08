package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/instruction"
	"github.com/agentic-substrate/substrate/internal/preference"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

type resolved struct {
	ins   []instruction.Record
	prefs []preference.Record
	notes []preference.Suppression
}

// resolveAs runs the real resolution path under one principal's session
// settings, which is where owner visibility either hides a row or does not.
func resolveAs(t *testing.T, dsn string, p *identity.Principal, pathStr string, overlay []string) resolved {
	t.Helper()
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	path, err := scope.Parse(pathStr)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	ids := make([]uuid.UUID, len(overlay))
	for i, s := range overlay {
		ids[i] = mustUUID(t, s)
	}
	var out resolved
	ctx := identity.WithPrincipal(t.Context(), p)
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		out.ins, err = instruction.Resolve(ctx, tx, path)
		if err != nil {
			return err
		}
		out.prefs, out.notes, err = preference.Resolve(ctx, tx, ids, instruction.Keys(out.ins))
		return err
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return out
}

func prefKeyForBody(t *testing.T, conn *pgx.Conn, body, scopeID string) string {
	t.Helper()
	var key string
	if err := conn.QueryRow(t.Context(),
		`SELECT key FROM preference WHERE body = $1 AND scope_id = $2`, body, scopeID).Scan(&key); err != nil {
		t.Fatalf("preference key for %q: %v", body, err)
	}
	return key
}

// TestImportedPreferenceStaysOwnerVisibleThroughReview is the end-to-end
// statement of #86: a personal preference imported from ~/.claude/CLAUDE.md
// is approvable through review decide, resolves for its owner, and is not
// readable by another member of the same team.
//
// Restoring store.VisibilityTeam on the InsertPreference in commitWrites is
// the one-line production change that makes the lead-cannot-see assertion red.
func TestImportedPreferenceStaysOwnerVisibleThroughReview(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	indentID := itemsBySlot(listed)[".claude/CLAUDE.md#Indent"]
	if indentID == "" {
		t.Fatalf("no Indent conflict in listing:\n%s", listed)
	}
	// The review path is the acceptance criterion, not a detail: an
	// owner-visible row must still be reachable by review_apply_preference_status.
	decideReview(t, srv, indentID,
		"-decision", "approved", "-reason", "mac indent", "-hostname", "mac", "-commit")

	var vis, status string
	if err := conn.QueryRow(t.Context(),
		`SELECT visibility::text, status::text FROM preference WHERE body = 'Prefer spaces.' AND scope_id = $1`,
		w.teamA).Scan(&vis, &status); err != nil {
		t.Fatalf("imported preference row: %v", err)
	}
	if vis != "owner" {
		t.Fatalf("imported preference is %s-visible, want owner", vis)
	}
	if status != "active" {
		t.Fatalf("review decide left the owner-visible preference %s, want active", status)
	}

	overlay := []string{w.teamA, w.org}
	owner := resolveAs(t, dsn, w.aliceP, w.pathStr, overlay)
	if countPrefBodies(owner.prefs, "Prefer spaces.") != 1 {
		t.Fatalf("owner cannot resolve their own imported preference; %#v", owner.prefs)
	}

	other := resolveAs(t, dsn, w.leadP, w.pathStr, overlay)
	if countPrefBodies(other.prefs, "Prefer spaces.") != 0 {
		t.Fatalf("another principal on the same team resolved alice's personal preference; %#v", other.prefs)
	}
	// Distinguishes "RLS restricted the read" from "the row was never
	// there": the same session must still see the team-visible rows.
	if countBodies(other.ins, "Always run gofmt.") != 1 {
		t.Fatalf("lead lost the team-visible imported instruction too, so the test proves nothing about RLS; %#v", other.ins)
	}
	if len(other.ins) == 0 || countBodies(other.ins, "true") != 1 {
		t.Fatalf("lead lost the seeded team-visible ci.required row; %#v", other.ins)
	}
}

// TestImportedPreferenceStillLosesToAnInstructionOnItsKey holds INST-3 over
// the owner-visible row.
//
// Limitation, stated rather than faked: the importer cannot itself produce a
// preference and an instruction on one key, because rowKey embeds the kind
// (import.preference.… vs import.instruction.…) and rel is scan-root
// relative, so imported keys never collide across kinds or scopes. The
// collision here is therefore constructed — but from the key the importer
// actually wrote, read back out of the database, not from a hand-written
// string, and resolved through the real resolver under alice's session.
func TestImportedPreferenceStillLosesToAnInstructionOnItsKey(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedReviewWorld(t, conn)
	srv := serveReview(t, dsn, w)
	importBothMachines(t, srv, "alice", w.pathStr)

	listed := listReview(t, srv, "lead")
	indentID := itemsBySlot(listed)[".claude/CLAUDE.md#Indent"]
	if indentID == "" {
		t.Fatalf("no Indent conflict in listing:\n%s", listed)
	}
	decideReview(t, srv, indentID,
		"-decision", "approved", "-reason", "mac indent", "-hostname", "mac", "-commit")

	key := prefKeyForBody(t, conn, "Prefer spaces.", w.teamA)
	if !strings.HasPrefix(key, "import.preference.") {
		t.Fatalf("imported preference key %q is not an importer key", key)
	}
	if _, err := conn.Exec(t.Context(),
		`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		 VALUES (gen_random_uuid(), $1, 'team', $2, 'rule', $3, 'Indent is decided by the project.', 'active', $2)`,
		w.projectA, w.alice, key); err != nil {
		t.Fatalf("seed colliding instruction: %v", err)
	}

	got := resolveAs(t, dsn, w.aliceP, w.pathStr, []string{w.teamA, w.org})
	if countPrefBodies(got.prefs, "Prefer spaces.") != 0 {
		t.Fatalf("owner-visible preference overrode the instruction on key %s (INST-3); %#v", key, got.prefs)
	}
	var line string
	for _, n := range got.notes {
		if n.Key == key {
			line = n.Line()
		}
	}
	if line == "" {
		t.Fatalf("suppression on key %s was not reported; notes %#v", key, got.notes)
	}
	if !strings.Contains(line, "Prefer spaces.") || !strings.Contains(line, "suppressed by instruction") {
		t.Fatalf("suppression line %q does not name the suppressed preference", line)
	}
}
