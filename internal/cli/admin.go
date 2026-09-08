package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/store"
)

func newAdminCmd(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Database-gated bootstrap commands",
		Long: `admin commands require the postgres DSN. That is deliberate rather than a
gap: whoever holds the DSN already has total control of the data, so gating on
it grants nothing the caller did not already have.`,
	}
	cmd.AddCommand(newAdminCreateUserCmd(d))
	return cmd
}

// createUserOutput is the --json shape. Token is printed exactly once, here,
// and is never written to a log line.
type createUserOutput struct {
	PrincipalID string `json:"principal_id"`
	DisplayName string `json:"display_name"`
	Org         string `json:"org"`
	Team        string `json:"team"`
	Token       string `json:"token"`
}

func newAdminCreateUserCmd(d Deps) *cobra.Command {
	var flags struct {
		dsn     string
		name    string
		org     string
		team    string
		machine string
		admin   bool
	}
	cmd := &cobra.Command{
		Use:   "create-user",
		Short: "Create the first user principal and print its token once",
		Long: `Creates an org, a team, a user principal, its membership and an API token in
one transaction, then prints the token once. Nothing else in the codebase can
produce a user principal: identity.Mint refuses --for values other than agent.

The token does not expire. Store it with "substrate auth login", which writes
it 0600, and revoke it with "substrate token revoke" when the machine retires.`,
		Example: "  substrate admin create-user --dsn \"$SUBSTRATE_DSN\" --name ada --org acme --team platform --admin",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.dsn == "" {
				return &UserError{
					What: "admin create-user needs a database DSN",
					Why:  "neither --dsn nor SUBSTRATE_DSN is set",
					Next: "run: substrate admin create-user --dsn postgres://... --name <you> --org <org> --team <team>",
				}
			}
			machine := flags.machine
			if machine == "" {
				machine = d.env("HOSTNAME")
			}
			if machine == "" {
				machine = "unknown"
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			st, err := store.Open(ctx, flags.dsn)
			if err != nil {
				return fmt.Errorf("admin create-user: open store: %w", err)
			}
			defer st.Close()
			var res identity.CreateUserResult
			// One transaction: CreateUser writes five tables, and a partial
			// failure must leave zero rows rather than an org with no members.
			err = st.Tx(ctx, func(tx pgx.Tx) error {
				var terr error
				res, terr = identity.CreateUser(ctx, tx, identity.CreateUserInput{
					DisplayName: flags.name,
					Org:         flags.org,
					Team:        flags.team,
					Machine:     machine,
					Admin:       flags.admin,
				})
				return terr
			})
			if err != nil {
				return err
			}
			out := createUserOutput{
				PrincipalID: res.PrincipalID.String(),
				DisplayName: flags.name,
				Org:         flags.org,
				Team:        flags.team,
				Token:       res.Token,
			}
			p := printer{w: cmd.OutOrStdout(), json: jsonFlag(cmd)}
			return p.emit(out, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "principal: %s (%s)\norg/team:  %s/%s\ntoken:     %s\n\nThis token is shown once and does not expire. Store it now:\n  substrate auth login --server <url>\n",
					out.PrincipalID, out.DisplayName, out.Org, out.Team, out.Token)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&flags.dsn, "dsn", d.env("SUBSTRATE_DSN"), "postgres DSN (required)")
	f.StringVar(&flags.name, "name", "", "display name of the user principal (required)")
	f.StringVar(&flags.org, "org", "", "org to create or reuse (required)")
	f.StringVar(&flags.team, "team", "", "team to create or reuse inside the org (required)")
	f.StringVar(&flags.machine, "machine", "", "machine the token is issued for (defaults to $HOSTNAME)")
	f.BoolVar(&flags.admin, "admin", false, "mint at human_admin trust rather than human")
	return cmd
}
