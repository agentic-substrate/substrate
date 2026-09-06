package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// rlsWorld is two teams in one org. Team B has no grant on projectA, so a
// team-visible memory there is the SCOPE-3 leak fixture. projectGranted is
// team-A-owned with a project_grant to team B (EDD R19 option B).
type rlsWorld struct {
	orgID, orgScope                          string
	teamAID, teamAScope, teamBID, teamBScope string
	projectA, projectGranted                 string
	alice, bob, carol, dave, admin, erin     string
	aliceUser, bobUser                       string
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
		erin:           newID(t, conn),
		aliceUser:      newID(t, conn),
		bobUser:        newID(t, conn),
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
		($5, 'user', 'root', 'human_admin'),
		($6, 'user', 'erin', 'human')`,
		w.alice, w.bob, w.carol, w.dave, w.admin, w.erin)
	mustExec(t, conn, `INSERT INTO org (id, name) VALUES ($1, 'acme-rls')`, w.orgID)
	mustExec(t, conn, `INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'alpha'), ($3, $2, 'beta')`,
		w.teamAID, w.orgID, w.teamBID)
	mustExec(t, conn, `INSERT INTO membership (principal_id, team_id, role) VALUES
		($1, $2, 'member'),
		($3, $4, 'member'),
		($5, $4, 'admin'),
		($6, $2, 'member')`,
		w.alice, w.teamAID, w.bob, w.teamBID, w.carol, w.erin)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES
		($1, 'user', NULL, $2, 0, 'placeholder'),
		($3, 'user', NULL, $4, 0, 'placeholder')`,
		w.aliceUser, w.alice, w.bobUser, w.bob)
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
	for _, table := range []string{
		"instruction", "preference", "memory", "skill", "review_item",
		"skill_version", "memory_edge", "memory_feedback", "ingest_receipt",
	} {
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
		// projectGranted is a scope bob CAN write (grant); owner_id = alice is the conjunct under test.
		tx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, []string{w.projectGranted}, false)
		_, err := tx.Exec(t.Context(), `INSERT INTO memory (id, scope_id, visibility, owner_id, tier, kind, title, body, source, verification, created_by)
			VALUES (gen_random_uuid(), $1, 'team', $2, 'working', 'fact', 't', 'b', '{"machine":"wsl"}', '{"type":"human"}', $2)`,
			w.projectGranted, w.alice)
		if err == nil {
			t.Fatal("bob inserted a row owned by alice on a scope he can write")
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

	bobTx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, nil, false)
	aliceTx := beginApp(t, st, w.alice, w.orgID, []string{w.teamAID}, nil, false)

	type tbl struct {
		name       string
		countSQL   string
		id         string
		insert     string
		insertArgs []any
		update     string
	}
	tables := []tbl{
		{
			name:     "instruction",
			countSQL: `SELECT count(*) FROM instruction WHERE id = $1`,
			id:       insID,
			insert: `INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, created_by)
				VALUES (gen_random_uuid(), $1, 'team', $2, 'rule', 'rls.allow', 'x', $2)`,
			insertArgs: []any{w.projectA, w.alice},
			update:     `UPDATE instruction SET body = 'edited' WHERE id = $1`,
		},
		{
			name:     "preference",
			countSQL: `SELECT count(*) FROM preference WHERE id = $1`,
			id:       prefID,
			insert: `INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, created_by)
				VALUES (gen_random_uuid(), $1, 'team', $2, 'quote', 'single', $2)`,
			insertArgs: []any{w.teamAScope, w.alice},
			update:     `UPDATE preference SET body = 'edited' WHERE id = $1`,
		},
		{
			name:     "skill",
			countSQL: `SELECT count(*) FROM skill WHERE id = $1`,
			id:       skillID,
			insert: `INSERT INTO skill (id, name, scope_id, visibility, owner_id, description)
				VALUES (gen_random_uuid(), 'team/alpha/rls-allow', $1, 'team', $2, 'd')`,
			insertArgs: []any{w.projectA, w.alice},
			update:     `UPDATE skill SET description = 'edited' WHERE id = $1`,
		},
		{
			name:     "review_item",
			countSQL: `SELECT count(*) FROM review_item WHERE id = $1`,
			id:       w.reviewA,
			insert: `INSERT INTO review_item (id, kind, scope_id, team_id, payload, proposed_by)
				VALUES (gen_random_uuid(), 'drift_proposal', $1, $2, '{"subject":"y"}', $3)`,
			insertArgs: []any{w.projectA, w.teamAID, w.alice},
			update:     `UPDATE review_item SET status = 'withdrawn' WHERE id = $1`,
		},
	}
	for _, tb := range tables {
		t.Run(tb.name+"/deny-select", func(t *testing.T) {
			var n int
			if err := bobTx.QueryRow(t.Context(), tb.countSQL, tb.id).Scan(&n); err != nil {
				t.Fatalf("denied read errored: %v", err)
			}
			if n != 0 {
				t.Errorf("team-B read team-A team-visible row")
			}
		})
		t.Run(tb.name+"/allow-select", func(t *testing.T) {
			var n int
			if err := aliceTx.QueryRow(t.Context(), tb.countSQL, tb.id).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("owning team cannot read %s: count=%d (missing policy looks the same as deny)", tb.name, n)
			}
		})
		t.Run(tb.name+"/allow-insert", func(t *testing.T) {
			if _, err := aliceTx.Exec(t.Context(), tb.insert, tb.insertArgs...); err != nil {
				t.Fatalf("owning writer INSERT denied: %v", err)
			}
		})
		t.Run(tb.name+"/allow-update", func(t *testing.T) {
			tag, err := aliceTx.Exec(t.Context(), tb.update, tb.id)
			if err != nil {
				t.Fatalf("owning writer UPDATE errored: %v", err)
			}
			if tag.RowsAffected() != 1 {
				t.Fatalf("owning writer UPDATE rows=%d, want 1", tag.RowsAffected())
			}
		})
		t.Run(tb.name+"/deny-update", func(t *testing.T) {
			tag, err := bobTx.Exec(t.Context(), tb.update, tb.id)
			if err != nil {
				t.Fatalf("denied UPDATE leaked an error: %v", err)
			}
			if tag.RowsAffected() != 0 {
				t.Fatalf("team-B UPDATE rows=%d, want 0", tag.RowsAffected())
			}
		})
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

func TestRLSUserScopeIsCallerOwn(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	st, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	bobTx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, nil, false)
	_, err = bobTx.Exec(t.Context(), `INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, created_by)
		VALUES (gen_random_uuid(), $1, 'owner', $2, 'indent', 'tabs', $2)`, w.aliceUser, w.bob)
	if err == nil {
		t.Fatal("bob wrote into alice's user scope")
	}

	aliceTx := beginApp(t, st, w.alice, w.orgID, []string{w.teamAID}, nil, false)
	if _, err := aliceTx.Exec(t.Context(), `INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, created_by)
		VALUES (gen_random_uuid(), $1, 'owner', $2, 'indent', 'tabs', $2)`, w.aliceUser, w.alice); err != nil {
		t.Fatalf("alice writing her own user scope: %v", err)
	}
}

func TestRLSChildTablesAndFKOracle(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	st, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	skillID := newID(t, conn)
	mustExec(t, conn, `INSERT INTO skill (id, name, scope_id, visibility, owner_id, description)
		VALUES ($1, 'team/alpha/oracle', $2, 'team', $3, 'd')`, skillID, w.projectA, w.alice)
	verID := newID(t, conn)
	mustExec(t, conn, `INSERT INTO skill_version (id, skill_id, semver, git_sha, git_path, author_id)
		VALUES ($1, $2, '1.0.0', 'abc', 'skills/x', $3)`, verID, skillID, w.alice)

	bobTx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, nil, false)
	aliceTx := beginApp(t, st, w.alice, w.orgID, []string{w.teamAID}, nil, false)

	t.Run("skill_version deny/allow", func(t *testing.T) {
		var n int
		if err := bobTx.QueryRow(t.Context(), `SELECT count(*) FROM skill_version WHERE id = $1`, verID).Scan(&n); err != nil {
			t.Fatalf("denied read errored: %v", err)
		}
		if n != 0 {
			t.Fatal("bob read a skill_version whose parent skill is team-A")
		}
		if err := aliceTx.QueryRow(t.Context(), `SELECT count(*) FROM skill_version WHERE id = $1`, verID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("alice cannot read her skill_version: count=%d", n)
		}
		tag, err := bobTx.Exec(t.Context(), `UPDATE skill_version SET git_sha = 'evil', approval = 'approved' WHERE id = $1`, verID)
		if err != nil {
			t.Fatalf("denied UPDATE leaked an error: %v", err)
		}
		if tag.RowsAffected() != 0 {
			t.Fatal("bob rewrote git_sha/approval on a skill_version he cannot see")
		}
	})

	t.Run("fk oracle on memory_edge", func(t *testing.T) {
		missing := newID(t, conn)
		hiddenTx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, nil, false)
		hiddenErr := insertEdge(t, hiddenTx, w.memTeam, w.memTeam, w.bob)
		missingTx := beginApp(t, st, w.bob, w.orgID, []string{w.teamBID}, nil, false)
		missingErr := insertEdge(t, missingTx, missing, missing, w.bob)
		if hiddenErr == nil {
			t.Fatal("edge insert naming a memory bob cannot see succeeded")
		}
		if missingErr == nil {
			t.Fatal("edge insert naming a missing memory succeeded")
		}
		if pgErrCode(hiddenErr) != pgErrCode(missingErr) {
			t.Fatalf("hidden memory error %q (%s) vs missing %q (%s): FK is an existence oracle",
				hiddenErr, pgErrCode(hiddenErr), missingErr, pgErrCode(missingErr))
		}
		if err := insertEdge(t, aliceTx, w.memTeam, w.memTeam, w.alice); err != nil {
			t.Fatalf("alice edge on her visible memory: %v", err)
		}
	})

	t.Run("ingest_receipt is caller-owned", func(t *testing.T) {
		_, err := bobTx.Exec(t.Context(), `INSERT INTO ingest_receipt (client_id, principal_id, subject_type, subject_id)
			VALUES (gen_random_uuid(), $1, 'memory', gen_random_uuid())`, w.alice)
		if err == nil {
			t.Fatal("bob claimed alice's ingest_receipt principal_id")
		}
		if _, err := aliceTx.Exec(t.Context(), `INSERT INTO ingest_receipt (client_id, principal_id, subject_type, subject_id)
			VALUES (gen_random_uuid(), $1, 'memory', gen_random_uuid())`, w.alice); err != nil {
			t.Fatalf("alice own receipt: %v", err)
		}
	})

	t.Run("memory_feedback follows parent visibility", func(t *testing.T) {
		_, err := bobTx.Exec(t.Context(), `INSERT INTO memory_feedback (id, memory_id, principal_id, trust, useful)
			VALUES (gen_random_uuid(), $1, $2, 'human', true)`, w.memTeam, w.bob)
		if err == nil {
			t.Fatal("bob left feedback on a memory he cannot see")
		}
	})
}

func TestRLSReviewItemUpdateForgery(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	st, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	erinTx := beginApp(t, st, w.erin, w.orgID, []string{w.teamAID}, nil, false)
	tag, err := erinTx.Exec(t.Context(), `UPDATE review_item SET status = 'approved', decided_by = $1 WHERE id = $2`, w.erin, w.reviewA)
	if err != nil {
		t.Fatalf("forged decide leaked an error: %v", err)
	}
	if tag.RowsAffected() != 0 {
		t.Fatal("teammate forged decided_by / status on another member's review_item")
	}

	// Own transaction: WITH CHECK failure aborts the tx, and withdraw must still run.
	aliceForge := beginApp(t, st, w.alice, w.orgID, []string{w.teamAID}, nil, false)
	tag, err = aliceForge.Exec(t.Context(), `UPDATE review_item SET status = 'approved', decided_by = $1 WHERE id = $2`, w.alice, w.reviewA)
	if err == nil && tag.RowsAffected() > 0 {
		t.Fatal("proposer self-approved a review_item")
	}

	aliceTx := beginApp(t, st, w.alice, w.orgID, []string{w.teamAID}, nil, false)
	tag, err = aliceTx.Exec(t.Context(), `UPDATE review_item SET proposed_by = $1 WHERE id = $2`, w.erin, w.reviewA)
	if err == nil && tag.RowsAffected() > 0 {
		t.Fatal("proposed_by was rewritten")
	}

	tag, err = aliceTx.Exec(t.Context(), `UPDATE review_item SET status = 'withdrawn' WHERE id = $1`, w.reviewA)
	if err != nil {
		t.Fatalf("proposer withdraw: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("proposer withdraw rows=%d, want 1", tag.RowsAffected())
	}

	adminTx := beginApp(t, st, w.admin, w.orgID, nil, nil, true)
	openID := newID(t, conn)
	mustExec(t, conn, `INSERT INTO review_item (id, kind, scope_id, team_id, payload, proposed_by)
		VALUES ($1, 'promotion', $2, $3, '{"subject":"z"}', $4)`, openID, w.projectA, w.teamAID, w.alice)
	tag, err = adminTx.Exec(t.Context(), `UPDATE review_item SET status = 'approved', decided_by = $1 WHERE id = $2`, w.admin, openID)
	if err != nil {
		t.Fatalf("human_admin decide: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("human_admin decide rows=%d, want 1", tag.RowsAffected())
	}
}

func TestRLSFrozenColumns(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedTwoTeams(t, conn)
	st, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	aliceTx := beginApp(t, st, w.alice, w.orgID, []string{w.teamAID}, nil, false)
	tag, err := aliceTx.Exec(t.Context(), `UPDATE memory SET status = 'confirmed' WHERE id = $1`, w.memTeam)
	if err != nil {
		t.Fatalf("frozen status UPDATE leaked an error: %v", err)
	}
	if tag.RowsAffected() != 0 {
		t.Fatal("owner changed memory.status; agents must not self-service status (MEM-6)")
	}

	tag, err = aliceTx.Exec(t.Context(), `UPDATE memory SET status = 'superseded' WHERE id = $1`, w.memTeam)
	if err != nil {
		t.Fatalf("superseded without superseded_by leaked an error: %v", err)
	}
	if tag.RowsAffected() != 0 {
		t.Fatal("owner set status=superseded without setting superseded_by")
	}

	tag, err = aliceTx.Exec(t.Context(), `UPDATE memory SET status = 'superseded', superseded_by = $2 WHERE id = $1`, w.memTeam, w.memOwner)
	if err != nil {
		t.Fatalf("system supersede transition leaked an error: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatal("owner could not set status=superseded while setting superseded_by in the same statement")
	}

	if err := aliceTx.Rollback(t.Context()); err != nil {
		t.Fatalf("rollback owner tx: %v", err)
	}

	adminTx := beginApp(t, st, w.admin, w.orgID, nil, nil, true)
	if _, err := adminTx.Exec(t.Context(), `UPDATE memory SET status = 'confirmed' WHERE id = $1`, w.memTeam); err != nil {
		t.Fatalf("human_admin status change: %v", err)
	}
}

func insertEdge(t *testing.T, tx pgx.Tx, from, to, actor string) error {
	t.Helper()
	_, err := tx.Exec(t.Context(), `INSERT INTO memory_edge (from_id, to_id, relation, created_by)
		VALUES ($1, $2, 'relates_to', $3)`, from, to, actor)
	return err
}

func pgErrCode(err error) string {
	var e *pgconn.PgError
	if errors.As(err, &e) {
		return e.Code
	}
	return "none"
}
