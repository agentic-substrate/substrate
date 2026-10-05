package artifacts

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/strictjson"
	"github.com/ncruces/go-sqlite3/driver"
)

const MaxBackupDatabase int64 = 1024 * 1024 * 1024

func Snapshot(source, destination string, scope authority.BackupScope) (int, error) {
	db, err := openBackup(source)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	version, err := validateBackup(db, scope, 0)
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("create snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		return 0, err
	}
	err = conn.Raw(func(raw any) error {
		backup, err := raw.(driver.Conn).Raw().BackupInit("main", databaseURI(destination, "rw"))
		if err != nil {
			return err
		}
		for {
			done, stepErr := backup.Step(128)
			if stepErr != nil {
				return errors.Join(stepErr, backup.Close())
			}
			if done {
				break
			}
		}
		return backup.Close()
	})
	err = errors.Join(err, conn.Close())
	if err != nil {
		return 0, fmt.Errorf("SQLite snapshot failed: %w", err)
	}
	if err := ValidateBackup(destination, scope, version); err != nil {
		return 0, err
	}
	f, err = os.OpenFile(destination, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	err = errors.Join(f.Sync(), f.Close())
	return version, err
}

func databaseURI(path, mode string) string {
	query := url.Values{"mode": {mode}, "_pragma": {"trusted_schema(OFF)", "foreign_keys(ON)"}}
	if mode == "ro" {
		query.Set("immutable", "1")
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func openBackup(path string) (*sql.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		fd, err := syscall.Open(path+suffix, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("database unavailable; stop the node and check the private artifacts.db: %w", err)
		}
		f := os.NewFile(uintptr(fd), path+suffix)
		info, statErr := f.Stat()
		closeErr := f.Close()
		if statErr != nil || closeErr != nil {
			return nil, errors.Join(statErr, closeErr)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
			return nil, errors.New("database and journals must be owned private regular files without symlinks")
		}
		if suffix != "" && info.Size() != 0 {
			return nil, errors.New("database has a journal or WAL; recover it with the compatible node, stop the node, and retry")
		}
		if suffix == "" && (info.Size() == 0 || info.Size() > MaxBackupDatabase) {
			return nil, errors.New("database must be nonempty and at most the 1 GiB backup limit")
		}
	}
	db, err := driver.Open(databaseURI(path, "ro"))
	if err != nil {
		return nil, fmt.Errorf("read-only database unavailable: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func ValidateBackup(path string, scope authority.BackupScope, version int) error {
	db, err := openBackup(path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = validateBackup(db, scope, version)
	return err
}

type schemaEntry struct{ Type, Name, Table, SQL string }

func backupLayout(db *sql.DB) ([]schemaEntry, error) {
	rows, err := db.Query("SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE sql IS NOT NULL ORDER BY type,name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schemaEntry{}
	for rows.Next() {
		var e schemaEntry
		if err := rows.Scan(&e.Type, &e.Name, &e.Table, &e.SQL); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func validateBackup(db *sql.DB, scope authority.BackupScope, expected int) (int, error) {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("invalid SQLite backup: %w", err)
	}
	if version < 1 || version > 5 || expected != 0 && version != expected {
		return 0, errors.New("incompatible artifact schema; use the matching Substrate version to recover this backup")
	}
	reference, err := driver.Open(":memory:")
	if err != nil {
		return 0, err
	}
	defer reference.Close()
	for i, migration := range []string{schema, selectionSchema, retrievalSchema, publicationSchema, maintenanceSchema} {
		if i < version {
			if _, err := reference.Exec(migration); err != nil {
				return 0, err
			}
		}
	}
	want, err := backupLayout(reference)
	if err != nil {
		return 0, err
	}
	got, err := backupLayout(db)
	if err != nil {
		return 0, err
	}
	if !reflect.DeepEqual(want, got) {
		return 0, errors.New("unsupported backup schema layout; unexpected tables, views, triggers, or definitions")
	}
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return 0, errors.New("corrupt SQLite backup; choose another complete snapshot")
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return 0, err
	}
	bad := rows.Next()
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return 0, err
	}
	if bad {
		return 0, errors.New("backup has broken foreign-key relationships")
	}
	if err := validateBackupState(db, scope, version); err != nil {
		return 0, fmt.Errorf("invalid application state in backup: %w", err)
	}
	return version, nil
}

func backupHex(value string, size int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == size && hex.EncodeToString(b) == value
}

func validateBackupState(db *sql.DB, scope authority.BackupScope, version int) error {
	repos := map[string]string{}
	for _, b := range scope.Bindings {
		repos[b.RepositoryID] = b.SpaceID
	}
	rows, err := db.Query("SELECT id,owner_id,space_id,repo_id,kind,lifecycle,head FROM artifacts")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, owner, space, repo, kind, life, head string
		if err := rows.Scan(&id, &owner, &space, &repo, &kind, &life, &head); err != nil {
			rows.Close()
			return err
		}
		if !backupHex(id, 32) || owner != scope.OwnerID || repos[repo] != space || space == "" || !validText(kind, life, head) || head != "" && !backupHex(head, 32) {
			rows.Close()
			return errors.New("artifact owner, scope, or ID does not match the authority")
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	checks := []string{
		"SELECT count(*) FROM artifacts a LEFT JOIN revisions r ON r.id=a.head AND r.artifact_id=a.id WHERE a.head<>'' AND r.id IS NULL",
		"SELECT count(*) FROM artifacts a JOIN revisions r ON r.id=a.head WHERE (a.kind='memory' AND r.state<>'pending-local') OR (a.kind<>'memory' AND r.state<>'approved')",
		"SELECT count(*) FROM pending p JOIN artifacts a ON a.id=p.artifact_id WHERE p.owner_id<>a.owner_id OR p.space_id<>a.space_id OR p.repo_id<>a.repo_id OR (p.revision_id<>'' AND NOT EXISTS(SELECT 1 FROM revisions r WHERE r.id=p.revision_id AND r.artifact_id=a.id))",
		"SELECT count(*) FROM pending p WHERE NOT EXISTS(SELECT 1 FROM contributions c WHERE c.owner_id=p.owner_id AND c.space_id=p.space_id AND c.repo_id=p.repo_id AND c.operation_id=p.operation_id)",
		"SELECT count(*) FROM pending p JOIN contributions c ON c.owner_id=p.owner_id AND c.space_id=p.space_id AND c.repo_id=p.repo_id AND c.operation_id=p.operation_id WHERE p.artifact_id<>json_extract(c.receipt,'$.artifact_id') OR p.revision_id<>json_extract(c.receipt,'$.revision_id') OR p.action NOT IN ('contribute','retire','restore','approve','resolve-conflict','publish')",
	}
	if version >= 2 {
		checks = append(checks, "SELECT count(*) FROM approvals ap JOIN revisions r ON r.id=ap.revision_id JOIN artifacts a ON a.id=r.artifact_id WHERE r.state<>'approved' OR a.kind='memory' OR (ap.overrides='')<>(ap.override_revision='') OR (ap.overrides<>'' AND NOT EXISTS(SELECT 1 FROM artifacts target JOIN revisions tr ON tr.artifact_id=target.id AND tr.id=ap.override_revision WHERE target.id=ap.overrides AND target.owner_id=a.owner_id AND target.space_id=a.space_id AND target.repo_id=a.repo_id AND target.kind=a.kind))")
	}
	if version >= 3 {
		for _, table := range []string{"indexed", "tokens"} {
			clause := ""
			if table == "indexed" {
				clause = "i.revision_id<>'' AND "
			}
			checks = append(checks, "SELECT count(*) FROM "+table+" i WHERE "+clause+"NOT EXISTS(SELECT 1 FROM revisions r WHERE r.id=i.revision_id AND r.artifact_id=i.artifact_id)")
		}
		checks = append(checks, "SELECT count(*) FROM indexed WHERE limited NOT IN (0,1)", "SELECT count(*) FROM tokens WHERE position<0 OR token=''")
	}
	if version >= 5 {
		checks = append(checks, "SELECT abs(count(*)-1) FROM maintenance", "SELECT count(*) FROM index_queue WHERE failure NOT IN ('','indexing failed')")
	}
	for _, query := range checks {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("broken artifact, revision, scope, or durable-work relationship")
		}
	}
	rows, err = db.Query("SELECT r.id,r.base,r.content,r.provenance,r.author_id,r.state,r.verification,r.source,a.kind FROM revisions r JOIN artifacts a ON a.id=r.artifact_id")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, base, content, provenance, author, state, verification, source, kind string
		if err := rows.Scan(&id, &base, &content, &provenance, &author, &state, &verification, &source, &kind); err != nil {
			rows.Close()
			return err
		}
		if !backupHex(id, 32) || len(content) > 1024*1024 || len(provenance) > 4096 || author != scope.OwnerID || verification != "unverified" || !validText(base, content, provenance, state) || !strings.Contains("|pending-local|candidate|conflict|conflict-retired|resolved|retired-candidate|approved|", "|"+state+"|") {
			rows.Close()
			return errors.New("invalid revision metadata")
		}
		if kind == "memory" {
			if source != "" || strings.TrimSpace(content) == "" {
				rows.Close()
				return errors.New("memory cannot contain executable source")
			}
		} else {
			var src Source
			if err := strictjson.Decode([]byte(source), &src); err != nil {
				rows.Close()
				return err
			}
			if err := validateBackupSource(src, len(content)); err != nil {
				rows.Close()
				return err
			}
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if err := validateBackupMetadata(db, scope, repos, version); err != nil {
		return err
	}
	if version >= 3 {
		return validateBackupIndex(db)
	}
	return nil
}

func validateBackupSource(src Source, total int) error {
	if !(backupHex(src.Commit, 20) || backupHex(src.Commit, 32)) || !(backupHex(src.Blob, 20) || backupHex(src.Blob, 32)) || !backupSourcePath(src.Path) || len(src.Files) > 32 {
		return errors.New("invalid immutable Git source")
	}
	seen := map[string]bool{src.Path: true}
	for _, file := range src.Files {
		total += len(file.Content)
		if seen[file.Path] || !backupSourcePath(file.Path) || !(backupHex(file.Blob, 20) || backupHex(file.Blob, 32)) || !validText(file.Content) {
			return errors.New("invalid Git dependency bundle")
		}
		seen[file.Path] = true
	}
	if total > 1024*1024 {
		return errors.New("Git bundle exceeds 1 MiB")
	}
	return nil
}

func backupSourcePath(path string) bool {
	return path != "" && len(path) <= 4096 && !filepath.IsAbs(path) && filepath.Clean(path) == path && path != "." && path != ".." && !strings.HasPrefix(path, "../") && !strings.ContainsAny(path, "\x00\r\n\\:") && validText(path)
}
