package store_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/agentic-substrate/substrate/internal/pgtest"
	"github.com/agentic-substrate/substrate/internal/store"
)

// Production does not migrate as a superuser: CNPG's initdb owner `app` is a
// plain database owner, and the two roles already exist (managed.roles) with
// `app` granted into both. Every other fixture migrates as `postgres`, which
// skips the CREATEROLE, ADMIN and schema-CREATE checks, so a migration that
// needs any of them passed CI and failed its first real sync with 42501.
func TestMigrateAsNonSuperuserDatabaseOwner(t *testing.T) {
	dsn, conn := pgtest.StartMigrated(t)
	ctx := t.Context()

	// The shared container already created substrate_migrate and substrate_app
	// cluster-wide, which is exactly the pre-created state CNPG leaves.
	for _, stmt := range []string{
		`DROP DATABASE IF EXISTS substrate_owner_test`,
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'owner_test') THEN CREATE ROLE owner_test LOGIN PASSWORD 'test'; END IF; END $$`,
		`CREATE DATABASE substrate_owner_test OWNER owner_test`,
		`GRANT substrate_migrate, substrate_app TO owner_test`,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	// CNPG creates the extensions as superuser in postInitApplicationSQL;
	// CREATE EXTENSION vector is not trusted, so mirror that step.
	su := *u
	su.Path = "/substrate_owner_test"
	suConn, err := pgx.Connect(ctx, su.String())
	if err != nil {
		t.Fatalf("connect as superuser: %v", err)
	}
	t.Cleanup(func() { _ = suConn.Close(context.Background()) })
	for _, ext := range []string{"vector", "pg_trgm", "ltree"} {
		if _, err := suConn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS "+ext); err != nil {
			t.Fatalf("create extension %s: %v", ext, err)
		}
	}

	owner := su
	owner.User = url.UserPassword("owner_test", "test")
	if err := store.Migrate(ctx, owner.String()); err != nil {
		t.Fatalf("migrate as non-superuser database owner: %v", err)
	}
}
