// Command substrate-adapter is the per-machine daemon. It renders instruction
// and preference files from the server, links approved skills, drains the
// hook-capture outbox, and refreshes the offline memory cache.
//
// Phase 1 status: version reporting only. Loops land with the render surface.
package main

import (
	"flag"
	"fmt"

	"github.com/jacorbello/substrate/internal/version"
)

func main() {
	flag.Parse()
	fmt.Printf("substrate-adapter %s (%s)\n", version.Version, version.Revision())
}
