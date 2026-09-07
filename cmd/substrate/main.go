// Command substrate is the operator CLI: import, review, token, offload, doctor.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/agentic-substrate/substrate/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "token":
			if err := tokenCmd(os.Args[2:], os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "import":
			if err := importCmd(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "review":
			if err := reviewCmd(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}
	flag.Parse()
	fmt.Printf("substrate %s (%s)\n", version.Version, version.Revision())
}
