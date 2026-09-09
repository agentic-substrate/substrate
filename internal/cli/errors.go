package cli

import "fmt"

// UserError is a failure the operator can act on. What names the thing that
// failed, Why states the cause in the operator's terms, and Next is the single
// command or edit that fixes it -- a message without a Next is a dead end, so
// every construction site fills all three.
type UserError struct {
	What string
	Why  string
	Next string
}

// Error renders the three parts on separate lines. main prints this verbatim;
// SilenceErrors keeps cobra from printing it a second time.
func (e *UserError) Error() string {
	return fmt.Sprintf("%s\n  why:  %s\n  next: %s", e.What, e.Why, e.Next)
}
