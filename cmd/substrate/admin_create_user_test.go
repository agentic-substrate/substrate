package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/cli"
	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/pgtest"
	"github.com/agentic-substrate/substrate/internal/store"
)

// runCreateUser drives the real command tree the way main does, against a real
// migrated Postgres. Nothing is faked below cobra: no injected DBTX, because
// the bug this file exists to catch lives between the command and the store.
func runCreateUser(t *testing.T, dsn string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := cli.NewRoot(cli.Deps{
		Stdout:    &out,
		Stderr:    &out,
		Env:       func(string) string { return "" },
		ConfigDir: t.TempDir(),
	})
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"admin", "create-user", "--dsn", dsn}, args...))
	err := root.ExecuteContext(t.Context())
	return out.String(), err
}

// Turn red by putting `st.Tx` back in place of `st.TxBootstrap` in admin.go:
// Tx demands identity.FromContext, nothing in cmd/ ever puts a principal
// there, and the command dies with "store: no principal on context" having
// written zero rows.
func TestAdminCreateUserWritesThroughTheRealCommand(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	out, err := runCreateUser(t, dsn, "--name", "ada", "--org", "realpath", "--team", "platform", "--machine", "box")
	if err != nil {
		t.Fatalf("admin create-user: %v\n%s", err, out)
	}
	if !strings.Contains(out, "token:") {
		t.Fatalf("create-user printed no token:\n%s", out)
	}
	var trust, role string
	if err := conn.QueryRow(t.Context(), `SELECT p.trust::text, m.role::text
		FROM principal p
		JOIN membership m ON m.principal_id = p.id
		JOIN team t ON t.id = m.team_id
		JOIN org o ON o.id = t.org_id
		WHERE p.display_name = 'ada' AND o.name = 'realpath'`).Scan(&trust, &role); err != nil {
		t.Fatalf("create-user wrote no principal/membership row: %v", err)
	}
	if trust != "human" {
		t.Fatalf("trust = %q, want human without --admin", trust)
	}
	var tokens int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM api_token a
		JOIN principal p ON p.id = a.principal_id
		WHERE p.display_name = 'ada' AND a.machine = 'box' AND a.expires_at IS NULL`).Scan(&tokens); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokens != 1 {
		t.Fatalf("api_token rows = %d, want exactly 1 non-expiring token", tokens)
	}
}

// Turn red by hardcoding 'admin' in the membership insert again: a user
// created without --admin then silently holds team-admin rights.
func TestAdminCreateUserRoleFollowsTheAdminFlag(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	if out, err := runCreateUser(t, dsn, "--name", "plain", "--org", "roleorg", "--team", "eng", "--machine", "box"); err != nil {
		t.Fatalf("create-user without --admin: %v\n%s", err, out)
	}
	if out, err := runCreateUser(t, dsn, "--name", "boss", "--org", "roleorg", "--team", "eng", "--machine", "box", "--admin"); err != nil {
		t.Fatalf("create-user --admin: %v\n%s", err, out)
	}
	roles := map[string]string{}
	rows, err := conn.Query(t.Context(), `SELECT p.display_name, m.role::text FROM membership m
		JOIN principal p ON p.id = m.principal_id
		JOIN team t ON t.id = m.team_id
		JOIN org o ON o.id = t.org_id WHERE o.name = 'roleorg'`)
	if err != nil {
		t.Fatalf("query roles: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, role string
		if err := rows.Scan(&name, &role); err != nil {
			t.Fatalf("scan role: %v", err)
		}
		roles[name] = role
	}
	if rows.Err() != nil {
		t.Fatalf("rows: %v", rows.Err())
	}
	if roles["plain"] != "member" {
		t.Fatalf("role without --admin = %q, want member", roles["plain"])
	}
	if roles["boss"] != "admin" {
		t.Fatalf("role with --admin = %q, want admin", roles["boss"])
	}
}

// Turn red by restoring ON CONFLICT DO NOTHING on the org and team inserts:
// the second call then returns the ids it generated and threw away, which
// name no row in either table.
func TestCreateUserReturnsTheIDsOfTheRowsItReused(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	create := func(name string) identity.CreateUserResult {
		t.Helper()
		var res identity.CreateUserResult
		if err := st.TxBootstrap(t.Context(), func(tx pgx.Tx) error {
			var terr error
			res, terr = identity.CreateUser(t.Context(), tx, identity.CreateUserInput{
				DisplayName: name, Org: "reuse", Team: "shared", Machine: "box",
			})
			return terr
		}); err != nil {
			t.Fatalf("CreateUser(%s): %v", name, err)
		}
		return res
	}
	first := create("one")
	second := create("two")
	if first.OrgID != second.OrgID {
		t.Fatalf("reusing --org returned a different org id: %s then %s", first.OrgID, second.OrgID)
	}
	if first.TeamID != second.TeamID {
		t.Fatalf("reusing --team returned a different team id: %s then %s", first.TeamID, second.TeamID)
	}
	var stored uuid.UUID
	if err := conn.QueryRow(t.Context(), `SELECT id FROM org WHERE name = 'reuse'`).Scan(&stored); err != nil {
		t.Fatalf("select org: %v", err)
	}
	if stored != second.OrgID {
		t.Fatalf("OrgID %s names no org row; the stored org is %s", second.OrgID, stored)
	}
}

// Turn red by dropping the Commit/Rollback pairing in store.TxBootstrap: a
// failure partway through would then leave an org with no members behind.
func TestTxBootstrapRollsBackEveryRow(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	boom := errors.New("boom")
	err = st.TxBootstrap(t.Context(), func(tx pgx.Tx) error {
		if _, cerr := identity.CreateUser(t.Context(), tx, identity.CreateUserInput{
			DisplayName: "ghost", Org: "rolledback", Team: "gone", Machine: "box",
		}); cerr != nil {
			return cerr
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("TxBootstrap error = %v, want boom", err)
	}
	var orgs, principals int
	if err := conn.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM org WHERE name = 'rolledback'),
		(SELECT count(*) FROM principal WHERE display_name = 'ghost')`).Scan(&orgs, &principals); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if orgs != 0 || principals != 0 {
		t.Fatalf("rollback left %d org and %d principal rows, want zero of each", orgs, principals)
	}
}
