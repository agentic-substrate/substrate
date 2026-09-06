// Command substrate is the operator CLI: import, review, token, offload, doctor.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/agentic-substrate/substrate/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "token" {
		if err := tokenCmd(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	flag.Parse()
	fmt.Printf("substrate %s (%s)\n", version.Version, version.Revision())
}
