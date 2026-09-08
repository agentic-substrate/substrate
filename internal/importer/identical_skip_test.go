package importer

import (
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/identity"
)

// identicalActive dedupes on body text alone, across the whole scope chain,
// before rel/heading/key are consulted. A genuinely new bullet whose wording
// coincides with any active row anywhere is dropped — and the drop used to be
// silent, which is Gotcha 6's failure mode: the query just returns fewer rows.
// The matching stays as it is; only the silence goes.
func TestApplySkipIsVisibleWhenBodyCoincidesWithAnActiveRow(t *testing.T) {
	dsn, conn := startMigrated(t)
	w := seedImportWorld(t, conn)
	st := openStore(t, dsn)
	ctx := identity.WithPrincipal(t.Context(), w.aliceP)

	// seedImportWorld leaves an active instruction ci.required = "true".
	// This block is a different rule in a different file that happens to say
	// the same word.
	plan := Plan{Blocks: []Block{{
		Hash: sha256Hex([]byte("true")), Heading: "Flags", Body: "true",
		Kind: "instruction", Rel: "AGENTS.md", Ordinal: 0,
		Sources: []Source{{Hostname: "mac"}},
	}}}
	res, err := Apply(ctx, st, ApplyRequest{
		Plan: plan, Machine: "mac", TrustedMachine: "mac", Scope: w.pathStr, Commit: true,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res.Active)+len(res.Proposed) != 0 {
		t.Fatalf("the coincidental body was written after all: %#v", res)
	}
	if len(res.Skipped) == 0 {
		t.Fatal("a block was dropped with no Skipped entry: the drop is invisible")
	}
	var found bool
	for _, s := range res.Skipped {
		if strings.Contains(s.Reason, "ci.required") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the skip does not name what the body matched: %#v", res.Skipped)
	}
}
