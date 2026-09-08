package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is the credential file at <config dir>/config.json. Token is the
// user token stored by `auth login`; it is never printed by any command.
type Config struct {
	Server string `json:"server"`
	Token  string `json:"token,omitempty"`
}

// ConfigFileName is the credential file's name inside the config directory.
const ConfigFileName = "config.json"

// ErrNoConfig reports that no config file exists yet. It is not a failure for
// commands that work fine without one, such as `context show`.
var ErrNoConfig = errors.New("cli: no config file")

func configPath(d Deps) (string, error) {
	dir, err := d.configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFileName), nil
}

// loadConfig reads config.json. It refuses a file any other user can read,
// the way ssh refuses a group-readable private key: this file holds a
// never-expiring token, so a permissive mode is an error, not a warning.
func loadConfig(d Deps) (Config, string, error) {
	path, err := configPath(d)
	if err != nil {
		return Config{}, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, path, ErrNoConfig
		}
		return Config{}, path, fmt.Errorf("read %s: %w", path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Config{}, path, &UserError{
			What: fmt.Sprintf("refusing to read %s", path),
			Why:  fmt.Sprintf("it holds an API token but its mode is %04o, readable by other users", info.Mode().Perm()),
			Next: fmt.Sprintf("chmod 600 %s", path),
		}
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is derived from the user's own config dir
	if err != nil {
		return Config{}, path, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, path, &UserError{
			What: fmt.Sprintf("cannot parse %s", path),
			Why:  err.Error(),
			Next: fmt.Sprintf("delete %s and run: substrate auth login", path),
		}
	}
	return cfg, path, nil
}

// saveConfig writes cfg 0600 via a temp file plus rename, so a crash mid-write
// leaves the previous config intact rather than a truncated one.
func saveConfig(d Deps, cfg Config) (string, error) {
	path, err := configPath(d)
	if err != nil {
		return "", err
	}
	return path, writeJSON0600(path, cfg)
}

func writeJSON0600(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename onto %s: %w", path, err)
	}
	return nil
}
