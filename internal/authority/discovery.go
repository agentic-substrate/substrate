package authority

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	RepositoryID  string `json:"repo_id"`
	SpaceID       string `json:"space_id"`
}

func Discover(path string) (Manifest, error) {
	binding, err := checkout(path)
	if err != nil {
		return Manifest{}, errors.New("discovery requires a Git checkout")
	}
	name := filepath.Join(binding.Checkout, ".substrate.json")
	fd, err := syscall.Open(name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Manifest{}, errors.New("discovery manifest unavailable; register the checkout through trusted setup")
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return Manifest{}, errors.New("manifest must be a regular file of at most 8192 bytes")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 8193))
	decoder.DisallowUnknownFields()
	var hint Manifest
	if err := decoder.Decode(&hint); err != nil {
		return Manifest{}, errors.New("invalid discovery manifest; expected only schema_version, repo_id, and space_id")
	}
	if err := decoder.Decode(new(any)); err != io.EOF || hint.SchemaVersion != 1 || hint.RepositoryID == "" || hint.SpaceID == "" {
		return Manifest{}, errors.New("invalid discovery manifest; use schema version 1 and nonempty IDs")
	}
	return hint, nil
}
