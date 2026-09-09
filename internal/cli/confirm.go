package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// confirm gates a write on a human. --yes passes straight through; on a
// terminal the operator types the word; anywhere else the command refuses.
//
// The refusal is the point. Scope now falls back to the git remote, and
// h.repoScope deliberately never discloses the resolved chain, so an
// unconfirmed apply in the wrong checkout writes at a chain nobody ever saw
// and nobody can read back afterwards.
func confirm(d Deps, op string, yes bool) error {
	if yes {
		return nil
	}
	f, ok := d.Stdin.(*os.File)
	if !ok || !isTerminal(f) {
		return &UserError{
			What: op + ": refusing to write without confirmation",
			Why:  "stdin is not a terminal, so there is nobody to answer the prompt",
			Next: "re-run with --yes once the scope printed above is the one you meant",
		}
	}
	if _, err := fmt.Fprint(d.stdout(), "type 'yes' to continue: "); err != nil {
		return err
	}
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return &UserError{
			What: op + ": could not read the confirmation",
			Why:  err.Error(),
			Next: "re-run with --yes",
		}
	}
	if strings.TrimSpace(strings.ToLower(line)) != "yes" {
		return &UserError{
			What: op + ": not confirmed",
			Why:  "the answer was not 'yes'",
			Next: "re-run and answer yes, or pass --yes",
		}
	}
	return nil
}

// isTerminal reports whether f is a character device. Stdlib only: the
// alternative is a dependency for one bit.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
