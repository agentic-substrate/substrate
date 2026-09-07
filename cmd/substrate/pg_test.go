package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/agentic-substrate/substrate/internal/store"
)

const officialPGImage = "pgvector/pgvector:pg16"
const localPGImage = "substrate-test-pgvector:16"

func dockerImageExists(name string) bool {
	//nolint:gosec // image name is a package constant, not user input
	cmd := exec.Command("docker", "image", "inspect", name)
	return cmd.Run() == nil
}

func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func postgresImage(t *testing.T) string {
	t.Helper()
	if dockerImageExists(officialPGImage) {
		return officialPGImage
	}
	if dockerImageExists(localPGImage) {
		return localPGImage
	}
	pull := exec.Command("docker", "pull", officialPGImage)
	out, err := pull.CombinedOutput()
	if err == nil {
		return officialPGImage
	}
	t.Logf("docker pull %s failed: %v\n%s", officialPGImage, err, out)
	dir := filepath.Join(repoRoot(), "testdata/pgvector")
	//nolint:gosec // build context is the repo's testdata/pgvector, not user input
	build := exec.Command("docker", "build", "-t", localPGImage, dir)
	out, err = build.CombinedOutput()
	if err != nil {
		t.Fatalf("postgres 16 with pgvector is unavailable: pull %s failed and building testdata/pgvector failed: %v\n%s\nFix: install Docker, start the daemon, and either pull %s or allow a build of testdata/pgvector FROM postgres:16-alpine.", officialPGImage, err, out, officialPGImage)
	}
	return localPGImage
}

var (
	pgOnce sync.Once
	pgDSN  string
	pgErr  error
)

func startMigrated(t *testing.T) (string, *pgx.Conn) {
	t.Helper()
	pgOnce.Do(func() {
		ctx := context.Background()
		image := postgresImage(t)
		ctr, err := postgres.Run(ctx, image,
			postgres.WithDatabase("substrate"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("test"),
			postgres.BasicWaitStrategies(),
		)
		if err != nil {
			pgErr = err
			return
		}
		dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			pgErr = err
			return
		}
		if err := store.Migrate(ctx, dsn); err != nil {
			pgErr = err
			return
		}
		pgDSN = dsn
	})
	if pgErr != nil {
		t.Fatalf("postgres 16 testcontainer failed to start: %v\nFix: install Docker, start the daemon, and ensure it can pull %s or build testdata/pgvector (Postgres 16 with pgvector; migrations create pg_trgm and ltree).", pgErr, officialPGImage)
	}
	conn, err := pgx.Connect(t.Context(), pgDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return pgDSN, conn
}

var (
	globalOnce sync.Once
	globalID   string
	globalErr  error
)

func ensureGlobal(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	globalOnce.Do(func() {
		var id string
		if err := conn.QueryRow(context.Background(), "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
			globalErr = err
			return
		}
		if _, err := conn.Exec(context.Background(), `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'global', NULL, '', 0, 'placeholder')`, id); err != nil {
			globalErr = err
			return
		}
		globalID = id
	})
	if globalErr != nil {
		t.Fatalf("seed global scope: %v", globalErr)
	}
	return globalID
}
