package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Turn red by hoisting any command, flag or printer into a package-level
// variable: two roots then share it and this fails intermittently under -race,
// which is exactly the failure mode that is hard to diagnose later.
func TestTwoRootsRunConcurrentlyWithoutCrosstalk(t *testing.T) {
	type run struct {
		org string
		out bytes.Buffer
	}
	runs := []*run{{org: "alpha"}, {org: "bravo"}}
	dirs := []string{t.TempDir(), t.TempDir()}
	for i, r := range runs {
		if err := writeJSON0600(filepath.Join(dirs[i], ContextFileName), Scope{Org: r.org}); err != nil {
			t.Fatalf("seed context: %v", err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, len(runs))
	for i, r := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			root := NewRoot(Deps{ConfigDir: dirs[i], Stdout: &r.out, Stderr: &r.out})
			root.SetOut(&r.out)
			root.SetErr(&r.out)
			root.SetArgs([]string{"context", "show", "--json"})
			errs[i] = root.Execute()
		}()
	}
	wg.Wait()
	for i, r := range runs {
		if errs[i] != nil {
			t.Fatalf("root %d: %v\n%s", i, errs[i], r.out.String())
		}
		if !strings.Contains(r.out.String(), r.org) {
			t.Fatalf("root %d lost its own org %q:\n%s", i, r.org, r.out.String())
		}
		other := runs[1-i].org
		if strings.Contains(r.out.String(), other) {
			t.Fatalf("root %d saw the other root's org %q:\n%s", i, other, r.out.String())
		}
	}
}

// Turn red by restoring main.go's fall-through: `substrate frobnicate` then
// prints the version banner and exits 0.
func TestUnknownCommandFailsWithASuggestion(t *testing.T) {
	out, err := runRoot(t, Deps{ConfigDir: t.TempDir()}, "contxt")
	if err == nil {
		t.Fatalf("unknown command exited 0:\n%s", out)
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error does not name the unknown command: %v", err)
	}
}

// Turn red by dropping the doctor command: three of its four checks read the
// config this package writes, and it is the entry point every fix message
// points at.
func TestDoctorReportsEveryCheckAndFailsWhenAnyDoes(t *testing.T) {
	out, err := runRoot(t, Deps{
		ConfigDir: t.TempDir(),
		Getwd:     func() (string, error) { return t.TempDir(), nil },
	}, "doctor")
	if err == nil {
		t.Fatalf("doctor passed with no config at all:\n%s", out)
	}
	for _, want := range []string{"config", "credential", "server", "scope", "fix:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}
