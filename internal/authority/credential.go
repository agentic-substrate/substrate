package authority

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func WriteCredential(path, token string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return errors.New("credential destination parent must exist")
	}
	if _, err := gitOutput(parent, "rev-parse", "--show-toplevel"); err == nil {
		return errors.New("credential files must be outside Git checkouts")
	}
	file, err := openPrivate(filepath.Join(parent, filepath.Base(path)), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL)
	if err != nil {
		return errors.New("cannot create private credential file; choose a new destination outside Git")
	}
	saved := false
	defer func() {
		file.Close()
		if !saved {
			os.Remove(file.Name())
		}
	}()
	if _, err := io.WriteString(file, token+"\n"); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	saved = true
	return nil
}
func ReadCredential(path string) (string, error) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", ErrDenied
	}
	if _, err := gitOutput(parent, "rev-parse", "--show-toplevel"); err == nil {
		return "", ErrDenied
	}
	file, err := openPrivate(path, syscall.O_RDONLY)
	if err != nil {
		return "", errors.New("credential unavailable; use an owner-only credential file outside Git")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 66))
	if err != nil {
		return "", ErrDenied
	}
	token := strings.TrimSpace(string(data))
	if len(token) != 64 {
		return "", ErrDenied
	}
	return token, nil
}
