package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/agentic-substrate/substrate/internal/adapter"
)

// ContextFileName is the saved-context file inside the config directory.
const ContextFileName = "context.json"

// Scope is the org/team/project a command applies to, plus the repo key when
// the checkout supplied one. Repo is a repo key and travels as the `repo`
// field; it is never rendered into a scope string, because binding a remote to
// a chain is the server's job (R18). `user` is deliberately absent: it is a
// preference overlay, not a chain level (Gotcha 1).
type Scope struct {
	Org     string `json:"org,omitempty"`
	Team    string `json:"team,omitempty"`
	Project string `json:"project,omitempty"`
	Repo    string `json:"repo,omitempty"`
	// Source names where the winning values came from, so `context show` can
	// tell an operator why they are pointed where they are.
	Source string `json:"source"`
}

// Scope sources, in the order resolveScope tries them.
const (
	SourceFlags   = "flags"
	SourceFile    = "context file"
	SourceGit     = "git remote"
	SourceUnknown = "unresolved"
)

type scopeFlags struct {
	org     string
	team    string
	project string
}

func (f scopeFlags) any() bool { return f.org != "" || f.team != "" || f.project != "" }

func contextPath(d Deps) (string, error) {
	dir, err := d.configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ContextFileName), nil
}

func loadContext(d Deps) (Scope, string, error) {
	path, err := contextPath(d)
	if err != nil {
		return Scope{}, "", err
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is derived from the user's own config dir
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Scope{}, path, ErrNoConfig
		}
		return Scope{}, path, fmt.Errorf("read %s: %w", path, err)
	}
	var s Scope
	if err := json.Unmarshal(raw, &s); err != nil {
		return Scope{}, path, &UserError{
			What: fmt.Sprintf("cannot parse %s", path),
			Why:  err.Error(),
			Next: fmt.Sprintf("delete %s and run: substrate context use --org <org>", path),
		}
	}
	return s, path, nil
}

// resolveScope picks the scope a command runs against: explicit flags, then the
// saved context file, then the checkout's git remote, then an error. There is
// no global default -- guessing `global:` would silently file work at a scope
// the caller may not write (#63).
func resolveScope(d Deps, f scopeFlags) (Scope, error) {
	if f.any() {
		return Scope{Org: f.org, Team: f.team, Project: f.project, Source: SourceFlags}, nil
	}
	saved, _, err := loadContext(d)
	if err != nil && !errors.Is(err, ErrNoConfig) {
		return Scope{}, err
	}
	if err == nil && (saved.Org != "" || saved.Team != "" || saved.Project != "") {
		saved.Source = SourceFile
		return saved, nil
	}
	if cwd, err := d.getwd(); err == nil {
		if key, ok := adapter.RepoKey(cwd); ok {
			return Scope{Repo: key, Source: SourceGit}, nil
		}
	}
	return Scope{Source: SourceUnknown}, &UserError{
		What: "no scope resolved",
		Why:  "no --org/--team/--project flag, no saved context, and this directory has no parseable git origin",
		Next: "run: substrate context use --org <org> [--team <team>] [--project <project>]",
	}
}

func newContextCmd(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Show or set the scope commands run against",
		Long: `Scope resolves in a fixed order, first hit wins:

  1. explicit --org/--team/--project flags
  2. the saved context file written by "substrate context use"
  3. this checkout's git origin, sent as a repo key
  4. otherwise an error -- there is no global default`,
	}
	cmd.AddCommand(newContextShowCmd(d), newContextUseCmd(d))
	return cmd
}

func newContextShowCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "show",
		Short:   "Print the resolved scope and where it came from",
		Example: "  substrate context show\n  substrate context show --org acme --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := scopeFlagsFrom(cmd)
			s, err := resolveScope(d, f)
			if err != nil {
				return err
			}
			p := printer{w: cmd.OutOrStdout(), json: jsonFlag(cmd)}
			return p.emit(s, func(w io.Writer) error {
				// The server-side resolved chain is deliberately not fetched:
				// it would disclose another team's naming (rest.go repoScope).
				if s.Repo != "" {
					if _, err := fmt.Fprintf(w, "repo:    %s\n", s.Repo); err != nil {
						return err
					}
				}
				for _, kv := range [][2]string{{"org", s.Org}, {"team", s.Team}, {"project", s.Project}} {
					if kv[1] == "" {
						continue
					}
					if _, err := fmt.Fprintf(w, "%-8s %s\n", kv[0]+":", kv[1]); err != nil {
						return err
					}
				}
				_, err := fmt.Fprintf(w, "source:  %s\n", s.Source)
				return err
			})
		},
	}
}

func newContextUseCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "use",
		Short:   "Save an org/team/project as the default scope",
		Example: "  substrate context use --org acme --team platform",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := scopeFlagsFrom(cmd)
			if !f.any() {
				return &UserError{
					What: "context use needs something to save",
					Why:  "none of --org, --team or --project was given",
					Next: "run: substrate context use --org <org>",
				}
			}
			path, err := contextPath(d)
			if err != nil {
				return err
			}
			s := Scope{Org: f.org, Team: f.team, Project: f.project, Source: SourceFile}
			if err := writeJSON0600(path, s); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "saved %s\n", path)
			return err
		},
	}
}
