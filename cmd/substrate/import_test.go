package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/importer"
)

func TestImportUsage(t *testing.T) {
	err := importCmd(nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "scan or plan") {
		t.Fatalf("got %v, want usage", err)
	}
}

func TestImportUnknownCommand(t *testing.T) {
	err := importCmd([]string{"frob"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("got %v, want unknown command", err)
	}
}

func TestImportScanRequiresRoot(t *testing.T) {
	out := filepath.Join(t.TempDir(), "inventory.json")
	err := importCmd([]string{"scan", "-hostname", "wsl", "-out", out}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-root") {
		t.Fatalf("got %v, want -root required", err)
	}
}

func TestImportScanRequiresHostname(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "inventory.json")
	err := importCmd([]string{"scan", "-root", root, "-out", out}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-hostname") {
		t.Fatalf("got %v, want -hostname required", err)
	}
}

func TestImportScanRequiresOut(t *testing.T) {
	root := t.TempDir()
	err := importCmd([]string{"scan", "-root", root, "-hostname", "wsl"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-out") {
		t.Fatalf("got %v, want -out required", err)
	}
}

func TestImportPlanRequiresOut(t *testing.T) {
	inv := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(inv, []byte(`{"hostname":"wsl"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := importCmd([]string{"plan", inv}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-out") {
		t.Fatalf("got %v, want -out required", err)
	}
}

func TestImportPlanTwoMachinesWritesPlanJSON(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // keep LookPath from finding a real memorix
	tmp := t.TempDir()
	aHome := filepath.Join(tmp, "machine-a")
	bHome := filepath.Join(tmp, "machine-b")
	mustWriteCLI(t, filepath.Join(aHome, ".claude", "CLAUDE.md"), ""+
		"# Shared\n"+
		"Always run gofmt.\n"+
		"\n"+
		"# Indent\n"+
		"Prefer tabs.\n")
	mustWriteCLI(t, filepath.Join(bHome, ".claude", "CLAUDE.md"), ""+
		"# Shared\n"+
		"Always run gofmt.\n"+
		"\n"+
		"# Indent\n"+
		"Prefer spaces.\n")

	invA := filepath.Join(tmp, "inventory-a.json")
	invB := filepath.Join(tmp, "inventory-b.json")
	planPath := filepath.Join(tmp, "plan.json")

	if err := importCmd([]string{"scan", "-root", aHome, "-hostname", "wsl", "-out", invA}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("scan a: %v", err)
	}
	if err := importCmd([]string{"scan", "-root", bHome, "-hostname", "mac", "-out", invB}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("scan b: %v", err)
	}
	if err := importCmd([]string{"plan", "-out", planPath, invA, invB}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("plan: %v", err)
	}

	raw, err := os.ReadFile(planPath) //nolint:gosec // planPath is under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	var got importer.Plan
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	sharedHits := 0
	for _, b := range got.Blocks {
		if strings.Contains(b.Body, "Always run gofmt.") {
			sharedHits++
		}
		if strings.Contains(b.Body, "Prefer tabs.") || strings.Contains(b.Body, "Prefer spaces.") {
			t.Fatalf("differing block in blocks: %q", b.Body)
		}
	}
	if sharedHits != 1 {
		t.Fatalf("identical block appeared %d times, want 1\n%s", sharedHits, raw)
	}
	if len(got.Conflicts) != 1 || len(got.Conflicts[0].Pair) != 2 {
		t.Fatalf("want one conflict pair\n%s", raw)
	}
	hosts := map[string]string{}
	for _, side := range got.Conflicts[0].Pair {
		if len(side.Hostnames) != 1 {
			t.Fatalf("side hostnames %#v", side.Hostnames)
		}
		hosts[side.Hostnames[0]] = side.Body
	}
	if !strings.Contains(hosts["wsl"], "Prefer tabs.") || !strings.Contains(hosts["mac"], "Prefer spaces.") {
		t.Fatalf("conflict hosts %#v", hosts)
	}

	// Scan must not have written anything under the machine trees besides the fixtures.
	if _, err := os.Stat(filepath.Join(aHome, "inventory.json")); !os.IsNotExist(err) {
		t.Fatalf("scan wrote inside machine-a: %v", err)
	}
}

func mustWriteCLI(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
