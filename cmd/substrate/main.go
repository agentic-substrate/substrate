// Command substrate is the operator CLI: auth, admin, context and doctor,
// plus import, review, adapter and token. Every verb lives in internal/cli;
// main only builds the production Deps and prints the one error cobra returns.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/agentic-substrate/substrate/internal/cli"
)

func main() {
	root := cli.NewRoot(cli.Deps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Env:    os.Getenv,
		Getwd:  os.Getwd,
		Now:    time.Now,
	})
	// main owns error printing (SilenceErrors), so a UserError's
	// what/why/next reaches stderr exactly once.
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
