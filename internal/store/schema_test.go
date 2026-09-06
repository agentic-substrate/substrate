package store

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
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
	if err := Migrate(ctx, dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return dsn, conn
}

func mustExec(t *testing.T, conn *pgx.Conn, q string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(t.Context(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func mustFail(t *testing.T, conn *pgx.Conn, q string, args ...any) error {
	t.Helper()
	_, err := conn.Exec(t.Context(), q, args...)
	if err == nil {
		t.Fatalf("exec %q succeeded; want an error", q)
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func newID(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var id string
	if err := conn.QueryRow(t.Context(), "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
		t.Fatalf("gen_random_uuid: %v", err)
	}
	return id
}

type chain struct {
	actor, global, org, team, project, repo, userScope string
	orgID, teamID                                      string
}

func seedChain(t *testing.T, conn *pgx.Conn) chain {
	t.Helper()
	c := chain{
		actor:     newID(t, conn),
		global:    newID(t, conn),
		org:       newID(t, conn),
		team:      newID(t, conn),
		project:   newID(t, conn),
		repo:      newID(t, conn),
		userScope: newID(t, conn),
		orgID:     newID(t, conn),
		teamID:    newID(t, conn),
	}
	mustExec(t, conn, `INSERT INTO principal (id, kind, display_name, trust) VALUES ($1, 'user', 'test', 'human')`, c.actor)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'global', NULL, '', 0, 'placeholder')`, c.global)
	mustExec(t, conn, `INSERT INTO org (id, name) VALUES ($1, 'acme')`, c.orgID)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'org', $2, 'acme', 0, 'placeholder')`, c.org, c.global)
	mustExec(t, conn, `INSERT INTO team (id, org_id, name) VALUES ($1, $2, 'core')`, c.teamID, c.orgID)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'team', $2, 'core', 0, 'placeholder')`, c.team, c.org)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'project', $2, 'plotlens', 0, 'placeholder')`, c.project, c.team)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'repo', $2, 'plotlens/api', 0, 'placeholder')`, c.repo, c.project)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'user', NULL, 'jeremy', 0, 'placeholder')`, c.userScope)
	return c
}

func TestSchemaInvariants(t *testing.T) {
	_, conn := startMigrated(t)
	c := seedChain(t, conn)

	t.Run("bad parent kind is rejected", func(t *testing.T) {
		// project under global skips team — EDD §3.1 requires the immediate predecessor.
		err := mustFail(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES (gen_random_uuid(), 'project', $1, 'skipped', 0, 'placeholder')`, c.global)
		if !strings.Contains(err.Error(), "predecessor") && !strings.Contains(err.Error(), "parent") {
			t.Fatalf("error %q does not mention the parent-kind rule", err)
		}
	})

	t.Run("two root scopes of the same kind and key collide", func(t *testing.T) {
		err := mustFail(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES (gen_random_uuid(), 'user', NULL, 'jeremy', 0, 'placeholder')`)
		if !isUniqueViolation(err) {
			t.Fatalf("want unique violation, got %v", err)
		}
	})

	t.Run("second global is rejected", func(t *testing.T) {
		err := mustFail(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES (gen_random_uuid(), 'global', NULL, 'other', 0, 'placeholder')`)
		if !isUniqueViolation(err) {
			t.Fatalf("want unique violation for a second global, got %v", err)
		}
	})

	t.Run("second active instruction on the same scope and key collides", func(t *testing.T) {
		mustExec(t, conn, `INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, created_by)
			VALUES (gen_random_uuid(), $1, 'owner', $2, 'rule', 'python.version', '3.12', $2)`, c.project, c.actor)
		err := mustFail(t, conn, `INSERT INTO instruction (id, scope_id, visibility, owner_id, kind, key, body, created_by)
			VALUES (gen_random_uuid(), $1, 'owner', $2, 'rule', 'python.version', '3.13', $2)`, c.project, c.actor)
		if !isUniqueViolation(err) {
			t.Fatalf("want unique violation, got %v", err)
		}
	})

	t.Run("active_version_id cannot point at an unapproved version", func(t *testing.T) {
		skillID := newID(t, conn)
		verID := newID(t, conn)
		mustExec(t, conn, `INSERT INTO skill (id, name, scope_id, visibility, owner_id, description)
			VALUES ($1, 'team/plotlens/validation', $2, 'team', $3, 'd')`, skillID, c.project, c.actor)
		mustExec(t, conn, `INSERT INTO skill_version (id, skill_id, semver, git_sha, git_path, author_id, approval)
			VALUES ($1, $2, '1.0.0', 'abc', 'skills/team/plotlens/validation', $3, 'proposed')`, verID, skillID, c.actor)
		err := mustFail(t, conn, `UPDATE skill SET active_version_id = $1 WHERE id = $2`, verID, skillID)
		if !strings.Contains(strings.ToLower(err.Error()), "approved") {
			t.Fatalf("error %q does not mention approval", err)
		}
	})

	t.Run("update on audit is refused", func(t *testing.T) {
		mustExec(t, conn, `INSERT INTO audit (action, subject_type, subject_id) VALUES ('memory.write', 'memory', gen_random_uuid())`)
		_, err := conn.Exec(t.Context(), `SET ROLE substrate_app`)
		if err != nil {
			t.Fatalf("SET ROLE substrate_app: %v", err)
		}
		t.Cleanup(func() { _, _ = conn.Exec(context.Background(), `RESET ROLE`) })
		_, err = conn.Exec(t.Context(), `UPDATE audit SET action = 'tamper'`)
		if err == nil {
			t.Fatal("UPDATE audit succeeded; the table must be INSERT-only for substrate_app")
		}
	})
}

func TestUserIsNotAChainLevel(t *testing.T) {
	_, conn := startMigrated(t)
	c := seedChain(t, conn)

	t.Run("user cannot have a parent", func(t *testing.T) {
		_ = mustFail(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES (gen_random_uuid(), 'user', $1, 'child', 0, 'placeholder')`, c.global)
	})

	t.Run("user cannot have children", func(t *testing.T) {
		_ = mustFail(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES (gen_random_uuid(), 'org', $1, 'under-user', 0, 'placeholder')`, c.userScope)
	})
}

func TestPathIsUUIDDerived(t *testing.T) {
	_, conn := startMigrated(t)
	c := seedChain(t, conn)
	id := newID(t, conn)
	mustExec(t, conn, `INSERT INTO scope (id, kind, parent_id, key, depth, path) VALUES ($1, 'branch', $2, 'feature/x.y z', 0, 'placeholder')`, id, c.repo)
	var path string
	if err := conn.QueryRow(t.Context(), `SELECT path::text FROM scope WHERE id = $1`, id).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(path, "/") || strings.Contains(path, " ") || strings.Contains(path, "feature") {
		t.Fatalf("path %q was derived from the human key; labels must be UUID-based (EDD R23)", path)
	}
	want := "k" + strings.ReplaceAll(id, "-", "")
	if !strings.HasSuffix(path, want) {
		t.Fatalf("path %q does not end with UUID label %q", path, want)
	}
}

func TestVerificationTypeIsGenerated(t *testing.T) {
	_, conn := startMigrated(t)
	c := seedChain(t, conn)
	id := newID(t, conn)
	mustExec(t, conn, `INSERT INTO memory (id, scope_id, visibility, owner_id, tier, kind, title, body, source, verification, created_by)
		VALUES ($1, $2, 'owner', $3, 'semantic', 'fact', 't', 'b', '{"machine":"wsl"}', '{"type":"human"}', $3)`, id, c.project, c.actor)
	var vt string
	if err := conn.QueryRow(t.Context(), `SELECT verification_type FROM memory WHERE id = $1`, id).Scan(&vt); err != nil {
		t.Fatal(err)
	}
	if vt != "human" {
		t.Fatalf("verification_type = %q, want human", vt)
	}
}

func TestFeedbackTrustWeights(t *testing.T) {
	_, conn := startMigrated(t)
	c := seedChain(t, conn)
	mem := newID(t, conn)
	mustExec(t, conn, `INSERT INTO memory (id, scope_id, visibility, owner_id, tier, kind, title, body, source, verification, created_by)
		VALUES ($1, $2, 'owner', $3, 'working', 'observation', 't', 'b', '{"machine":"wsl"}', '{"type":"agent_inference"}', $3)`, mem, c.project, c.actor)
	mustExec(t, conn, `INSERT INTO memory_feedback (id, memory_id, principal_id, trust, useful)
		VALUES (gen_random_uuid(), $1, $2, 'human_admin', true)`, mem, c.actor)
	var useful int
	if err := conn.QueryRow(t.Context(), `SELECT useful_count FROM memory WHERE id = $1`, mem).Scan(&useful); err != nil {
		t.Fatal(err)
	}
	if useful != 3 {
		t.Fatalf("useful_count = %d, want 3 (human_admin weight)", useful)
	}
}

func TestAppRoleHasNoBypassRLSAndDoesNotOwnTables(t *testing.T) {
	_, conn := startMigrated(t)
	var bypass bool
	if err := conn.QueryRow(t.Context(), `SELECT rolbypassrls FROM pg_roles WHERE rolname = 'substrate_app'`).Scan(&bypass); err != nil {
		t.Fatal(err)
	}
	if bypass {
		t.Fatal("substrate_app must not have BYPASSRLS")
	}
	var owner string
	if err := conn.QueryRow(t.Context(), `SELECT pg_catalog.pg_get_userbyid(relowner) FROM pg_class WHERE relname = 'memory' AND relkind = 'r'`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner == "substrate_app" {
		t.Fatal("substrate_app must not own the tables")
	}
	if owner != "substrate_migrate" {
		t.Fatalf("memory owner = %q, want substrate_migrate", owner)
	}
}

func TestDownThenUp(t *testing.T) {
	dsn, conn := startMigrated(t)
	_ = conn.Close(t.Context())
	ctx := t.Context()
	if err := migrateDown(ctx, dsn); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := Migrate(ctx, dsn); err != nil {
		t.Fatalf("up after down: %v", err)
	}
	conn2, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn2.Close(context.Background()) }()
	var n int
	if err := conn2.QueryRow(ctx, `SELECT count(*) FROM scope`).Scan(&n); err != nil {
		t.Fatalf("scope missing after down/up: %v", err)
	}
}
