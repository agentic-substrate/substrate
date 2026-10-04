package authority

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func outsideCheckout(path string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("Git is required to verify private placement; install Git and retry")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return errors.New("cannot verify private placement; check destination directory permissions")
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return errors.New("cannot resolve private placement")
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return errors.New("private placement requires an accessible directory")
	}
	for {
		_, err := os.Lstat(filepath.Join(canonical, ".git"))
		if err == nil {
			return errors.New("private state and credentials must be outside Git checkouts")
		}
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("cannot verify private placement; check ancestor directory permissions")
		}
		parent := filepath.Dir(canonical)
		if parent == canonical {
			return nil
		}
		canonical = parent
	}
}
