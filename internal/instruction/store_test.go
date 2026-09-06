package instruction

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/preference"
	"github.com/agentic-substrate/substrate/internal/scope"
	"github.com/agentic-substrate/substrate/internal/store"
)

type fixture struct {
	actor, orgID, teamID                  string
	global, org, team, project, userScope string
	path                                  scope.Path
}

func seedResolveWorld(t *testing.T, conn *pgx.Conn) fixture {
	t.Helper()
	id := func() string {
		t.Helper()
		var s string
		if err := conn.QueryRow(t.Context(), "SELECT gen_random_uuid()::text").Scan(&s); err != nil {
			t.Fatalf("gen_random_uuid: %v", err)
		}
		return s
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(t.Context(), q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	f := fixture{
		actor:     id(),
		orgID:     id(),
		teamID:    id(),
		global:    id(),
		org:       id(),
		team:      id(),
		project:   id(),
		userScope: id(),
		path:      mustParse(t, "global:/org:acme/team:core/project:plotlens"),
	}
	exec(`INSERT INTO principal (id, kind, display_name, trust) VALUES ($1, 'user', 'alice', 'human')`, f.actor)
	exec(`INSERT INTO org (id, name) VALUES ($1, 'acme')`, f.orgID)
	exec(`INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'core')`, f.teamID, f.orgID)
	exec(`INSERT INTO membership (principal_id, team_id, role) VALUES ($1, $2, 'member')`, f.actor, f.teamID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'global', NULL, '', 0, 'placeholder')`, f.global)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'org', $2, 'acme', 0, 'placeholder')`, f.org, f.global)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES ($1, 'team', $2, 'core', 0, 'placeholder', $3)`, f.team, f.org, f.teamID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id) VALUES ($1, 'project', $2, 'plotlens', 0, 'placeholder', $3)`, f.project, f.team, f.teamID)
	exec(`INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'user', NULL, $2, 0, 'placeholder')`, f.userScope, f.actor)

	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'constraint', 'python.version', '3.13', 'active', $2)`, f.global, f.actor)
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'constraint', 'python.version', '3.11', 'retired', $2)`, f.project, f.actor)
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'constraint', 'python.version', '3.12', 'active', $2)`, f.project, f.actor)
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'convention', 'go.module', 'github.com/acme/plotlens', 'active', $2)`, f.project, f.actor)
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'rule', 'indent', 'spaces', 'active', $2)`, f.project, f.actor)
	exec(`INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'constraint', 'python.version', '3.10', 'proposed', $2)`, f.org, f.actor)

	exec(`INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'indent', 'tabs', 'active', $2)`, f.userScope, f.actor)
	exec(`INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'editor', 'vim', 'active', $2)`, f.userScope, f.actor)
	exec(`INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'editor', 'emacs', 'active', $2)`, f.team, f.actor)
	exec(`INSERT INTO preference (id, scope_id, visibility, owner_id, key, body, status, created_by)
		VALUES (gen_random_uuid(), $1, 'global', $2, 'color', 'auto', 'retired', $2)`, f.org, f.actor)
	return f
}

func TestResolveThroughStoreTx(t *testing.T) {
	dsn, conn := startMigrated(t)
	f := seedResolveWorld(t, conn)
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	actor, err := uuid.Parse(f.actor)
	if err != nil {
		t.Fatal(err)
	}
	orgID, err := uuid.Parse(f.orgID)
	if err != nil {
		t.Fatal(err)
	}
	teamID, err := uuid.Parse(f.teamID)
	if err != nil {
		t.Fatal(err)
	}
	userScope, err := uuid.Parse(f.userScope)
	if err != nil {
		t.Fatal(err)
	}
	teamScope, err := uuid.Parse(f.team)
	if err != nil {
		t.Fatal(err)
	}
	orgScope, err := uuid.Parse(f.org)
	if err != nil {
		t.Fatal(err)
	}

	p := &identity.Principal{
		ID:      actor,
		Kind:    identity.KindUser,
		Trust:   identity.TrustHuman,
		OrgID:   orgID,
		TeamIDs: []uuid.UUID{teamID},
	}
	ctx := identity.WithPrincipal(t.Context(), p)

	var ins []Record
	var prefs []preference.Record
	var notes []preference.Suppression
	if err := st.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		ins, err = Resolve(ctx, tx, f.path)
		if err != nil {
			return err
		}
		prefs, notes, err = preference.Resolve(ctx, tx, []uuid.UUID{userScope, teamScope, orgScope}, Keys(ins))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	got := renderEffective(ins, prefs, notes)
	want := loadGolden(t, "store-effective.golden")
	if got != want {
		t.Fatalf("store resolve:\n got %q\nwant %q", got, want)
	}
}

func TestResolveWithoutPrincipalIsEmptyGate(t *testing.T) {
	dsn, _ := startMigrated(t)
	st, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)
	err = st.Tx(t.Context(), func(tx pgx.Tx) error {
		_, err := Resolve(t.Context(), tx, mustParse(t, "global:"))
		return err
	})
	if err == nil {
		t.Fatal("Tx without a principal succeeded; Resolve must not run on a bare pool")
	}
}
