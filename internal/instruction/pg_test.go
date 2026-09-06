package instruction

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
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
	dir := filepath.Join(repoRoot(), "testdata", "pgvector")
	//nolint:gosec // build context is the repo's testdata/pgvector, not user input
	build := exec.Command("docker", "build", "-t", localPGImage, dir)
	out, err = build.CombinedOutput()
	if err != nil {
		t.Fatalf("postgres 16 with pgvector is unavailable: pull %s failed and building testdata/pgvector failed: %v\n%s\nFix: install Docker, start the daemon, and either pull %s or allow a build of testdata/pgvector FROM postgres:16-alpine.", officialPGImage, err, out, officialPGImage)
	}
	return localPGImage
}

func startMigrated(t *testing.T) (string, *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	image := postgresImage(t)
	ctr, err := postgres.Run(ctx, image,
		postgres.WithDatabase("substrate"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("postgres 16 testcontainer failed to start: %v\nFix: install Docker, start the daemon, and ensure it can pull %s or build testdata/pgvector (Postgres 16 with pgvector; migrations create pg_trgm and ltree).", err, officialPGImage)
	}
	t.Cleanup(func() {
		if err := ctr.Terminate(context.Background()); err != nil {
			t.Errorf("terminate postgres: %v", err)
		}
	})
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}
	if err := store.Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return dsn, conn
}
