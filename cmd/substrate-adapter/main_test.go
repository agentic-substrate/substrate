package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/scope"

	"github.com/google/uuid"
)

// internal/adapter guards an empty -scope in two places: homeScope refuses to
// file a home-scoped drift proposal without one, and observationScope refuses
// to enqueue an observation without one. Both check for "".
//
// A non-empty default here defeats both. `global:` was the default, and
// policy.Check refuses every write at global for an agent token, so the guards
// never fired and the server refused the work instead -- a guard that cannot
// fire, one layer below where anyone was looking.
//
// The default must therefore be empty, or a scope an agent may actually write.
// Restoring `global:` is the one-line change that makes this red.
func TestScopeFlagDefaultCannotDefeatTheGuards(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	got, found := "", false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 3 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "String" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "flag" {
			return true
		}
		name, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		if unquote(t, name.Value) != "scope" {
			return true
		}
		def, ok := call.Args[1].(*ast.BasicLit)
		if !ok {
			t.Fatal(`-scope default is not a string literal; this test can no longer read it`)
		}
		got, found = unquote(t, def.Value), true
		return false
	})
	if !found {
		t.Fatal(`no flag.String("scope", ...) found in main.go; if the flag was renamed, update this test rather than deleting it`)
	}

	if got == "" {
		return // Unset: both guards fire and skip-with-log, which is the intent.
	}

	sc, err := scope.Parse(got)
	if err != nil {
		t.Fatalf("-scope default %q does not parse: %v", got, err)
	}
	agent := identity.Principal{
		ID:           uuid.New(),
		Kind:         "agent",
		Trust:        "agent_autonomous",
		TeamIDs:      []uuid.UUID{uuid.New()},
		Capabilities: []string{"memory:write"},
	}
	if err := policy.Check("memory.write", sc, agent); err != nil {
		t.Fatalf("-scope defaults to %q, which no agent token may write (%v).\n"+
			"That default defeats the empty-scope guards in homeScope and observationScope: "+
			"they check for \"\", so a non-empty unusable default means the work is filed and "+
			"refused by the server instead of skipped and logged. Use \"\" instead.", got, err)
	}
}

func unquote(t *testing.T, lit string) string {
	t.Helper()
	s, err := strconv.Unquote(lit)
	if err != nil {
		t.Fatalf("unquote %s: %v", lit, err)
	}
	return s
}
