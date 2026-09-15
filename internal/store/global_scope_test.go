package store_test

import (
	"testing"

	"github.com/agentic-substrate/substrate/internal/pgtest"
)

// Every read path resolves the global chain (internal/rest/resolve.go
// globalPath), so a database without the global scope row answers 500 on
// /v1/render — which is the first request `substrate auth login` makes. The
// only thing that ever inserted it was the test fixture, so CI was green and
// every fresh install was broken.
//
// Red when: no migration seeds the global scope.
func TestMigrationsSeedTheSingleGlobalScope(t *testing.T) {
	_, conn := pgtest.StartMigrated(t)
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM scope WHERE kind = 'global'`).Scan(&n); err != nil {
		t.Fatalf("count global scopes: %v", err)
	}
	if n != 1 {
		t.Fatalf("global scopes after migrate = %d, want exactly 1", n)
	}
}
