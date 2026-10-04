package artifacts

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

const schema = `
CREATE TABLE artifacts (
 id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, space_id TEXT NOT NULL,
 repo_id TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('memory','skill','agent-definition')),
 lifecycle TEXT NOT NULL CHECK(lifecycle IN ('active','retired')), head TEXT NOT NULL
);
CREATE INDEX artifact_scope ON artifacts(owner_id, space_id, repo_id);
CREATE TABLE revisions (
 id TEXT PRIMARY KEY, artifact_id TEXT NOT NULL REFERENCES artifacts(id),
 base TEXT NOT NULL, content TEXT NOT NULL, provenance TEXT NOT NULL,
 author_id TEXT NOT NULL, state TEXT NOT NULL, verification TEXT NOT NULL,
 source TEXT NOT NULL
);
CREATE INDEX artifact_revisions ON revisions(artifact_id);
CREATE TABLE contributions (
 owner_id TEXT NOT NULL, space_id TEXT NOT NULL, repo_id TEXT NOT NULL,
 operation_id TEXT NOT NULL, fingerprint TEXT NOT NULL, receipt TEXT NOT NULL,
 PRIMARY KEY(owner_id, space_id, repo_id, operation_id)
);
CREATE TABLE pending (
 owner_id TEXT NOT NULL, space_id TEXT NOT NULL, repo_id TEXT NOT NULL,
 operation_id TEXT NOT NULL, artifact_id TEXT NOT NULL REFERENCES artifacts(id),
 revision_id TEXT NOT NULL, action TEXT NOT NULL,
 PRIMARY KEY(owner_id, space_id, repo_id, operation_id)
);
PRAGMA user_version=1;
`

func Open(auth *authority.Store) (*Store, error) {
	if auth == nil {
		return nil, ErrUnavailable
	}
	if _, err := auth.Inventory(); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(auth.Dir, "artifacts.db"))
	if err != nil {
		return nil, ErrUnavailable
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if err := privateFile(path+suffix, false); err != nil {
			return nil, ErrUnavailable
		}
	}
	if err := privateFile(path, true); err != nil {
		return nil, ErrUnavailable
	}
	query := url.Values{"mode": {"rw"}, "_txlock": {"immediate"}, "_pragma": {"busy_timeout(5000)", "journal_mode(DELETE)", "synchronous(EXTRA)", "foreign_keys(ON)"}}
	db, err := driver.Open((&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String(), fts5.Register)
	if err != nil {
		return nil, ErrUnavailable
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, authority: auth}, nil
}

func privateFile(path string, create bool) error {
	flags := syscall.O_RDWR | syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_NONBLOCK
	if create {
		flags |= syscall.O_CREAT
	}
	fd, err := syscall.Open(path, flags, 0600)
	if os.IsNotExist(err) && !create {
		return nil
	}
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return ErrUnavailable
	}
	return nil
}

const selectionSchema = `
CREATE TABLE registrations (
 artifact_id TEXT PRIMARY KEY REFERENCES artifacts(id), qualified TEXT NOT NULL UNIQUE,
 alias TEXT NOT NULL, overridable INTEGER NOT NULL CHECK(overridable IN (0,1))
);
CREATE TABLE approvals (
 revision_id TEXT PRIMARY KEY REFERENCES revisions(id),
 overrides TEXT NOT NULL, override_revision TEXT NOT NULL
);
PRAGMA user_version=2;
`

func migrate(db *sql.DB) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return ErrUnavailable
	}
	if version == 0 {
		if _, err := tx.Exec(schema); err != nil {
			return ErrUnavailable
		}
		version = 1
	}
	if version == 1 {
		if _, err := tx.Exec(selectionSchema); err != nil {
			return ErrUnavailable
		}
	} else if version != 2 {
		return ErrUnavailable
	}
	if err := tx.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Session(token, checkout string) (*Session, error) {
	if _, err := s.authority.Authenticate(token, checkout); err != nil {
		return nil, err
	}
	return &Session{store: s, token: token, checkout: checkout}, nil
}

func (s *Session) authenticate() (authority.Context, error) {
	return s.store.authority.Authenticate(s.token, s.checkout)
}
