package cli

import (
	"github.com/spf13/cobra"

	"github.com/agentic-substrate/substrate/internal/version"
)

// NewRoot builds a complete, independent command tree bound to d. Nothing in
// this package is a package-level variable, so two roots built with different
// Deps can run concurrently -- which `make check`'s -race would otherwise turn
// into an intermittent failure rather than a clear one.
func NewRoot(d Deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "substrate",
		Short: "Operator CLI for the Substrate control plane",
		Long: `substrate administers a Substrate control plane from your machine.

Clone to an authenticated, scope-aware command in three steps:

  1. substrate admin create-user --dsn "$SUBSTRATE_DSN" --name you --org acme --team platform --admin
  2. substrate auth login --server https://substrate.example    # token on stdin
  3. substrate context use --org acme --team platform

Then "substrate doctor" tells you whether any of it worked.`,
		Example: `  substrate doctor
  substrate context show --json
  substrate auth status`,
		Version: version.Version + " (" + version.Revision() + ")",
		// main owns error printing: it renders a UserError's what/why/next
		// verbatim, and cobra printing it a second time would double it.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(d.stdout())
	root.SetErr(d.stderr())

	pf := root.PersistentFlags()
	pf.Bool("json", false, "emit machine-readable JSON instead of text")
	pf.String("org", "", "org this command applies to (overrides the saved context)")
	pf.String("team", "", "team this command applies to (overrides the saved context)")
	pf.String("project", "", "project this command applies to (overrides the saved context)")

	root.AddCommand(
		newAuthCmd(d),
		newAdminCmd(d),
		newContextCmd(d),
		newDoctorCmd(d),
		newImportCmd(d),
		newReviewCmd(d),
		newAdapterCmd(d),
		newTokenCmd(d),
	)
	return root
}

// jsonFlag reads the inherited --json flag. Reading it off the command rather
// than a package variable is what keeps two concurrent roots independent.
func jsonFlag(cmd *cobra.Command) bool {
	v, err := cmd.Flags().GetBool("json")
	return err == nil && v
}

func scopeFlagsFrom(cmd *cobra.Command) scopeFlags {
	get := func(name string) string {
		v, err := cmd.Flags().GetString(name)
		if err != nil {
			return ""
		}
		return v
	}
	return scopeFlags{org: get("org"), team: get("team"), project: get("project")}
}
