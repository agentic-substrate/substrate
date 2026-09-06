package store

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
)

func TestLookupMintedToken(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	raw, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes, expires_at)
		VALUES (gen_random_uuid(), $1, 'wsl', $2, ARRAY['memory:write'], now() + interval '24 hours')`,
		w.alice, identity.HashToken(raw))

	p, err := identity.Lookup(ctx, st.Pool(), identity.EncodeToken(raw))
	if err != nil {
		t.Fatalf("Lookup minted token: %v", err)
	}
	if p.ID.String() != w.alice {
		t.Fatalf("principal %s, want alice %s", p.ID, w.alice)
	}
	if p.IsAdmin() {
		t.Fatal("alice trust=human must not be admin")
	}
	if len(p.TeamIDs) != 1 || p.TeamIDs[0].String() != w.teamAID {
		t.Fatalf("team ids %v, want [%s]", p.TeamIDs, w.teamAID)
	}
	if p.OrgID.String() != w.orgID {
		t.Fatalf("org %s, want %s", p.OrgID, w.orgID)
	}
}

func TestHumanTeamAdminIsAdminFalse(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	raw, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes)
		VALUES (gen_random_uuid(), $1, 'wsl', $2, ARRAY['memory:write'])`, w.carol, identity.HashToken(raw))

	p, err := identity.Lookup(ctx, st.Pool(), identity.EncodeToken(raw))
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p.IsAdmin() {
		t.Fatal("trust=human with membership.role=admin must yield is_admin=false (EDD R25)")
	}

	ctx = identity.WithPrincipal(ctx, p)
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		var admin string
		if err := tx.QueryRow(ctx, `SELECT current_setting('substrate.is_admin', true)`).Scan(&admin); err != nil {
			return err
		}
		if admin != "false" {
			t.Errorf("is_admin GUC=%q, want false", admin)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRevokedTokenRejected(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	raw, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes, revoked_at)
		VALUES (gen_random_uuid(), $1, 'wsl', $2, ARRAY['memory:write'], now())`, w.alice, identity.HashToken(raw))

	if _, err := identity.Lookup(ctx, st.Pool(), identity.EncodeToken(raw)); err == nil {
		t.Fatal("revoked token was accepted")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	raw, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes, expires_at)
		VALUES (gen_random_uuid(), $1, 'wsl', $2, ARRAY['memory:write'], now() - interval '1 minute')`,
		w.alice, identity.HashToken(raw))
	if _, err := identity.Lookup(ctx, st.Pool(), identity.EncodeToken(raw)); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestAgentTokenOlderThan24hRejected(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	agent := newID(t, conn)
	mustExec(t, conn, `INSERT INTO principal (id, kind, display_name, trust, minted_by)
		VALUES ($1, 'agent', 'bot', 'agent_interactive', $2)`, agent, w.alice)
	raw, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes, expires_at, created_at)
		VALUES (gen_random_uuid(), $1, 'wsl', $2, ARRAY['memory:write'], now() + interval '24 hours', now() - interval '25 hours')`,
		agent, identity.HashToken(raw))

	if _, err := identity.Lookup(ctx, st.Pool(), identity.EncodeToken(raw)); err == nil {
		t.Fatal("agent token older than 24h was accepted (EDD R3)")
	}
}

func TestMintStoresHashNotPlaintext(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	res, err := identity.Mint(ctx, st.Pool(), identity.MintInput{
		For:     "agent",
		Parent:  w.alice,
		Machine: "wsl",
		Scopes:  []string{"memory:write"},
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if res.Token == "" {
		t.Fatal("mint must print the token once")
	}
	raw, err := identity.DecodeToken(res.Token)
	if err != nil {
		t.Fatalf("minted token not decodable: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM api_token WHERE token_hash = $1`, identity.HashToken(raw)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("hash rows=%d, want 1", n)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM api_token WHERE token_hash = $1`, []byte(res.Token)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("database stored the plaintext token")
	}

	p, err := identity.Lookup(ctx, st.Pool(), res.Token)
	if err != nil {
		t.Fatalf("lookup freshly minted: %v", err)
	}
	if p.Kind != identity.KindAgent {
		t.Fatalf("kind %q, want agent", p.Kind)
	}
	if len(p.TeamIDs) != 1 || p.TeamIDs[0].String() != w.teamAID {
		t.Fatalf("agent should inherit parent memberships, got %v", p.TeamIDs)
	}

	if err := identity.Revoke(ctx, st.Pool(), res.Token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := identity.Lookup(ctx, st.Pool(), res.Token); err == nil {
		t.Fatal("revoked minted token still authenticates")
	}
}

func TestTxSetsSessionGUCs(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	mustExec(t, conn, `INSERT INTO project_grant (project_scope_id, team_id, role) VALUES ($1, $2, 'member')`,
		w.projectGranted, w.teamAID)
	raw, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes)
		VALUES (gen_random_uuid(), $1, 'wsl', $2, ARRAY['memory:write'])`, w.alice, identity.HashToken(raw))

	p, err := identity.Lookup(ctx, st.Pool(), identity.EncodeToken(raw))
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	ctx = identity.WithPrincipal(ctx, p)
	ctx = identity.WithRequestID(ctx, "guc-test")
	err = st.Tx(ctx, func(tx pgx.Tx) error {
		var actor, teams, org, grants, admin, req string
		if err := tx.QueryRow(ctx, `SELECT current_setting('substrate.actor_id', true),
			current_setting('substrate.team_ids', true),
			current_setting('substrate.org_id', true),
			current_setting('substrate.granted_project_ids', true),
			current_setting('substrate.is_admin', true),
			current_setting('substrate.request_id', true)`).Scan(&actor, &teams, &org, &grants, &admin, &req); err != nil {
			return err
		}
		if actor != w.alice {
			t.Errorf("actor_id=%q, want alice", actor)
		}
		if !strings.Contains(teams, w.teamAID) {
			t.Errorf("team_ids=%q, want to contain %s", teams, w.teamAID)
		}
		if org != w.orgID {
			t.Errorf("org_id=%q, want %s", org, w.orgID)
		}
		if admin != "false" {
			t.Errorf("is_admin=%q, want false", admin)
		}
		if req != "guc-test" {
			t.Errorf("request_id=%q, want guc-test", req)
		}
		if !strings.Contains(grants, w.projectGranted) {
			t.Errorf("granted_project_ids=%q, want to contain %s (resolved once per request, EDD R19)", grants, w.projectGranted)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHumanAdminGUCTrue(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	raw, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, `INSERT INTO api_token (id, principal_id, machine, token_hash, scopes)
		VALUES (gen_random_uuid(), $1, 'wsl', $2, ARRAY['memory:write'])`, w.admin, identity.HashToken(raw))
	p, err := identity.Lookup(ctx, st.Pool(), identity.EncodeToken(raw))
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !p.IsAdmin() {
		t.Fatal("trust=human_admin must be IsAdmin")
	}
	ctx = identity.WithPrincipal(ctx, p)
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		var admin string
		if err := tx.QueryRow(ctx, `SELECT current_setting('substrate.is_admin', true)`).Scan(&admin); err != nil {
			return err
		}
		if admin != "true" {
			t.Errorf("is_admin GUC=%q, want true", admin)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
