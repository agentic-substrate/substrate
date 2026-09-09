package adapter

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWrite writes content via a temp file in the same directory, then
// rename. A crash leaves the old file or the new one, never a partial dest.
// The mode is taken from the destination path itself when it already exists,
// falling back to 0644 for a brand-new file. Use AtomicWriteMode when the
// right mode is not the destination's own current mode -- a backup file does
// not exist yet, so its mode must come from the file being backed up instead.
func AtomicWrite(path string, content []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return AtomicWriteMode(path, content, mode)
}

// AtomicWriteMode is AtomicWrite with an explicit mode. The temp file is
// chmod'd to mode before the rename, so the file is never observable at path
// with the wrong mode.
func AtomicWriteMode(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("adapter: mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("adapter: temp file: %w", err)
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return fmt.Errorf("adapter: write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("adapter: sync temp: %w", err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return fmt.Errorf("adapter: chmod temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("adapter: close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("adapter: rename: %w", err)
	}
	cleanup = false
	if err := syncParentDir(dir); err != nil {
		return err
	}
	return nil
}

func fsyncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // dir is the dest parent under Home or a checkout
	if err != nil {
		return fmt.Errorf("adapter: open dir %s: %w", dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("adapter: sync dir %s: %w", dir, err)
	}
	return nil
}

var syncParentDir = fsyncDir
