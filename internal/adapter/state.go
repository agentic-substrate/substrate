package adapter

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure Go SQLite driver (EDD R11); no CGO
)

// DB is the adapter's SQLite store (~/.substrate/adapter.sqlite).
type DB struct {
	sql   *sql.DB
	drift driftBackoff
}

// DriftReportBackoffBase and MaxDriftReportBackoff bound how often a target
// whose drift cannot be proposed re-reports. A file that can never be proposed
// is re-detected every render tick forever; without a backoff that is one
// metric increment and one ERROR line per tick, per file, for as long as the
// daemon runs. The target still appears in SyncResult.UnproposedDrift on every
// cycle, so this throttles the noise without hiding the condition.
const (
	DriftReportBackoffBase = DefaultInterval
	MaxDriftReportBackoff  = time.Hour
)

// driftBackoff is per-process, not persisted: a daemon restart should report
// the current state of the world once, and a fresh SyncResult carries the file
// regardless.
type driftBackoff struct {
	mu    sync.Mutex
	state map[string]*driftReport
}

type driftReport struct {
	nextAt     int64
	reports    int
	suppressed int
}

// shouldReportDrift reports whether this cycle may log and count an
// unproposable target, and how many cycles were suppressed since the last one.
func (db *DB) shouldReportDrift(path string, now int64) (bool, int) {
	if db == nil {
		return true, 0
	}
	db.drift.mu.Lock()
	defer db.drift.mu.Unlock()
	if db.drift.state == nil {
		db.drift.state = map[string]*driftReport{}
	}
	st := db.drift.state[path]
	if st == nil {
		st = &driftReport{}
		db.drift.state[path] = st
	}
	if st.reports > 0 && now < st.nextAt {
		st.suppressed++
		return false, st.suppressed
	}
	st.reports++
	suppressed := st.suppressed
	st.suppressed = 0
	st.nextAt = now + int64(driftBackoffFor(st.reports).Seconds())
	return true, suppressed
}

// clearDriftBackoff forgets a path whose proposal landed, so a later, unrelated
// failure on the same file reports immediately.
func (db *DB) clearDriftBackoff(path string) {
	if db == nil {
		return
	}
	db.drift.mu.Lock()
	defer db.drift.mu.Unlock()
	delete(db.drift.state, path)
}

func driftBackoffFor(reports int) time.Duration {
	wait := DriftReportBackoffBase
	for i := 1; i < reports && wait < MaxDriftReportBackoff; i++ {
		wait *= 2
	}
	if wait > MaxDriftReportBackoff {
		return MaxDriftReportBackoff
	}
	return wait
}

// Workspace is a discovered checkout (EDD R26).
type Workspace struct {
	Path   string
	Remote string
	Branch string
}

// schema is EDD §7.1 plus workspace (R26) and a kv cursor for cache since=.
// next_attempt_at is the drain backoff cursor so Drain never sleeps (Gotcha 8).
const schema = `
CREATE TABLE IF NOT EXISTS managed_file (
	path TEXT PRIMARY KEY,
	target TEXT NOT NULL,
	sha256 TEXT NOT NULL,
	rendered_at INTEGER NOT NULL,
	render_version INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS workspace (
	worktree_path TEXT PRIMARY KEY,
	remote TEXT NOT NULL DEFAULT '',
	branch TEXT NOT NULL DEFAULT '',
	last_seen INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS outbox (
	id INTEGER PRIMARY KEY,
	client_id TEXT UNIQUE NOT NULL,
	payload TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	next_attempt_at INTEGER NOT NULL DEFAULT 0,
	consecutive_4xx INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS outbox_dead (
	client_id TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	dead_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS memory_cache (
	id TEXT PRIMARY KEY,
	scope_path TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	identifiers TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE VIRTUAL TABLE IF NOT EXISTS memory_cache_fts USING fts5(
	title,
	body,
	identifiers,
	content='memory_cache',
	content_rowid='rowid'
);
CREATE TRIGGER IF NOT EXISTS memory_cache_ai AFTER INSERT ON memory_cache BEGIN
	INSERT INTO memory_cache_fts(rowid, title, body, identifiers)
	VALUES (new.rowid, new.title, new.body, new.identifiers);
END;
CREATE TRIGGER IF NOT EXISTS memory_cache_ad AFTER DELETE ON memory_cache BEGIN
	INSERT INTO memory_cache_fts(memory_cache_fts, rowid, title, body, identifiers)
	VALUES ('delete', old.rowid, old.title, old.body, old.identifiers);
END;
CREATE TRIGGER IF NOT EXISTS memory_cache_au AFTER UPDATE ON memory_cache BEGIN
	INSERT INTO memory_cache_fts(memory_cache_fts, rowid, title, body, identifiers)
	VALUES ('delete', old.rowid, old.title, old.body, old.identifiers);
	INSERT INTO memory_cache_fts(rowid, title, body, identifiers)
	VALUES (new.rowid, new.title, new.body, new.identifiers);
END;
CREATE TABLE IF NOT EXISTS kv (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS skill_link (
	name TEXT PRIMARY KEY,
	git_sha TEXT NOT NULL,
	linked_paths TEXT NOT NULL DEFAULT ''
);
`

// Open opens the adapter SQLite file, creating it and the schema as needed.
func Open(path string) (*DB, error) {
	return openSQLite(path, DefaultBusyTimeout)
}

// OpenHook opens adapter.sqlite with the short busy_timeout used by hooks
// (Gotcha 8). The daemon uses Open.
func OpenHook(path string) (*DB, error) {
	return openSQLite(path, HookBusyTimeout)
}

func openSQLite(path string, busy time.Duration) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("adapter: state dir: %w", err)
	}
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("adapter: sqlite open: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	ms := int(busy / time.Millisecond)
	if ms < 1 {
		ms = 1
	}
	if _, err := sqlDB.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("adapter: pragma wal: %w", err)
	}
	if _, err := sqlDB.Exec(fmt.Sprintf(`PRAGMA busy_timeout=%d`, ms)); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("adapter: pragma busy_timeout: %w", err)
	}
	if _, err := sqlDB.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("adapter: pragma: %w", err)
	}
	if _, err := sqlDB.Exec(schema); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("adapter: schema: %w", err)
	}
	// Existing adapter.sqlite files created before consecutive_4xx.
	_, _ = sqlDB.Exec(`ALTER TABLE outbox ADD COLUMN consecutive_4xx INTEGER NOT NULL DEFAULT 0`)
	// Existing rows predate the per-checkout render split (#111), so they
	// default to 0 -- older than any RenderVersion the code knows. That
	// default is what makes the first post-upgrade sync recognisable (#112).
	_, _ = sqlDB.Exec(`ALTER TABLE managed_file ADD COLUMN render_version INTEGER NOT NULL DEFAULT 0`)
	return &DB{sql: sqlDB}, nil
}

// Close releases the SQLite handle.
func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

type managedRow struct {
	Target string
	SHA256 string
	// RenderVersion is the RenderVersion in force when this file was last
	// written. A row older than the current one means the rules that produce
	// the content changed underneath a file the adapter itself wrote, which
	// drift review cannot see: disk still matches the stored hash (#112).
	RenderVersion int
}

func (db *DB) getManaged(path string) (managedRow, bool, error) {
	var row managedRow
	err := db.sql.QueryRow(`SELECT target, sha256, render_version FROM managed_file WHERE path = ?`, path).
		Scan(&row.Target, &row.SHA256, &row.RenderVersion)
	if err == sql.ErrNoRows {
		return managedRow{}, false, nil
	}
	if err != nil {
		return managedRow{}, false, fmt.Errorf("adapter: managed_file lookup: %w", err)
	}
	return row, true, nil
}

func (db *DB) upsertManaged(path, target, sum string, renderedAt int64) error {
	_, err := db.sql.Exec(`
		INSERT INTO managed_file (path, target, sha256, rendered_at, render_version)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET
			target = excluded.target,
			sha256 = excluded.sha256,
			rendered_at = excluded.rendered_at,
			render_version = excluded.render_version
	`, path, target, sum, renderedAt, RenderVersion)
	if err != nil {
		return fmt.Errorf("adapter: managed_file upsert: %w", err)
	}
	return nil
}

func (db *DB) upsertWorkspace(ws Workspace, seen int64) error {
	_, err := db.sql.Exec(`
		INSERT INTO workspace (worktree_path, remote, branch, last_seen)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (worktree_path) DO UPDATE SET
			remote = excluded.remote,
			branch = excluded.branch,
			last_seen = excluded.last_seen
	`, ws.Path, ws.Remote, ws.Branch, seen)
	if err != nil {
		return fmt.Errorf("adapter: workspace upsert: %w", err)
	}
	return nil
}

func (db *DB) pruneWorkspaces(keep map[string]struct{}) error {
	rows, err := db.sql.Query(`SELECT worktree_path FROM workspace`)
	if err != nil {
		return fmt.Errorf("adapter: workspace prune: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var drop []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return fmt.Errorf("adapter: workspace prune: %w", err)
		}
		if _, ok := keep[path]; !ok {
			drop = append(drop, path)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("adapter: workspace prune: %w", err)
	}
	for _, path := range drop {
		if _, err := db.sql.Exec(`DELETE FROM workspace WHERE worktree_path = ?`, path); err != nil {
			return fmt.Errorf("adapter: workspace prune: %w", err)
		}
	}
	return nil
}

// Workspaces returns discovered checkouts, one row per worktree_path.
func (db *DB) Workspaces() ([]Workspace, error) {
	rows, err := db.sql.Query(`SELECT worktree_path, remote, branch FROM workspace ORDER BY worktree_path`)
	if err != nil {
		return nil, fmt.Errorf("adapter: workspace list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.Path, &ws.Remote, &ws.Branch); err != nil {
			return nil, fmt.Errorf("adapter: workspace scan: %w", err)
		}
		out = append(out, ws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("adapter: workspace rows: %w", err)
	}
	return out, nil
}

type skillLinkRow struct {
	Name        string
	GitSHA      string
	LinkedPaths string
}

func (db *DB) getSkillLink(name string) (skillLinkRow, bool, error) {
	var row skillLinkRow
	err := db.sql.QueryRow(`SELECT name, git_sha, linked_paths FROM skill_link WHERE name = ?`, name).Scan(&row.Name, &row.GitSHA, &row.LinkedPaths)
	if err == sql.ErrNoRows {
		return skillLinkRow{}, false, nil
	}
	if err != nil {
		return skillLinkRow{}, false, fmt.Errorf("adapter: skill_link lookup: %w", err)
	}
	return row, true, nil
}

func (db *DB) upsertSkillLink(name, sha, paths string) error {
	_, err := db.sql.Exec(`
		INSERT INTO skill_link (name, git_sha, linked_paths)
		VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			git_sha = excluded.git_sha,
			linked_paths = excluded.linked_paths
	`, name, sha, paths)
	if err != nil {
		return fmt.Errorf("adapter: skill_link upsert: %w", err)
	}
	return nil
}

func (db *DB) listSkillLinks() ([]skillLinkRow, error) {
	rows, err := db.sql.Query(`SELECT name, git_sha, linked_paths FROM skill_link ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("adapter: skill_link list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []skillLinkRow
	for rows.Next() {
		var row skillLinkRow
		if err := rows.Scan(&row.Name, &row.GitSHA, &row.LinkedPaths); err != nil {
			return nil, fmt.Errorf("adapter: skill_link scan: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("adapter: skill_link rows: %w", err)
	}
	return out, nil
}

func (db *DB) deleteSkillLink(name string) error {
	if _, err := db.sql.Exec(`DELETE FROM skill_link WHERE name = ?`, name); err != nil {
		return fmt.Errorf("adapter: skill_link delete: %w", err)
	}
	return nil
}
