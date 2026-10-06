package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
	"github.com/agentic-substrate/substrate/internal/strictjson"
	"golang.org/x/sys/unix"
)

const manifestLimit int64 = 16 * 1024
const authorityLimit int64 = 1024 * 1024

type fileEntry struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	Format           int         `json:"format"`
	AuthorityVersion int         `json:"authority_version"`
	ArtifactSchema   int         `json:"artifact_schema"`
	Files            []fileEntry `json:"files"`
}

func Backup(stateDir, destination string) error {
	if err := authority.CheckPrivatePlacement(stateDir); err != nil {
		return fmt.Errorf("backup source: %w", err)
	}
	auth := &authority.Store{Dir: stateDir}
	lock, err := node.Lock(auth)
	if err != nil {
		return err
	}
	defer lock.Close()
	stage, destination, err := newStage(destination)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	err = auth.WithBackupSnapshot(func(data []byte, scope authority.BackupScope) error {
		if err := writeSynced(filepath.Join(stage, "authority.json"), data); err != nil {
			return err
		}
		version, err := artifacts.Snapshot(filepath.Join(stateDir, "artifacts.db"), filepath.Join(stage, "artifacts.db"), scope)
		if err != nil {
			return err
		}
		m := manifest{Format: 1, AuthorityVersion: 1, ArtifactSchema: version}
		for _, name := range []string{"authority.json", "artifacts.db"} {
			entry, err := hashFile(filepath.Join(stage, name))
			if err != nil {
				return err
			}
			m.Files = append(m.Files, entry)
		}
		encoded, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return writeSynced(filepath.Join(stage, "manifest.json"), encoded)
	})
	if err != nil {
		return fmt.Errorf("backup failed; current state is unchanged: %w", err)
	}
	return promoteStage(stage, destination)
}

func Restore(backupDir, destination string) error {
	dir, err := openDirectory(backupDir)
	if err != nil {
		return fmt.Errorf("backup directory: %w", err)
	}
	defer dir.Close()
	names, err := dir.Readdirnames(4)
	if err != nil && err != io.EOF {
		return err
	}
	want := map[string]bool{"manifest.json": true, "authority.json": true, "artifacts.db": true}
	if len(names) != 3 {
		return errors.New("incomplete backup or unexpected files; use a directory containing exactly manifest.json, authority.json, and artifacts.db")
	}
	for _, name := range names {
		if !want[name] {
			return errors.New("backup contains unexpected files; choose a complete unmodified backup")
		}
	}
	f, err := openFileAt(dir, "manifest.json", manifestLimit)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, manifestLimit+1))
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	var m manifest
	if len(data) > int(manifestLimit) || strictjson.Decode(data, &m) != nil {
		return errors.New("invalid backup manifest; use a complete compatible backup")
	}
	if err := validateManifest(m); err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(backupDir)
	if err != nil {
		return err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(canonical, abs)
	if err != nil || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("restore destination must be outside the backup directory to preserve its inventory")
	}
	stage, destination, err := newStage(destination)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, entry := range m.Files {
		f, err := openFileAt(dir, entry.Name, fileLimit(entry.Name))
		if err != nil {
			return err
		}
		err = copyVerified(f, filepath.Join(stage, entry.Name), entry)
		err = errors.Join(err, f.Close())
		if err != nil {
			return fmt.Errorf("invalid backup %s: %w", entry.Name, err)
		}
	}
	authorityPath := filepath.Join(stage, "authority.json")
	data, err = os.ReadFile(authorityPath)
	if err != nil {
		return err
	}
	scope, err := authority.ValidateBackupSnapshot(data)
	if err != nil {
		return fmt.Errorf("invalid backup authority: %w", err)
	}
	if err := artifacts.ValidateBackup(filepath.Join(stage, "artifacts.db"), scope, m.ArtifactSchema); err != nil {
		return err
	}
	clean, err := authority.ResetBackupSessions(data)
	if err != nil {
		return err
	}
	if err := os.Remove(authorityPath); err != nil {
		return err
	}
	if err := writeSynced(authorityPath, clean); err != nil {
		return err
	}
	if err := artifacts.PrepareRestored(&authority.Store{Dir: stage}); err != nil {
		return fmt.Errorf("restore migration failed; choose a compatible backup: %w", err)
	}
	if err := artifacts.ValidateBackup(filepath.Join(stage, "artifacts.db"), scope, 0); err != nil {
		return fmt.Errorf("restored migration produced invalid state: %w", err)
	}
	if err := syncFiles(stage); err != nil {
		return err
	}
	return promoteStage(stage, destination)
}

func validateManifest(m manifest) error {
	if m.Format != 1 || m.AuthorityVersion != 1 || m.ArtifactSchema < 1 || m.ArtifactSchema > 5 {
		return errors.New("incompatible backup format or schema; use the matching Substrate version")
	}
	if len(m.Files) != 2 {
		return errors.New("incomplete backup manifest; authority and artifact database are required")
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		limit := fileLimit(f.Name)
		hash, err := hex.DecodeString(f.SHA256)
		if limit == 0 || seen[f.Name] || f.Size <= 0 || f.Size > limit || err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != f.SHA256 {
			return errors.New("invalid backup inventory, size, or SHA256 checksum")
		}
		seen[f.Name] = true
	}
	return nil
}

func fileLimit(name string) int64 {
	switch name {
	case "authority.json":
		return authorityLimit
	case "artifacts.db":
		return artifacts.MaxBackupDatabase
	}
	return 0
}

func privateInfo(info os.FileInfo, directory bool) error {
	if info.IsDir() != directory || !directory && !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return errors.New("backup needs owner-only directories and owned regular files without symlinks")
	}
	return nil
}

func openDirectory(path string) (*os.File, error) {
	if err := authority.CheckPrivatePlacement(path); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err == nil {
		err = privateInfo(info, true)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func openFileAt(dir *os.File, name string, limit int64) (*os.File, error) {
	fd, err := syscall.Openat(int(dir.Fd()), name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("backup file unavailable: %w", err)
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err == nil {
		err = privateInfo(info, false)
	}
	if err == nil && info.Size() > limit {
		err = fmt.Errorf("%s exceeds its backup format size limit", name)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func newStage(destination string) (string, string, error) {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(abs); !errors.Is(err, os.ErrNotExist) {
		return "", "", errors.New("destination must be unused; choose a new directory outside Git")
	}
	parent := filepath.Dir(abs)
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil || canonical != parent {
		return "", "", errors.New("destination parent must exist without symlinks")
	}
	if err := authority.CheckOutsideCheckout(parent); err != nil {
		return "", "", err
	}
	stage, err := os.MkdirTemp(parent, ".substrate-recovery-*")
	return stage, abs, err
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func hashFile(path string) (fileEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileEntry{}, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, io.LimitReader(f, fileLimit(filepath.Base(path))+1))
	if err != nil {
		return fileEntry{}, err
	}
	if size > fileLimit(filepath.Base(path)) {
		return fileEntry{}, errors.New("snapshot exceeds its backup format size limit")
	}
	return fileEntry{Name: filepath.Base(path), Size: size, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func copyVerified(source *os.File, path string, entry fileEntry) error {
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if info.Size() != entry.Size {
		return errors.New("file size does not match the manifest")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(source, entry.Size+1))
	if err == nil && (size != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256) {
		err = errors.New("checksum or size mismatch; choose another complete backup")
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func syncFiles(stage string) error {
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		f, err := os.OpenFile(filepath.Join(stage, entry.Name()), os.O_RDWR, 0)
		if err != nil {
			return err
		}
		err = errors.Join(f.Sync(), f.Close())
		if err != nil {
			return err
		}
	}
	return syncDirectory(stage)
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func promoteStage(stage, destination string) error {
	if err := syncDirectory(stage); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return errors.New("destination must be unused; another directory already occupies it")
		}
		return fmt.Errorf("atomic no-replace promotion failed: %w", err)
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("%s was published, but syncing its parent directory failed: %w", destination, err)
	}
	return nil
}
