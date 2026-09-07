package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/importer"
)

func TestImportUsage(t *testing.T) {
	err := importCmd(nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "apply") {
		t.Fatalf("got %v, want usage naming apply", err)
	}
}

func TestImportUnknownCommand(t *testing.T) {
	err := importCmd([]string{"frob"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("got %v, want unknown command", err)
	}
}

func TestImportScanRequiresRoot(t *testing.T) {
	// Defaulting empty -root to os.Getenv("HOME") is the one-line change that makes this red.
	canary := t.TempDir()
	mustWriteCLI(t, filepath.Join(canary, ".claude", "CLAUDE.md"), "# Canary\nfrom HOME\n")
	t.Setenv("HOME", canary)
	t.Setenv("PATH", t.TempDir())
	out := filepath.Join(t.TempDir(), "inventory.json")
	err := importCmd([]string{"scan", "-hostname", "wsl", "-out", out}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-root") {
		t.Fatalf("got %v, want -root required", err)
	}
}

func TestImportScanRejectsOutInsideRoot(t *testing.T) {
	// Deleting the inside-root check in importScan is the one-line change that makes this red.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	target := filepath.Join(root, ".claude", "CLAUDE.md")
	body := "# Shared\nAlways run gofmt.\n"
	mustWriteCLI(t, target, body)

	err := importCmd([]string{"scan", "-root", root, "-hostname", "wsl", "-out", target}, &bytes.Buffer{}, &bytes.Buffer{})
	got, readErr := os.ReadFile(target) //nolint:gosec // target is under t.TempDir
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != body {
		t.Fatalf("scan replaced a file inside -root:\n%s", got)
	}
	if err == nil || !strings.Contains(err.Error(), "-out") {
		t.Fatalf("got %v, want -out inside -root rejected", err)
	}
}

func TestImportPlanRejectsOutEqualToInventoryPath(t *testing.T) {
	// Deleting the inventory-Path check in importPlan is the one-line change that makes this red.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	tmp := t.TempDir()
	root := filepath.Join(tmp, "machine")
	claude := filepath.Join(root, ".claude", "CLAUDE.md")
	body := "# Shared\nAlways run gofmt.\n"
	mustWriteCLI(t, claude, body)

	invPath := filepath.Join(tmp, "inventory.json")
	if err := importCmd([]string{"scan", "-root", root, "-hostname", "wsl", "-out", invPath}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("scan: %v", err)
	}

	err := importCmd([]string{"plan", "-out", claude, invPath}, &bytes.Buffer{}, &bytes.Buffer{})
	got, readErr := os.ReadFile(claude) //nolint:gosec // claude is under t.TempDir
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != body {
		t.Fatalf("plan replaced an inventoried path:\n%s", got)
	}
	if err == nil || !strings.Contains(err.Error(), "-out") {
		t.Fatalf("got %v, want -out equal to inventory Path rejected", err)
	}
}

func TestImportScanRequiresHostname(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "inventory.json")
	err := importCmd([]string{"scan", "-root", root, "-out", out}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-hostname") {
		t.Fatalf("got %v, want -hostname required", err)
	}
}

func TestImportScanRequiresOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	err := importCmd([]string{"scan", "-root", root, "-hostname", "wsl"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-out") {
		t.Fatalf("got %v, want -out required", err)
	}
}

func TestImportScanOutModeIsAlways0600(t *testing.T) {
	// os.WriteFile on an existing file keeps the old mode; that is the one-line change that makes this red.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Shared\nAlways run gofmt.\n")
	out := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(out, []byte("{}\n"), 0o644); err != nil { //nolint:gosec // fixture must start world-readable
		t.Fatal(err)
	}

	if err := importCmd([]string{"scan", "-root", root, "-hostname", "wsl", "-out", out}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode %o, want 0600", perm)
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
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
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

func TestImportApplyDefaultsToDryRun(t *testing.T) {
	// Defaulting Commit to true when -commit is omitted is the one-line
	// change that makes this red.
	planPath := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"blocks":[],"conflicts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var sawCommit *bool
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/import" || r.Method != http.MethodPost {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		body, _ = io.ReadAll(r.Body)
		var in struct {
			Commit *bool `json:"commit"`
			DryRun bool  `json:"dry_run"`
		}
		_ = json.Unmarshal(body, &in)
		sawCommit = in.Commit
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dry_run":true,"by_hostname":{"mac":{"active":1,"proposed":1,"conflict":1}}}`))
	}))
	t.Cleanup(srv.Close)

	stdout := &bytes.Buffer{}
	err := importCmd([]string{
		"apply", "-machine", "mac", "-trusted", "mac",
		"-server", srv.URL, "-token", "alice",
		"-scope", "global:/org:acme/team:core/project:plotlens",
		planPath,
	}, stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if sawCommit != nil && *sawCommit {
		t.Fatal("apply without -commit posted commit=true")
	}
	if !strings.Contains(string(body), `"dry_run":true`) && (sawCommit == nil || *sawCommit) {
		t.Fatalf("apply without -commit must be dry-run; body %s", body)
	}
	if stdout.Len() == 0 {
		t.Fatal("dry-run printed nothing")
	}
}

func TestImportApplyDryRunDoesNotWriteLocalFiles(t *testing.T) {
	// Writing plan.json or a file under the fixture tree from apply is the
	// change that makes this red.
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Shared\nAlways run gofmt.\n")
	planPath := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"blocks":[],"conflicts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, root)
	planBefore := snapshotDir(t, filepath.Dir(planPath))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dry_run":true,"active":[{"hostname":"mac","kind":"instruction","status":"active","hash":"abc","body":"Always run gofmt."}]}`))
	}))
	t.Cleanup(srv.Close)

	stdout := &bytes.Buffer{}
	err := importCmd([]string{
		"apply", "-machine", "mac", "-trusted", "mac",
		"-server", srv.URL, "-token", "alice",
		"-scope", "global:/org:acme/team:core/project:plotlens",
		planPath,
	}, stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("dry-run printed nothing")
	}
	if snapshotDir(t, root) != before {
		t.Fatal("apply --dry-run mutated the fixture tree")
	}
	if snapshotDir(t, filepath.Dir(planPath)) != planBefore {
		t.Fatal("apply --dry-run mutated files next to plan.json")
	}
}

func snapshotDir(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.WriteString(filepath.ToSlash(rel))
		b.WriteByte(' ')
		b.WriteString(info.Mode().String())
		if !d.IsDir() {
			body, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir
			if err != nil {
				return err
			}
			sum := sha256.Sum256(body)
			b.WriteByte(' ')
			b.WriteString(hex.EncodeToString(sum[:]))
		}
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
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

func TestImportScanExcludeFlagDropsMatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Mine\nAlways run gofmt.\n")
	mustWriteCLI(t, filepath.Join(root, "fixtures", "eval", "AGENTS.md"), "# Fixture\n")
	out := filepath.Join(t.TempDir(), "inventory.json")

	err := importCmd([]string{
		"scan", "-root", root, "-hostname", "wsl", "-out", out,
		"-exclude", "fixtures/**",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	body, err := os.ReadFile(out) //nolint:gosec // out is a t.TempDir path built by this test
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	if strings.Contains(string(body), "fixtures/eval/AGENTS.md") {
		t.Fatal("-exclude did not drop the matching path from inventory.json")
	}
	if !strings.Contains(string(body), ".claude/CLAUDE.md") {
		t.Fatal("-exclude dropped a path it should have kept")
	}
}
