package importer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanRequiresRoot(t *testing.T) {
	// Defaulting empty Roots to os.Getenv("HOME") is the one-line change that makes this red.
	canary := t.TempDir()
	mustWrite(t, filepath.Join(canary, ".claude", "CLAUDE.md"), "# Canary\nfrom HOME\n")
	t.Setenv("HOME", canary)
	t.Setenv("PATH", t.TempDir())

	_, err := Scan(Request{Hostname: "wsl"})
	if err == nil || !strings.Contains(err.Error(), "-root") {
		t.Fatalf("got %v, want error naming -root", err)
	}
}

func TestScanRefusesRelativeRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	_, err := Scan(Request{Roots: []string{"."}, Hostname: "wsl"})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("got %v, want absolute-path error", err)
	}
}

func TestScanDoesNotReadHOMEWhenRootGiven(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".claude", "CLAUDE.md"), "# HomeCanary\nsecret-from-home\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Shared\nAlways run gofmt.\n")

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Always run gofmt.") {
		t.Fatalf("scan missed the -root tree:\n%s", raw)
	}
	if strings.Contains(string(raw), "secret-from-home") || strings.Contains(string(raw), home) {
		t.Fatalf("scan consulted $HOME:\n%s", raw)
	}
}

func TestScanDoesNotFollowSymlinkOutsideRoot(t *testing.T) {
	// Replacing Lstat with os.ReadFile (which follows links) is the one-line change that makes this red.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	outside := t.TempDir()
	secret := filepath.Join(outside, "id_rsa")
	mustWrite(t, secret, "SECRET-KEY-MATERIAL\n")

	root := t.TempDir()
	link := filepath.Join(root, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET-KEY-MATERIAL") || strings.Contains(string(raw), secret) {
		t.Fatalf("scan followed a symlink outside -root:\n%s", raw)
	}
	for _, f := range inv.Files {
		if f.Rel == ".claude/CLAUDE.md" {
			t.Fatalf("inventoried symlink %s", f.Path)
		}
	}
}

func TestScanDoesNotWriteUnderRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, ".claude", "CLAUDE.md")
	mustWrite(t, path, "# Shared\nAlways run gofmt.\n")
	makeReadOnly(t, root)
	before := treeSnapshot(t, root)

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Files) == 0 {
		t.Fatal("scan returned no files; a no-op would also look read-only")
	}
	after := treeSnapshot(t, root)
	if after != before {
		t.Fatalf("scan mutated the root\nbefore %s\nafter  %s", before, after)
	}
}

func TestScanInventoriesHarnessFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	claude := "# Shared\nAlways run gofmt.\n"
	agents := "# Codex\nPrefer terse diffs.\n"
	cursor := "# Cursor\nAlwaysApply.\n"
	repoAgents := "# Repo\nUse modules.\n"
	mustWrite(t, filepath.Join(root, ".claude", "CLAUDE.md"), claude)
	mustWrite(t, filepath.Join(root, ".codex", "AGENTS.md"), agents)
	mustWrite(t, filepath.Join(root, ".cursor", "rules", "team.mdc"), cursor)
	mustWrite(t, filepath.Join(root, "plotlens", "api", "AGENTS.md"), repoAgents)

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Hostname != "wsl" {
		t.Fatalf("hostname %q, want wsl", inv.Hostname)
	}
	byRel := map[string]File{}
	for _, f := range inv.Files {
		byRel[filepath.ToSlash(f.Rel)] = f
	}
	assertFile := func(rel, detected, scope, body string) {
		t.Helper()
		f, ok := byRel[rel]
		if !ok {
			t.Fatalf("missing %s in %#v", rel, byRel)
		}
		if f.DetectedType != detected || f.ImpliedScope != scope {
			t.Fatalf("%s: type %q scope %q, want %q %q", rel, f.DetectedType, f.ImpliedScope, detected, scope)
		}
		if f.Content != body {
			t.Fatalf("%s content %q, want %q", rel, f.Content, body)
		}
		if f.Size != int64(len(body)) {
			t.Fatalf("%s size %d, want %d", rel, f.Size, len(body))
		}
		if f.Hash != sha256Hex([]byte(body)) {
			t.Fatalf("%s hash %s, want %s", rel, f.Hash, sha256Hex([]byte(body)))
		}
		if f.Path != filepath.Join(root, filepath.FromSlash(rel)) {
			t.Fatalf("%s path %q", rel, f.Path)
		}
	}
	assertFile(".claude/CLAUDE.md", "claude", "user", claude)
	assertFile(".codex/AGENTS.md", "agents", "user", agents)
	assertFile(".cursor/rules/team.mdc", "cursor", "user", cursor)
	assertFile("plotlens/api/AGENTS.md", "agents", "repo", repoAgents)
}

func TestScanDoesNotExecMemorix(t *testing.T) {
	// Restoring collectMemorix's exec.LookPath/runMemorix path is the change that makes this red.
	t.Setenv("HOME", t.TempDir())
	binDir := t.TempDir()
	sentinel := filepath.Join(t.TempDir(), "memorix-ran")
	fake := filepath.Join(binDir, "memorix")
	script := "#!/bin/sh\nprintf ran > '" + sentinel + "'\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture binary
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Shared\nAlways run gofmt.\n")

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("scan exec'd memorix; the live store is not a scan input")
	}
	for _, f := range inv.Files {
		if f.DetectedType == "memorix" {
			t.Fatalf("scan produced a memorix file without --memorix-json: %#v", f)
		}
	}

	exportPath := filepath.Join(t.TempDir(), "memorix.json")
	if err := os.WriteFile(exportPath, []byte(`{"memories":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err = Scan(Request{Roots: []string{root}, Hostname: "wsl", MemorixJSON: exportPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("scan exec'd memorix while reading --memorix-json")
	}
	found := false
	for _, f := range inv.Files {
		if f.DetectedType == "memorix" {
			found = true
		}
	}
	if !found {
		t.Fatal("want memorix file from --memorix-json")
	}
}

func TestScanSkipsMissingMemorix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Shared\nAlways run gofmt.\n")

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Skipped) == 0 {
		t.Fatal("want a skipped memorix source, got none")
	}
	found := false
	for _, s := range inv.Skipped {
		if s.Source == "memorix" && s.Reason != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("skipped = %#v, want memorix with a reason", inv.Skipped)
	}
	for _, f := range inv.Files {
		if f.DetectedType == "memorix" {
			t.Fatalf("skipped memorix still produced a memorix file: %#v", f)
		}
	}
}

func TestScanRecordsMemorixExportWithoutParsing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Shared\nAlways run gofmt.\n")
	payload := []byte(`{"memories":[{"id":"raw-export"}]}`)
	exportPath := filepath.Join(t.TempDir(), "memorix.json")
	if err := os.WriteFile(exportPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	inv, err := Scan(Request{
		Roots:       []string{root},
		Hostname:    "wsl",
		MemorixJSON: exportPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got File
	for _, f := range inv.Files {
		if f.DetectedType == "memorix" {
			got = f
		}
	}
	if got.Content != string(payload) {
		t.Fatalf("memorix content %q, want raw export", got.Content)
	}
}

func TestPlanSameFileTwoParagraphsAreBlocksNotConflicts(t *testing.T) {
	// Treating any slot with two hashes as a conflict is the one-line change that makes this red.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	tmp := t.TempDir()
	home := filepath.Join(tmp, "machine")
	mustWrite(t, filepath.Join(home, ".claude", "CLAUDE.md"), ""+
		"# Shared\n"+
		"Always run gofmt.\n"+
		"\n"+
		"Use modules.\n")

	inv, err := Scan(Request{Roots: []string{home}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]Inventory{*inv}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("conflicts = %d, want 0 for two paragraphs in one file\n%s", len(plan.Conflicts), raw)
	}
	if len(plan.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2\n%s", len(plan.Blocks), raw)
	}
}

func TestPlanTwoMachinesDifferingClaude(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	tmp := t.TempDir()
	aHome := filepath.Join(tmp, "machine-a")
	bHome := filepath.Join(tmp, "machine-b")
	mustWrite(t, filepath.Join(aHome, ".claude", "CLAUDE.md"), ""+
		"# Shared\n"+
		"Always run gofmt.\n"+
		"\n"+
		"# Indent\n"+
		"Prefer tabs.\n")
	mustWrite(t, filepath.Join(bHome, ".claude", "CLAUDE.md"), ""+
		"# Shared\n"+
		"Always run gofmt.\n"+
		"\n"+
		"# Indent\n"+
		"Prefer spaces.\n")

	invA, err := Scan(Request{Roots: []string{aHome}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	invB, err := Scan(Request{Roots: []string{bHome}, Hostname: "mac"})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := BuildPlan([]Inventory{*invA, *invB}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var got Plan
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	sharedHits := 0
	for _, b := range got.Blocks {
		if strings.Contains(b.Body, "Always run gofmt.") {
			sharedHits++
			hosts := hostSet(b.Sources)
			if !hosts["wsl"] || !hosts["mac"] {
				t.Fatalf("shared block sources %#v, want wsl and mac", b.Sources)
			}
		}
		if strings.Contains(b.Body, "Prefer tabs.") || strings.Contains(b.Body, "Prefer spaces.") {
			t.Fatalf("differing block %q listed under blocks; it belongs in a conflict pair", b.Body)
		}
	}
	if sharedHits != 1 {
		t.Fatalf("identical block appeared %d times, want 1\n%s", sharedHits, raw)
	}

	if len(got.Conflicts) != 1 {
		t.Fatalf("conflicts = %d, want 1 pair\n%s", len(got.Conflicts), raw)
	}
	pair := got.Conflicts[0].Pair
	if len(pair) != 2 {
		t.Fatalf("pair length %d, want 2\n%s", len(pair), raw)
	}
	bodies := map[string]string{}
	for _, side := range pair {
		if len(side.Hostnames) != 1 {
			t.Fatalf("conflict side hostnames %#v, want one hostname", side.Hostnames)
		}
		bodies[side.Hostnames[0]] = side.Body
	}
	if !strings.Contains(bodies["wsl"], "Prefer tabs.") {
		t.Fatalf("wsl side %q, want Prefer tabs", bodies["wsl"])
	}
	if !strings.Contains(bodies["mac"], "Prefer spaces.") {
		t.Fatalf("mac side %q, want Prefer spaces", bodies["mac"])
	}
}

func TestPlanDedupesBeforeClassify(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	tmp := t.TempDir()
	aHome := filepath.Join(tmp, "machine-a")
	bHome := filepath.Join(tmp, "machine-b")
	mustWrite(t, filepath.Join(aHome, ".claude", "CLAUDE.md"), ""+
		"# Shared\n"+
		"Always run gofmt.\n"+
		"\n"+
		"# Indent\n"+
		"Prefer tabs.\n")
	mustWrite(t, filepath.Join(bHome, ".claude", "CLAUDE.md"), ""+
		"# Shared\n"+
		"Always run gofmt.\n"+
		"\n"+
		"# Indent\n"+
		"Prefer spaces.\n")

	invA, err := Scan(Request{Roots: []string{aHome}, Hostname: "wsl"})
	if err != nil {
		t.Fatal(err)
	}
	invB, err := Scan(Request{Roots: []string{bHome}, Hostname: "mac"})
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	classified := map[string]int{}
	plan, err := BuildPlan([]Inventory{*invA, *invB}, func(b Block) Classification {
		calls++
		classified[b.Hash]++
		return Classify(b)
	})
	if err != nil {
		t.Fatal(err)
	}
	// 4 source blocks, 3 unique hashes. Classify-after-dedupe → 3 calls.
	// Moving classify before dedupe is the one-line change that makes this red.
	if calls != 3 {
		t.Fatalf("classify called %d times, want 3 (once per unique content hash); 4 means classify ran before dedupe", calls)
	}
	for h, n := range classified {
		if n != 1 {
			t.Fatalf("hash %s classified %d times; dedupe must run first", h, n)
		}
	}
	if len(plan.Blocks)+len(plan.Conflicts[0].Pair) != 3 {
		t.Fatalf("plan unique items %d+%d, want 3", len(plan.Blocks), len(plan.Conflicts[0].Pair))
	}
}

func TestClassifyNeverClaimsCertainty(t *testing.T) {
	cases := []Block{
		{Heading: "Indent", Body: "Use 4-space indentation.", ImpliedScope: "user", DetectedType: "claude", Rel: ".claude/CLAUDE.md"},
		{Heading: "Voice", Body: "I prefer terse replies.", ImpliedScope: "user", DetectedType: "claude", Rel: ".claude/CLAUDE.md"},
		{Heading: "Shared", Body: "Always run gofmt.", ImpliedScope: "user", DetectedType: "claude", Rel: ".claude/CLAUDE.md"},
		{Heading: "Python", Body: "Use 3.12", ImpliedScope: "repo", DetectedType: "agents", Rel: "plotlens/api/AGENTS.md"},
	}
	for _, b := range cases {
		got := Classify(b)
		if got.Confidence >= 1 {
			t.Fatalf("classify(%q) confidence %v claims certainty; R15 forbids that", b.Body, got.Confidence)
		}
		if got.Kind != "instruction" && got.Kind != "preference" {
			t.Fatalf("kind %q, want instruction or preference", got.Kind)
		}
		if got.Note == "" {
			t.Fatalf("classify(%q) missing note that the heuristic will misfile", b.Body)
		}
	}
}

func TestClassifyStyleAllowlistIsPreference(t *testing.T) {
	got := Classify(Block{Heading: "Indent", Body: "Use 4-space indentation.", ImpliedScope: "repo", Rel: "AGENTS.md"})
	if got.Kind != "preference" {
		t.Fatalf("kind %q, want preference", got.Kind)
	}
}

func TestClassifyFirstPersonUserGlobalIsPreference(t *testing.T) {
	got := Classify(Block{Body: "I prefer terse replies.", ImpliedScope: "user", Rel: ".claude/CLAUDE.md"})
	if got.Kind != "preference" {
		t.Fatalf("kind %q, want preference", got.Kind)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// makeReadOnly turns the tree unwritable so a write (including write-then-delete)
// fails the scan instead of looking clean in a post-hoc snapshot. Restores
// permissions so t.TempDir cleanup can remove the tree.
func makeReadOnly(t *testing.T, root string) {
	t.Helper()
	var dirs, files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range files {
		if err := os.Chmod(p, 0o444); err != nil { //nolint:gosec // fixture must be unwritable
			t.Fatal(err)
		}
	}
	for _, p := range dirs {
		if err := os.Chmod(p, 0o555); err != nil { //nolint:gosec // fixture must be unwritable
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, p := range dirs {
			_ = os.Chmod(p, 0o750) //nolint:gosec // restore so TempDir cleanup can run
		}
		for _, p := range files {
			_ = os.Chmod(p, 0o600) //nolint:gosec // restore so TempDir cleanup can run
		}
	})
}

func treeSnapshot(t *testing.T, root string) string {
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
			b.WriteByte(' ')
			b.WriteString(sha256Hex(body))
		}
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func hostSet(sources []Source) map[string]bool {
	m := map[string]bool{}
	for _, s := range sources {
		m[s.Hostname] = true
	}
	return m
}

func TestScanRecordsUnreadableDirAndKeepsWalking(t *testing.T) {
	// Returning the WalkDir error instead of recording it is the one-line
	// change that makes this red.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Shared\nAlways run gofmt.\n")

	locked := filepath.Join(root, "locked")
	mustWrite(t, filepath.Join(locked, "AGENTS.md"), "# Hidden\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) }) //nolint:gosec // restoring a test fixture directory so t.TempDir cleanup can remove it
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("running as root; an unreadable directory cannot be simulated")
	}

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatalf("scan aborted on an unreadable directory: %v", err)
	}
	if len(inv.Files) == 0 {
		t.Fatal("scan returned no files; it must keep walking past an unreadable directory")
	}
	var found bool
	for _, s := range inv.Skipped {
		if strings.Contains(s.Source, "locked") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unreadable directory was not recorded in Skipped: %+v", inv.Skipped)
	}
}

func TestScanSkipsVendoredAndCachedConfig(t *testing.T) {
	// Removing any of these names from skipDirs is the one-line change that
	// makes this red.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Mine\nAlways run gofmt.\n")
	for _, noise := range []string{
		filepath.Join(".cargo", "registry", "src", "zerocopy-0.8.40", "AGENTS.md"),
		filepath.Join("plugins", "cache", "somevendor", "AGENTS.md"),
		filepath.Join("plugins", "marketplaces", "somevendor", "AGENTS.md"),
		filepath.Join(".local", "share", "containers", "storage", "overlay", "diff", "AGENTS.md"),
		filepath.Join(".venv", "lib", "site-packages", "pkg", "AGENTS.md"),
	} {
		mustWrite(t, filepath.Join(root, noise), "# NotMine\nVendored rules.\n")
	}

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(inv.Files) != 1 {
		var got []string
		for _, f := range inv.Files {
			got = append(got, f.Rel)
		}
		t.Fatalf("scan inventoried %d files, want only the operator's own: %v", len(inv.Files), got)
	}
	if inv.Files[0].Rel != ".claude/CLAUDE.md" {
		t.Fatalf("kept %q, want .claude/CLAUDE.md", inv.Files[0].Rel)
	}
}

func TestScanSkipsDependencyCachesByPath(t *testing.T) {
	// Removing any entry from skipPathContains is the one-line change that
	// makes this red.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "CLAUDE.md"), "# Mine\nAlways run gofmt.\n")
	for _, noise := range []string{
		filepath.Join("go", "pkg", "mod", "modernc.org", "sqlite@v1.57.0", "CLAUDE.md"),
		filepath.Join(".codex", ".tmp", "plugins", "zoom", "AGENTS.md"),
		filepath.Join(".claude", "plugins", "cached", "AGENTS.md"),
	} {
		mustWrite(t, filepath.Join(root, noise), "# NotMine\nUpstream rules.\n")
	}

	inv, err := Scan(Request{Roots: []string{root}, Hostname: "wsl"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(inv.Files) != 1 || inv.Files[0].Rel != ".claude/CLAUDE.md" {
		var got []string
		for _, f := range inv.Files {
			got = append(got, f.Rel)
		}
		t.Fatalf("scan inventoried %v, want only .claude/CLAUDE.md", got)
	}
}
