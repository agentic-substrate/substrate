// Package cli builds the substrate operator command tree. Every command is a
// constructor taking Deps, never a package-level variable: `make check` runs
// tests with -race, and shared cobra state between two roots surfaces as a
// flake rather than a clean failure.
package cli

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/agentic-substrate/substrate/internal/cutover"
)

// Deps is everything a command reaches outside its own arguments. Production
// passes the real world; tests pass buffers, maps and temp dirs, which is what
// lets two roots run concurrently without crosstalk.
type Deps struct {
	Stdout, Stderr io.Writer
	// Stdin backs the token prompt. A nil Stdin means non-interactive: a
	// command that needs input must fail with a UserError, never block.
	Stdin     io.Reader
	Env       func(string) string
	Getwd     func() (string, error)
	HTTP      *http.Client
	Installer cutover.UnitInstaller
	// ConfigDir overrides the user config directory. Empty means look it up;
	// $HOME is never guessed.
	ConfigDir string
	Now       func() time.Time
}

func (d Deps) stdout() io.Writer {
	if d.Stdout == nil {
		return os.Stdout
	}
	return d.Stdout
}

func (d Deps) stderr() io.Writer {
	if d.Stderr == nil {
		return os.Stderr
	}
	return d.Stderr
}

func (d Deps) env(k string) string {
	if d.Env == nil {
		return os.Getenv(k)
	}
	return d.Env(k)
}

func (d Deps) getwd() (string, error) {
	if d.Getwd == nil {
		return os.Getwd()
	}
	return d.Getwd()
}

func (d Deps) httpClient() *http.Client {
	if d.HTTP == nil {
		return NewHTTPClient()
	}
	return d.HTTP
}

func (d Deps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// configDir is the directory holding config.json and context.json. It honours
// an explicit Deps.ConfigDir, then XDG_CONFIG_HOME, then os.UserConfigDir --
// whose error is propagated rather than papered over with $HOME, matching the
// refuses-to-guess stance the flag commands already take.
func (d Deps) configDir() (string, error) {
	if d.ConfigDir != "" {
		return d.ConfigDir, nil
	}
	if x := d.env("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "substrate"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", &UserError{
			What: "cannot locate your user config directory",
			Why:  err.Error(),
			Next: "set XDG_CONFIG_HOME to an absolute path and retry",
		}
	}
	return filepath.Join(base, "substrate"), nil
}
