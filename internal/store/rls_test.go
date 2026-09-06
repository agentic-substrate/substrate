package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// rlsWorld is two teams in one org. Team B has no grant on projectA, so a
// team-visible memory there is the SCOPE-3 leak fixture. projectGranted is
// team-A-owned with a project_grant to team B (EDD R19 option B).
type rlsWorld struct {
	orgID, orgScope                          string
	teamAID, teamAScope, teamBID, teamBScope string
	projectA, projectGranted                 string
	alice, bob, carol, dave, admin           string
	memOwner, memTeam, memOrg, memGlobal     string
	memGranted                               string
	reviewA                                  string
}

func seedTwoTeams(t *testing.T, conn *pgx.Conn) rlsWorld {
	t.Helper()
	w := rlsWorld{
		orgID:          newID(t, conn),
		orgScope:       newID(t, conn),
		teamAID:        newID(t, conn),
		teamAScope:     newID(t, conn),
		teamBID:        newID(t, conn),
		teamBScope:     newID(t, conn),
		projectA:       newID(t, conn),
		projectGranted: newID(t, conn),
		alice:          newID(t, conn),
		bob:            newID(t, conn),
		carol:          newID(t, conn),
		dave:           newID(t, conn),
		admin:          newID(t, conn),
		memOwner:       newID(t, conn),
		memTeam:        newID(t, conn),
		memOrg:         newID(t, conn),
		memGlobal:      newID(t, conn),
		memGranted:     newID(t, conn),
		reviewA:        newID(t, conn),
	}
	global := newID(t, conn)
	mustExec(t, conn, `INSERT INTO principal (id, kind, display_name, trust) VALUES
		($1, 'user', 'alice', 'human'),
		($2, 'user', 'bob', 'human'),
		($3, 'user', 'carol', 'human'),
		($4, 'user', 'dave', 'human'),
		($5, 'user', 'root', 'human_admin')`,
		w.alice, w.bob, w.carol, w.dave, w.admin)
	mustExec(t, conn, `INSERT INTO org (id, name) VALUES ($1, 'acme-rls')`, w.orgID)
	mustExec(t, conn, `INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'alpha'), ($3, $2, 'beta')`,
		w.teamAID, w.orgID, w.teamBID)
	mustExec(t, conn, `INSERT INTO membership (principal_id, team_id, role) VALUES
		($1, $2, 'member'),
		($3, $4, 'member'),
		($5, $4, 'admin')`,
		w.alice, w.teamAID, w.bob, w.teamBID, w.carol)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'global', NULL, '', 0, 'placeholder')`, global)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'org', $2, 'acme-rls', 0, 'placeholder')`, w.orgScope, global)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'team', $2, 'alpha', 0, 'placeholder', $3),
		($4, 'team', $2, 'beta', 0, 'placeholder', $5)`,
		w.teamAScope, w.orgScope, w.teamAID, w.teamBScope, w.teamBID)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES
		($1, 'project', $2, 'secret', 0, 'placeholder', $3),
		($4, 'project', $2, 'shared', 0, 'placeholder', $3)`,
		w.projectA, w.teamAScope, w.teamAID, w.projectGranted)
	mustExec(t, conn, `INSERT INTO project_grant (project_scope_id, team_id, role) VALUES ($1, $2, 'member')`,
		w.projectGranted, w.teamBID)

	insertMem := func(id, vis, owner, scope string) {
		t.Helper()
		mustExec(t, conn, `INSERT INTO memory (id, scope_id, visibility, owner_id, tier, kind, title, body, source, verification, created_by)
			VALUES ($1, $2, $3, $4, 'working', 'fact', 't', 'b', '{"machine":"wsl"}', '{"type":"human"}', $4)`,
			id, scope, vis, owner)
	}
	insertMem(w.memOwner, "owner", w.alice, w.projectA)
	insertMem(w.memTeam, "team", w.alice, w.projectA)
	insertMem(w.memOrg, "org", w.alice, w.projectA)
	insertMem(w.memGlobal, "global", w.alice, w.projectA)
	insertMem(w.memGranted, "team", w.alice, w.projectGranted)

	mustExec(t, conn, `INSERT INTO review_item (id, kind, scope_id, team_id, payload, proposed_by)
		VALUES ($1, 'drift_proposal', $2, $3, '{"subject":"x"}', $4)`,
		w.reviewA, w.projectA, w.teamAID, w.alice)
	return w
}

func pgUUIDArray(ids []string) string {
	if len(ids) == 0 {
		return "{}"
	}
	return "{" + strings.Join(ids, ",") + "}"
}

func applySession(t *testing.T, tx pgx.Tx, actor, org string, teams, grants []string, admin bool) {
	t.Helper()
	ctx := t.Context()
	set := func(k, v string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, k, v); err != nil {
			t.Fatalf("set_config %s: %v", k, err)
		}
	}
	set("substrate.actor_id", actor)
	set("substrate.org_id", org)
	set("substrate.team_ids", pgUUIDArray(teams))
	set("substrate.granted_project_ids", pgUUIDArray(grants))
	if admin {
		set("substrate.is_admin", "true")
	} else {
		set("substrate.is_admin", "false")
	}
}

func beginApp(t *testing.T, st *Store, actor, org string, teams, grants []string, admin bool) pgx.Tx {
	t.Helper()
	tx, err := st.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(t.Context()) })
	applySession(t, tx, actor, org, teams, grants, admin)
	return tx
}

func countMemory(t *testing.T, tx pgx.Tx, id string) (int, error) {
	t.Helper()
	var n int
	err := tx.QueryRow(t.Context(), `SELECT count(*) FROM memory WHERE id = $1`, id).Scan(&n)
	return n, err
}

func TestRLSForceAndHelpersExist(t *testing.T) {
	_, conn := startMigrated(t)
	for _, table := range []string{"instruction", "preference", "memory", "skill", "review_item"} {
		var rls, force bool
		if err := conn.QueryRow(t.Context(),
			`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname = $1 AND relkind = 'r'`,
			table).Scan(&rls, &force); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if !rls || !force {
			t.Errorf("%s: relrowsecurity=%v relforcerowsecurity=%v, want both true", table, rls, force)
		}
	}
}

func TestRLSTeamBCannotReadTeamAMemoryWithoutAppFilter(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	var role string
	if err := st.pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != "substrate_app" {
		t.Fatalf("pool current_user = %q, want substrate_app; leak tests must use the pool's role, not SET ROLE", role)
	}

	// Deliberately no application-layer WHERE on visibility or team: just the
	// row id. RLS is the only filter (SCOPE-3).
	tx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, nil, false)
	n, err := countMemory(t, tx, w.memTeam)
	if err != nil {
		t.Fatalf("denied read must return zero rows, not an error (error leaks existence): %v", err)
	}
	if n != 0 {
		t.Fatalf("team-B member read team-A team-visible memory (%d rows); RLS backstop failed", n)
	}
}

func TestRLSSelectInsertUpdateMatrix(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	type caller struct {
		name   string
		actor  string
		org    string
		teams  []string
		grants []string
		admin  bool
	}
	alice := caller{"alice", w.alice, w.orgID, []string{w.teamAID}, nil, false}
	bob := caller{"bob", w.bob, w.orgID, []string{w.teamBID}, nil, false}
	bobGrant := caller{"bob-grant", w.bob, w.orgID, []string{w.teamBID}, []string{w.projectGranted}, false}
	carol := caller{"carol-team-admin", w.carol, w.orgID, []string{w.teamBID}, nil, false} // membership.role=admin is NOT is_admin
	dave := caller{"dave-outsider", w.dave, "", nil, nil, false}
	root := caller{"human-admin", w.admin, w.orgID, nil, nil, true}

	t.Run("SELECT", func(t *testing.T) {
		cases := []struct {
			caller caller
			row    string
			want   int
		}{
			{alice, w.memOwner, 1},
			{alice, w.memTeam, 1},
			{alice, w.memOrg, 1},
			{alice, w.memGlobal, 1},
			{bob, w.memOwner, 0},
			{bob, w.memTeam, 0},
			{bob, w.memOrg, 1},
			{bob, w.memGlobal, 1},
			{bob, w.memGranted, 0}, // no grant in this session
			{bobGrant, w.memTeam, 0},
			{bobGrant, w.memGranted, 1},
			{carol, w.memOwner, 0},
			{carol, w.memTeam, 0},
			{carol, w.memOrg, 1},
			{dave, w.memOwner, 0},
			{dave, w.memTeam, 0},
			{dave, w.memOrg, 0},
			{dave, w.memGlobal, 1},
			{root, w.memOwner, 1},
			{root, w.memTeam, 1},
			{root, w.memOrg, 1},
			{root, w.memGlobal, 1},
		}
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%s", tc.caller.name, tc.row), func(t *testing.T) {
				tx := beginApp(t, st, tc.caller.actor, tc.caller.org, tc.caller.teams, tc.caller.grants, tc.caller.admin)
				n, err := countMemory(t, tx, tc.row)
				if err != nil {
					t.Fatalf("SELECT leaked an error: %v", err)
				}
				if n != tc.want {
					t.Fatalf("count=%d, want %d", n, tc.want)
				}
			})
		}
	})

	t.Run("INSERT", func(t *testing.T) {
		cases := []struct {
			name   string
			caller caller
			scope  string
			allow  bool
		}{
			{"alice writes team-A project", alice, w.projectA, true},
			{"bob cannot write team-A project", bob, w.projectA, false},
			{"bob can write granted project", bobGrant, w.projectGranted, true},
			{"dave cannot write team-A project", dave, w.projectA, false},
			{"admin can write team-A project", root, w.projectA, true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tx := beginApp(t, st, tc.caller.actor, tc.caller.org, tc.caller.teams, tc.caller.grants, tc.caller.admin)
				_, err := tx.Exec(t.Context(), `INSERT INTO memory (id, scope_id, visibility, owner_id, tier, kind, title, body, source, verification, created_by)
					VALUES (gen_random_uuid(), $1, 'team', $2, 'working', 'fact', 't', 'b', '{"machine":"wsl"}', '{"type":"human"}', $2)`,
					tc.scope, tc.caller.actor)
				if tc.allow {
					if err != nil {
						t.Fatalf("INSERT denied: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatal("INSERT succeeded; want policy denial")
				}
			})
		}
	})

	t.Run("INSERT spoofed owner is refused", func(t *testing.T) {
		tx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, nil, false)
		_, err := tx.Exec(t.Context(), `INSERT INTO memory (id, scope_id, visibility, owner_id, tier, kind, title, body, source, verification, created_by)
			VALUES (gen_random_uuid(), $1, 'team', $2, 'working', 'fact', 't', 'b', '{"machine":"wsl"}', '{"type":"human"}', $2)`,
			w.projectA, w.alice)
		if err == nil {
			t.Fatal("bob inserted a row owned by alice")
		}
	})

	t.Run("UPDATE", func(t *testing.T) {
		cases := []struct {
			name   string
			caller caller
			row    string
			want   int64
		}{
			{"alice updates her team memory", alice, w.memTeam, 1},
			{"bob cannot update alice team memory", bob, w.memTeam, 0},
			{"carol team-admin cannot update alice team memory", carol, w.memTeam, 0},
			{"dave cannot update global memory he can see", dave, w.memGlobal, 0},
			{"admin updates owner memory", root, w.memOwner, 1},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tx := beginApp(t, st, tc.caller.actor, tc.caller.org, tc.caller.teams, tc.caller.grants, tc.caller.admin)
				tag, err := tx.Exec(t.Context(), `UPDATE memory SET title = 'edited' WHERE id = $1`, tc.row)
				if err != nil {
					t.Fatalf("UPDATE leaked an error: %v", err)
				}
				if tag.RowsAffected() != tc.want {
					t.Fatalf("rows affected=%d, want %d", tag.RowsAffected(), tc.want)
				}
			})
		}
	})
}

func TestRLSDeniedReadWithoutSessionSettings(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	var n int
	err = st.pool.QueryRow(ctx, `SELECT count(*) FROM memory WHERE id = $1`, w.memTeam).Scan(&n)
	if err != nil {
		t.Fatalf("SELECT without session settings must not error: %v", err)
	}
	if n != 0 {
		t.Fatalf("unauthenticated pool read %d rows; policies must filter, not raise", n)
	}
}

func TestRLSSameShapeOnSiblingTables(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	ctx := t.Context()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	insID := newID(t, conn)
	prefID := newID(t, conn)
	skillID := newID(t, conn)
	mustExec(t, conn, `INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, created_by)
		VALUES ($1, $2, 'team', $3, 'rule', 'python.version', '3.12', $3)`, insID, w.projectA, w.alice)
	mustExec(t, conn, `INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, created_by)
		VALUES ($1, $2, 'team', $3, 'indent', 'tabs', $3)`, prefID, w.teamAScope, w.alice)
	mustExec(t, conn, `INSERT INTO skill (id, name, scope_id, visibility, owner_id, description)
		VALUES ($1, 'team/alpha/rls', $2, 'team', $3, 'd')`, skillID, w.projectA, w.alice)

	tx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, nil, false)
	for _, q := range []struct {
		name string
		sql  string
		id   string
	}{
		{"instruction", `SELECT count(*) FROM instruction WHERE id = $1`, insID},
		{"preference", `SELECT count(*) FROM preference WHERE id = $1`, prefID},
		{"skill", `SELECT count(*) FROM skill WHERE id = $1`, skillID},
		{"review_item", `SELECT count(*) FROM review_item WHERE id = $1`, w.reviewA},
	} {
		var n int
		if err := tx.QueryRow(t.Context(), q.sql, q.id).Scan(&n); err != nil {
			t.Fatalf("%s denied read errored: %v", q.name, err)
		}
		if n != 0 {
			t.Errorf("%s: team-B read team-A team-visible row", q.name)
		}
	}

	aliceTx := beginApp(t, st, w.alice, w.orgID, []string{w.teamAID}, nil, false)
	var n int
	if err := aliceTx.QueryRow(t.Context(), `SELECT count(*) FROM review_item WHERE id = $1`, w.reviewA).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("owning team cannot read its review_item: count=%d", n)
	}
}

func TestRLSHelpersAreStableNotDefiner(t *testing.T) {
	_, conn := startMigrated(t)
	for _, fn := range []string{"scope_org", "scope_team", "scope_project", "scope_writable"} {
		var vol, definer string
		err := conn.QueryRow(t.Context(), `
			SELECT p.provolatile, CASE WHEN p.prosecdef THEN 'definer' ELSE 'invoker' END
			FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'public' AND p.proname = $1`, fn).Scan(&vol, &definer)
		if err != nil {
			t.Fatalf("%s missing: %v", fn, err)
		}
		if vol != "s" {
			t.Errorf("%s provolatile=%q, want STABLE (s)", fn, vol)
		}
		if definer != "invoker" {
			t.Errorf("%s is SECURITY DEFINER; that bypasses RLS", fn)
		}
	}
}
