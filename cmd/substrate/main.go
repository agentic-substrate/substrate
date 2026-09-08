// Command substrate is the operator CLI: auth, admin, context, doctor, plus
// the import, review, adapter and token commands still on stdlib flag.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/agentic-substrate/substrate/internal/cli"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run dispatches the four commands still written against stdlib flag, then
// hands everything else to the cobra tree. There is no fall-through: a verb
// nobody recognises reaches cobra, which fails with a suggestion and a
// non-zero exit rather than silently printing the version and exiting 0.
func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "token":
			return tokenCmd(args[1:], os.Stdout)
		case "import":
			return importCmd(args[1:], os.Stdout, os.Stderr)
		case "review":
			return reviewCmd(args[1:], os.Stdout, os.Stderr)
		case "adapter":
			return adapterCmd(args[1:], os.Stdout, os.Stderr, nil)
		}
	}
	root := cli.NewRoot(cli.Deps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Env:    os.Getenv,
		Getwd:  os.Getwd,
	})
	root.SetArgs(args)
	err := root.Execute()
	var ue *cli.UserError
	if errors.As(err, &ue) {
		// main owns error printing (SilenceErrors), so the what/why/next
		// reaches stderr exactly once.
		return ue
	}
	return err
}
