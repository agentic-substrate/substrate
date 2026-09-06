package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/store"
)

func tokenCmd(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("want mint or revoke subcommand")
	}
	switch args[0] {
	case "mint":
		return tokenMint(args[1:], stdout)
	case "revoke":
		return tokenRevoke(args[1:])
	default:
		return fmt.Errorf("unknown token command %q", args[0])
	}
}

func tokenMint(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("token mint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	forKind := fs.String("for", "", "principal kind to mint (agent)")
	parent := fs.String("parent", "", "user principal UUID the agent is bound to")
	machine := fs.String("machine", "", "machine this token is for")
	scopes := fs.String("scopes", "", "comma-separated capabilities, e.g. memory:write")
	name := fs.String("name", "agent", "display name")
	dsn := fs.String("dsn", os.Getenv("SUBSTRATE_DSN"), "postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dsn == "" {
		return fmt.Errorf("token mint: -dsn or SUBSTRATE_DSN is required")
	}
	var caps []string
	if *scopes != "" {
		for _, s := range strings.Split(*scopes, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				caps = append(caps, s)
			}
		}
	}
	ctx := context.Background()
	st, err := store.Open(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	res, err := identity.Mint(ctx, st.Pool(), identity.MintInput{
		For:         *forKind,
		Parent:      *parent,
		Machine:     *machine,
		Scopes:      caps,
		DisplayName: *name,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, res.Token)
	return err
}

func tokenRevoke(args []string) error {
	fs := flag.NewFlagSet("token revoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dsn := fs.String("dsn", os.Getenv("SUBSTRATE_DSN"), "postgres DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: substrate token revoke <token>")
	}
	if *dsn == "" {
		return fmt.Errorf("token revoke: -dsn or SUBSTRATE_DSN is required")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, *dsn)
	if err != nil {
		return err
	}
	defer st.Close()
	return identity.Revoke(ctx, st.Pool(), fs.Arg(0))
}
