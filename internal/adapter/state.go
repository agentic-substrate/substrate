package adapter

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure Go SQLite driver (EDD R11); no CGO
)

// DB is the adapter's SQLite store (~/.substrate/adapter.sqlite).
type DB struct {
	sql *sql.DB
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
	rendered_at INTEGER NOT NULL
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
	next_attempt_at INTEGER NOT NULL DEFAULT 0
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
`

// Open opens the adapter SQLite file, creating it and the schema as needed.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("adapter: state dir: %w", err)
	}
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("adapter: sqlite open: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec(`PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("adapter: pragma: %w", err)
	}
	if _, err := sqlDB.Exec(schema); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("adapter: schema: %w", err)
	}
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
}

func (db *DB) getManaged(path string) (managedRow, bool, error) {
	var row managedRow
	err := db.sql.QueryRow(`SELECT target, sha256 FROM managed_file WHERE path = ?`, path).Scan(&row.Target, &row.SHA256)
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
		INSERT INTO managed_file (path, target, sha256, rendered_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET
			target = excluded.target,
			sha256 = excluded.sha256,
			rendered_at = excluded.rendered_at
	`, path, target, sum, renderedAt)
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
