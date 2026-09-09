package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// checkResult is one doctor line. Fix is empty only when Ok is true.
type checkResult struct {
	Name   string `json:"name"`
	Ok     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

type doctorReport struct {
	Checks    []checkResult `json:"checks"`
	Ok        bool          `json:"ok"`
	CheckedAt string        `json:"checked_at"`
}

func newDoctorCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check config, credential, server reachability and scope",
		Long: `Runs the four checks that explain almost every "it does not work": is there a
config file, is it private, does the stored credential still authenticate, and
does a scope resolve here. Each failing check prints the command that fixes it.`,
		Example: "  substrate doctor\n  substrate doctor --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep := doctorReport{Ok: true, CheckedAt: d.now().UTC().Format(time.RFC3339)}
			cfg, path, cfgErr := loadConfig(d)

			switch {
			case errors.Is(cfgErr, ErrNoConfig):
				rep.Checks = append(rep.Checks, checkResult{Name: "config", Detail: "no config file at " + path,
					Fix: "substrate auth login --server <url> < token.txt"})
			case cfgErr != nil:
				rep.Checks = append(rep.Checks, checkResult{Name: "config", Detail: cfgErr.Error(),
					Fix: "chmod 600 " + path})
			default:
				rep.Checks = append(rep.Checks, checkResult{Name: "config", Ok: true, Detail: path + " is readable and mode 0600"})
			}

			if cfgErr == nil && cfg.Token != "" {
				rep.Checks = append(rep.Checks, checkResult{Name: "credential", Ok: true, Detail: "a token is stored (not shown)"})
			} else {
				rep.Checks = append(rep.Checks, checkResult{Name: "credential", Detail: "no token stored",
					Fix: "substrate auth login --server <url> < token.txt"})
			}

			rep.Checks = append(rep.Checks, doctorServerCheck(cmd, d, cfg, cfgErr))

			if s, err := resolveScope(d, scopeFlagsFrom(cmd)); err != nil {
				rep.Checks = append(rep.Checks, checkResult{Name: "scope", Detail: "no scope resolves in this directory",
					Fix: "substrate context use --org <org>"})
			} else {
				rep.Checks = append(rep.Checks, checkResult{Name: "scope", Ok: true, Detail: "resolved from " + s.Source})
			}

			for _, c := range rep.Checks {
				if !c.Ok {
					rep.Ok = false
				}
			}
			p := printer{w: cmd.OutOrStdout(), json: jsonFlag(cmd)}
			if err := p.emit(rep, func(w io.Writer) error {
				for _, c := range rep.Checks {
					mark := "FAIL"
					if c.Ok {
						mark = "ok  "
					}
					if _, err := fmt.Fprintf(w, "%s %-11s %s\n", mark, c.Name, c.Detail); err != nil {
						return err
					}
					if c.Fix != "" {
						if _, err := fmt.Fprintf(w, "          fix: %s\n", c.Fix); err != nil {
							return err
						}
					}
				}
				return nil
			}); err != nil {
				return err
			}
			if !rep.Ok {
				return &UserError{
					What: "doctor found problems",
					Why:  "at least one check above failed",
					Next: "run the fix printed under each FAIL line",
				}
			}
			return nil
		},
	}
}

func doctorServerCheck(cmd *cobra.Command, d Deps, cfg Config, cfgErr error) checkResult {
	if cfgErr != nil || cfg.Server == "" {
		return checkResult{Name: "server", Detail: "no server configured",
			Fix: "substrate auth login --server <url> < token.txt"}
	}
	err := validateToken(cmd.Context(), d, cfg.Server, cfg.Token)
	if err != nil {
		var ue *UserError
		detail := err.Error()
		fix := "substrate auth login --server " + cfg.Server + " < token.txt"
		if errors.As(err, &ue) {
			detail = ue.Why
			fix = ue.Next
		}
		return checkResult{Name: "server", Detail: detail, Fix: fix}
	}
	return checkResult{Name: "server", Ok: true, Detail: cfg.Server + " accepted the stored credential"}
}
