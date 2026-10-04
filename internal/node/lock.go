package node

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/agentic-substrate/substrate/internal/authority"
)

var ErrRunning = errors.New("node already running: stop it before offline artifact administration")
var ErrUnavailable = errors.New("node unavailable: start substrate node with the same private state directory")

// Lock coordinates the runtime with every offline artifact administrator.
func Lock(auth *authority.Store) (*os.File, error) {
	if _, err := auth.Inventory(); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(auth.Dir, "runtime.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), "runtime.lock")
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		f.Close()
		return nil, ErrUnavailable
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrRunning
	}
	return f, nil
}
func privateSocket(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return nil, ErrUnavailable
	}
	return info, nil
}
