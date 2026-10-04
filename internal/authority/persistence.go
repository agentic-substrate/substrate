package authority

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func checkPrivate(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return privateInfo(info, directory)
}
func privateInfo(info os.FileInfo, directory bool) error {
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) || info.Mode().Perm()&0077 != 0 {
		return errors.New("state files need owner-only permissions and must not be symlinks")
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != uint32(os.Geteuid()) {
		return errors.New("state must belong to the current operating-system user")
	}
	return nil
}
func openPrivate(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err == nil {
		err = privateInfo(info, false)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func (s *Store) locked(create bool, fn func(string) error) error {
	if s.Dir == "" {
		return ErrUnavailable
	}
	absolute, err := filepath.Abs(s.Dir)
	if err != nil {
		return unavailable(err)
	}
	if create {
		parent := absolute
		for {
			if _, err := os.Stat(parent); err == nil {
				break
			}
			next := filepath.Dir(parent)
			if next == parent {
				return ErrUnavailable
			}
			parent = next
		}
		if _, err := gitOutput(parent, "rev-parse", "--show-toplevel"); err == nil {
			return errors.New("private state must be outside Git checkouts")
		}
		if err := os.MkdirAll(absolute, 0700); err != nil {
			return unavailable(err)
		}
	}
	if err := checkPrivate(absolute, true); err != nil {
		return unavailable(err)
	}
	if _, err := gitOutput(absolute, "rev-parse", "--show-toplevel"); err == nil {
		return ErrUnavailable
	}
	lock, err := openPrivate(filepath.Join(absolute, ".lock"), syscall.O_RDWR|syscall.O_CREAT)
	if err != nil {
		return unavailable(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return unavailable(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn(filepath.Join(absolute, "authority.json"))
}
func load(path string, create bool) (state, error) {
	file, err := openPrivate(path, syscall.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) && create {
		return state{}, nil
	}
	if err != nil {
		return state{}, unavailable(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 1024*1024 {
		return state{}, ErrUnavailable
	}
	var st state
	decoder := json.NewDecoder(io.LimitReader(file, 1024*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&st); err != nil {
		return state{}, unavailable(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return state{}, ErrUnavailable
	}
	if st.Version != 1 || st.OwnerID == "" || len(st.Spaces) == 0 || st.Sessions == nil {
		return state{}, ErrUnavailable
	}
	return st, nil
}
func save(path string, st *state) error {
	data, err := json.Marshal(st)
	if err != nil {
		return unavailable(err)
	}
	if len(data) > 1024*1024 {
		return errors.New("local authority capacity exceeded; revoke unused sessions before retrying")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".authority-*")
	if err != nil {
		return unavailable(err)
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	if _, err := io.Copy(file, bytes.NewReader(data)); err != nil {
		return unavailable(err)
	}
	if err := file.Sync(); err != nil {
		return unavailable(err)
	}
	if err := file.Close(); err != nil {
		return unavailable(err)
	}
	if err := os.Rename(name, path); err != nil {
		return unavailable(err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return unavailable(err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return unavailable(err)
	}
	return nil
}
func (s *Store) update(create bool, fn func(*state) error) error {
	return s.locked(create, func(path string) error {
		st, err := load(path, create)
		if err != nil {
			return err
		}
		if err := fn(&st); err != nil {
			return err
		}
		return save(path, &st)
	})
}
func (s *Store) read(fn func(*state) error) error {
	return s.locked(false, func(path string) error {
		st, err := load(path, false)
		if err != nil {
			return err
		}
		return fn(&st)
	})
}
