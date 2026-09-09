package cli

import (
	"fmt"
	"strings"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/store"
	"github.com/spf13/cobra"
)

func newTokenCmd(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Mint and revoke principal tokens (requires DSN)",
		Long: "Mint and revoke principal tokens.\n\n" +
			"These talk to Postgres directly, not to the control plane's API, so they " +
			"need --dsn or SUBSTRATE_DSN and are meant to be run by an operator on the " +
			"database host.",
	}
	cmd.AddCommand(newTokenMintCmd(d), newTokenRevokeCmd(d))
	return cmd
}

func newTokenMintCmd(d Deps) *cobra.Command {
	var f struct {
		forKind string
		parent  string
		machine string
		scopes  string
		name    string
		dsn     string
	}
	cmd := &cobra.Command{
		Use:   "mint",
		Short: "Mint a token; it is printed once and stored only as a hash",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			const op = "token mint"
			dsn, err := requireDSN(d, op, f.dsn)
			if err != nil {
				return err
			}
			var caps []string
			for _, s := range strings.Split(f.scopes, ",") {
				if s = strings.TrimSpace(s); s != "" {
					caps = append(caps, s)
				}
			}
			ctx := cmd.Context()
			st, err := store.Open(ctx, dsn)
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			defer st.Close()
			res, err := identity.Mint(ctx, st.Pool(), identity.MintInput{
				For:         f.forKind,
				Parent:      f.parent,
				Machine:     f.machine,
				Scopes:      caps,
				DisplayName: f.name,
			})
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			_, err = fmt.Fprintln(d.stdout(), res.Token)
			return err
		},
	}
	cmd.Flags().StringVar(&f.forKind, "for", "", "principal kind to mint (agent)")
	cmd.Flags().StringVar(&f.parent, "parent", "", "user principal UUID the agent is bound to")
	cmd.Flags().StringVar(&f.machine, "machine", "", "machine this token is for")
	cmd.Flags().StringVar(&f.scopes, "scopes", "", "comma-separated capabilities, e.g. memory:write")
	cmd.Flags().StringVar(&f.name, "name", "agent", "display name")
	cmd.Flags().StringVar(&f.dsn, "dsn", "", "postgres DSN (default $SUBSTRATE_DSN)")
	return cmd
}

func newTokenRevokeCmd(d Deps) *cobra.Command {
	var dsnFlag string
	cmd := &cobra.Command{
		Use:   "revoke <token>",
		Short: "Revoke a token by its value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			const op = "token revoke"
			dsn, err := requireDSN(d, op, dsnFlag)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			st, err := store.Open(ctx, dsn)
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			defer st.Close()
			if err := identity.Revoke(ctx, st.Pool(), args[0]); err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsnFlag, "dsn", "", "postgres DSN (default $SUBSTRATE_DSN)")
	return cmd
}

func requireDSN(d Deps, op, dsn string) (string, error) {
	if dsn == "" {
		dsn = d.env("SUBSTRATE_DSN")
	}
	if dsn == "" {
		return "", &UserError{
			What: op + ": no database DSN",
			Why:  "token commands talk to Postgres directly, not to the control plane API",
			Next: "export SUBSTRATE_DSN=postgres://…, or pass --dsn",
		}
	}
	return dsn, nil
}
