package store

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/scope"
)

// TestValidateAgreesWithScopeChainTrigger runs the same wire paths through
// scope.Parse (Validate) and a real INSERT into scope. The two verdicts must
// match. Skipped-level paths are the point of the table: Validate currently
// accepts them and the trigger (EDD §3.1, immediate predecessor) does not.
//
// Inserts run as the connected postgres role, which is permitted to write
// scope. A permission denial or RLS empty-result would masquerade as the
// trigger rejecting the path.
func TestValidateAgreesWithScopeChainTrigger(t *testing.T) {
	_, conn := startMigrated(t)

	cases := []struct {
		name string
		path string
	}{
		{"global only", "global:"},
		{"org", "global:/org:acme"},
		{"team", "global:/org:acme/team:core"},
		{"project", "global:/org:acme/team:core/project:plotlens"},
		{"repo", "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi"},
		{"branch", "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi/branch:main"},
		{"task", "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi/branch:main/task:t1"},
		{"session", "global:/org:acme/team:core/project:plotlens/repo:plotlens%2Fapi/branch:main/task:t1/session:s1"},

		{"skip to project", "global:/project:plotlens"},
		{"skip team", "global:/org:acme/project:plotlens"},
		{"skip org", "global:/team:core"},
		{"skip project", "global:/org:acme/team:core/repo:api"},
		{"skip repo", "global:/org:acme/team:core/project:plotlens/branch:main"},
		{"skip branch", "global:/org:acme/team:core/project:plotlens/repo:api/task:t1"},
		{"skip task", "global:/org:acme/team:core/project:plotlens/repo:api/branch:main/session:s1"},
		{"skip all to session", "global:/session:s1"},

		{"unrooted", "project:plotlens"},
		{"decreasing", "global:/repo:a%2Fb/project:plotlens"},
		{"repeated kind", "global:/team:a/team:b"},
		{"user in chain", "global:/user:jeremy"},
		{"unknown kind", "global:/planet:earth"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, parseErr := scope.Parse(tc.path)
			goOK := parseErr == nil
			dbOK := insertChain(t, conn, tc.path)
			if goOK != dbOK {
				t.Fatalf("path %q: Validate() accepted=%v, scope_chain insert accepted=%v", tc.path, goOK, dbOK)
			}
		})
	}
}

func insertChain(t *testing.T, conn *pgx.Conn, wire string) bool {
	t.Helper()
	ctx := t.Context()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var parent any
	for _, part := range strings.Split(wire, "/") {
		kind, name, ok := strings.Cut(part, ":")
		if !ok {
			return false
		}
		name = strings.ReplaceAll(name, "%2F", "/")
		var id string
		if err := tx.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
			t.Fatalf("gen_random_uuid: %v", err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, $2, $3, $4, 0, 'placeholder')`,
			id, kind, parent, name)
		if err != nil {
			return false
		}
		parent = id
	}
	return true
}
