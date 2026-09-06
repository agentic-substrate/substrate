// Command substrate is the operator CLI: import, review, token, offload, doctor.
//
// Phase 1 status: version reporting only.
package main

import (
	"flag"
	"fmt"

	"github.com/jacorbello/substrate/internal/version"
)

func main() {
	flag.Parse()
	fmt.Printf("substrate %s (%s)\n", version.Version, version.Revision())
}

// canary: a cmd/ surface change with no README update.
