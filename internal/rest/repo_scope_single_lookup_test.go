package rest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A repo key does not name one row. `scope` is
// UNIQUE NULLS NOT DISTINCT (parent_id, kind, key) (migrations/00002_schema.sql:38),
// so the same key may be bound under two parents, and repoScope used to look the
// key up twice: once to walk the chain, once more to ask scope_writable. Both
// lookups were unordered LIMIT 1, and under READ COMMITTED the second takes a
// fresh snapshot -- so a bind committed by another session in between silently
// moved the gate onto a different row than the one the chain came from (#110).
//
// The seam that makes that window deterministic instead of a race: scope_readable
// is redefined to sleep, holding repoScope inside its first statement while this
// test commits the competing bind on its own connection. The competing parent id
// sorts first, so a second lookup of the key returns the competing row. alice can
// write the row the chain came from and not the competing one, so the old
// two-lookup code refuses an import it must accept.
//
// Mutation that turns this red again: put the `FROM scope WHERE key = $1 LIMIT 1`
// subquery back in repoScope instead of passing refs[0].ID to scope_writable.
func TestRepoScopeGatesTheRowItResolved(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedWorld(t, conn)
	h, _, _ := openREST(t, dsn, fakeGit{})
	srv := serveREST(t, h, w)
	ctx := context.Background()

	// Sorts before seedWorld's random project id, so an index scan of the key
	// returns the competing row first once it is visible.
	competingProject := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	competingRepo := uuid.MustParse("00000000-0000-4000-8000-000000000002")

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("setup %q: %v", sql, err)
		}
	}

	mustExec(`CREATE OR REPLACE FUNCTION scope_readable(p_scope_id uuid, p_actor uuid)
		RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path = public AS $fn$
		SELECT pg_sleep(1) IS NOT NULL AND scope_writable(p_scope_id, p_actor) $fn$`)
	t.Cleanup(func() {
		_, _ = conn.Exec(ctx, `CREATE OR REPLACE FUNCTION scope_readable(p_scope_id uuid, p_actor uuid)
			RETURNS boolean LANGUAGE sql STABLE PARALLEL SAFE SET search_path = public AS $fn$
			SELECT scope_writable(p_scope_id, p_actor) $fn$`)
	})

	type result struct {
		code int
		body string
	}
	done := make(chan result, 1)
	go func() {
		commit := true
		res := doJSON(t, srv, http.MethodPost, "/v1/import", "alice", importBody(w.repoKey, "", &commit))
		done <- result{res.StatusCode, readBody(t, res)}
	}()

	// The request is now parked inside scope_readable. Team beta binds the same
	// repo key and commits, exactly as a second control-plane session would.
	time.Sleep(300 * time.Millisecond)
	mustExec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id)
		VALUES ($1,'project',$2,'dup',0,'placeholder',$3)`, competingProject, w.teamB, w.teamBID)
	mustExec(`INSERT INTO scope (id, kind, parent_id, key, depth, path, team_id)
		VALUES ($1,'repo',$2,$3,0,'placeholder',$4)`, competingRepo, competingProject, w.repoKey, w.teamBID)

	got := <-done
	if got.code != http.StatusCreated && got.code != http.StatusOK {
		t.Fatalf("import of a repo alice can write = %d, want 200 or 201; the writability gate "+
			"ran against a row the chain did not come from:\n%s", got.code, got.body)
	}

	// Anti-vacuity: without a visible competing row this test proves nothing.
	var seen bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM scope WHERE id = $1)`, competingRepo).Scan(&seen); err != nil {
		t.Fatalf("check competing row: %v", err)
	}
	if !seen {
		t.Fatal("the competing bind never landed, so the two lookups never had a chance to disagree")
	}
}
