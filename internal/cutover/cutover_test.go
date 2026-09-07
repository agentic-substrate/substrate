package cutover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/render"
)

func TestCutoverRequiresRoot(t *testing.T) {
	// Defaulting empty Roots to os.Getenv("HOME") is the one-line change that makes this red.
	canary := t.TempDir()
	mustWrite(t, filepath.Join(canary, ".claude", "CLAUDE.md"), "# from HOME\n")
	t.Setenv("HOME", canary)
	_, err := Cutover(Request{Commit: true, Installer: &FakeInstaller{}, Files: []Replacement{{
		Path:    filepath.Join(canary, ".claude", "CLAUDE.md"),
		Content: renderedClaude("2026-09-07T00:00:00Z"),
	}}})
	if err == nil || !strings.Contains(err.Error(), "-root") {
		t.Fatalf("got %v, want -root required", err)
	}
}

func TestCutoverRenamesRatherThanDeletes(t *testing.T) {
	// os.Remove of the live file, or truncating it in place, is the change
	// that makes this red. The original bytes must survive at *.pre-substrate.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	original := "# original\nalways gofmt\n"
	mustWriteMode(t, live, original, 0o640)
	origHash := sha256Hex([]byte(original))

	rendered := renderedClaude("2026-09-07T00:00:00Z")
	_, err := Cutover(Request{
		Roots:     []string{root},
		Commit:    true,
		Installer: &FakeInstaller{},
		Files:     []Replacement{{Path: live, Content: rendered}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backup := live + BackupSuffix
	got, err := os.ReadFile(backup) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatalf("displaced file missing (deleted rather than renamed): %v", err)
	}
	if sha256Hex(got) != origHash {
		t.Fatalf("backup hash %s, want original %s (truncated rather than renamed)", sha256Hex(got), origHash)
	}
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("backup mode %o, want 0640", info.Mode().Perm())
	}
	liveBody, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(liveBody) != rendered {
		t.Fatalf("live body %q, want rendered", liveBody)
	}
}

func TestCutoverRefusesExistingBackup(t *testing.T) {
	// os.Rename over an existing *.pre-substrate is the one-line change that
	// makes this red. That file is the one copy of an earlier cutover.
	root := t.TempDir()
	live := filepath.Join(root, ".codex", "AGENTS.md")
	mustWrite(t, live, "current original\n")
	earlier := "earlier-cutover-state\n"
	mustWrite(t, live+BackupSuffix, earlier)
	beforeLive, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}

	_, err = Cutover(Request{
		Roots:     []string{root},
		Commit:    true,
		Installer: &FakeInstaller{},
		Files:     []Replacement{{Path: live, Content: renderedClaude("2026-09-07T00:00:00Z")}},
	})
	if err == nil || !strings.Contains(err.Error(), BackupSuffix) {
		t.Fatalf("got %v, want existing %s refused", err, BackupSuffix)
	}
	gotBackup, err := os.ReadFile(live + BackupSuffix) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBackup) != earlier {
		t.Fatalf("overwrote earlier backup: %q", gotBackup)
	}
	gotLive, err := os.ReadFile(live) //nolint:gosec // under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(gotLive) != string(beforeLive) {
		t.Fatal("failed closed by mutating the live file after refusing the backup")
	}
}

func TestCutoverDryRunDoesNotWrite(t *testing.T) {
	// Applying renames/writes when Commit is false, returning empty Format,
	// or calling Installer.Install is the change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	original := "# original\n"
	mustWrite(t, live, original)
	mustWrite(t, filepath.Join(root, ".memorix", "memories.json"), `{"keep":true}`+"\n")
	before := snapshotTree(t, root)
	if strings.TrimSpace(before) == "" {
		t.Fatal("fixture snapshot was empty")
	}
	rendered := renderedClaude("2026-09-07T00:00:00Z")
	fake := &FakeInstaller{}
	rep, err := Cutover(Request{
		Roots:     []string{root},
		Commit:    false,
		Installer: fake,
		Files:     []Replacement{{Path: live, Content: rendered}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := rep.Format()
	if strings.TrimSpace(out) == "" {
		t.Fatal("dry-run printed nothing")
	}
	if !strings.Contains(out, live) {
		t.Fatalf("dry-run omitted target path:\n%s", out)
	}
	if !strings.Contains(out, sha256Hex([]byte(original))) {
		t.Fatalf("dry-run omitted current sha256:\n%s", out)
	}
	if !strings.Contains(out, sha256Hex([]byte(rendered))) {
		t.Fatalf("dry-run omitted replacement sha256:\n%s", out)
	}
	if !strings.Contains(out, live+BackupSuffix) {
		t.Fatalf("dry-run omitted *.pre-substrate rename:\n%s", out)
	}
	// Restoring unitRoots to an unconditional []string{DefaultMountRoot} is the
	// one-line change that makes this red: the plan would advertise a wider
	// scan set than this request harnessed.
	if rep.Unit == nil || len(rep.Unit.Roots) != 1 || rep.Unit.Roots[0] != root {
		var got []string
		if rep.Unit != nil {
			got = rep.Unit.Roots
		}
		t.Fatalf("unit roots %v, want [%s]", got, root)
	}
	if snapshotTree(t, root) != before {
		t.Fatal("cutover --dry-run mutated the fixture tree")
	}
	if len(fake.Installs) != 0 || fake.Uninstalls != 0 {
		t.Fatalf("unit was installed while dry-run was set: installs=%d uninstalls=%d", len(fake.Installs), fake.Uninstalls)
	}
}

func TestCutoverDryRunPlanEqualsCommit(t *testing.T) {
	// Building a different rename/write set for Commit vs dry-run is the
	// change that makes this red.
	newRoot := func() (string, []Replacement) {
		root := t.TempDir()
		live := filepath.Join(root, ".claude", "CLAUDE.md")
		mustWrite(t, live, "# original\n")
		mustWrite(t, filepath.Join(root, ".memorix", "db"), "store\n")
		rendered := renderedClaude("2026-09-07T00:00:00Z")
		return root, []Replacement{{Path: live, Content: rendered}}
	}
	dryRoot, files := newRoot()
	dry, err := Cutover(Request{Roots: []string{dryRoot}, Commit: false, Installer: &FakeInstaller{}, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	commitRoot, commitFiles := newRoot()
	committed, err := Cutover(Request{Roots: []string{commitRoot}, Commit: true, Installer: &FakeInstaller{}, Files: commitFiles})
	if err != nil {
		t.Fatal(err)
	}
	if plannedSet(dry) == "" {
		t.Fatal("dry-run planned set was empty")
	}
	if plannedSet(dry) != plannedSet(committed) {
		t.Fatalf("dry-run plan %q != commit plan %q", plannedSet(dry), plannedSet(committed))
	}
}

func TestCutoverUsesDriftHashNotWholeFile(t *testing.T) {
	// Storing sha256 of the whole rendered file as WouldDr is the one-line
	// change that makes this red (Gotcha 9: the footer carries generated-at).
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	mustWrite(t, live, "# original\n")
	a := renderedClaude("2026-09-07T00:00:00Z")
	b := renderedClaude("2099-12-31T23:59:59Z")
	if a == b {
		t.Fatal("fixture timestamps did not change the file")
	}
	if sha256Hex([]byte(a)) == sha256Hex([]byte(b)) {
		t.Fatal("whole-file hashes match; cannot prove DriftHash is used")
	}
	if render.DriftHash(a) != render.DriftHash(b) {
		t.Fatal("DriftHash changed when only generated-at changed")
	}
	rep, err := Cutover(Request{
		Roots:     []string{root},
		Commit:    false,
		Installer: &FakeInstaller{},
		Files:     []Replacement{{Path: live, Content: a}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(rep.Writes))
	}
	if rep.Writes[0].WouldDr != render.DriftHash(a) {
		t.Fatalf("WouldDr %s, want DriftHash %s", rep.Writes[0].WouldDr, render.DriftHash(a))
	}
	if rep.Writes[0].WouldDr == sha256Hex([]byte(a)) {
		t.Fatal("WouldDr is the whole-file hash; footer timestamp would look like drift")
	}
}

func TestCutoverRestoreRoundTripByHash(t *testing.T) {
	// Skipping restore, or leaving *.pre-substrate / generated files behind,
	// is the change that makes this red. The snapshot must be non-empty so a
	// cutover that touched nothing cannot pass as a perfect round trip.
	root := t.TempDir()
	originals := map[string]string{
		filepath.Join(".claude", "CLAUDE.md"):           "# original claude\n",
		filepath.Join(".codex", "AGENTS.md"):            "# original agents\n",
		filepath.Join(".cursor", "rules", "custom.mdc"): "# keep this cursor rule\n",
		filepath.Join(".memorix", "memories.json"):      `{"memories":["keep"]}` + "\n",
		filepath.Join("acme", "plotlens", "AGENTS.md"):  "# repo agents\n",
	}
	for rel, body := range originals {
		mustWriteMode(t, filepath.Join(root, rel), body, 0o640)
	}
	before := fileTriples(t, root)
	if len(before) == 0 {
		t.Fatal("pre-cutover snapshot was empty")
	}

	claude := filepath.Join(root, ".claude", "CLAUDE.md")
	agents := filepath.Join(root, ".codex", "AGENTS.md")
	repoAgents := filepath.Join(root, "acme", "plotlens", "AGENTS.md")
	newCursor := filepath.Join(root, ".cursor", "rules", "substrate.mdc")
	rendered := []Replacement{
		{Path: claude, Content: renderedClaude("2026-09-07T00:00:00Z")},
		{Path: agents, Content: renderedClaude("2026-09-07T00:00:00Z")},
		{Path: repoAgents, Content: renderedClaude("2026-09-07T00:00:00Z")},
		{Path: newCursor, Content: renderedClaude("2026-09-07T00:00:00Z")},
	}
	fake := &FakeInstaller{}
	if _, err := Cutover(Request{
		Roots:     []string{root},
		Home:      root,
		Commit:    true,
		Installer: fake,
		Files:     rendered,
		Server:    "https://cp.example",
		Machine:   "wsl",
		Binary:    "/usr/local/bin/substrate-adapter",
	}); err != nil {
		t.Fatal(err)
	}
	if len(fake.Installs) != 1 {
		t.Fatalf("Installs = %d, want 1", len(fake.Installs))
	}
	// Restoring unitRoots to an unconditional []string{DefaultMountRoot} is the
	// one-line change that makes this red: the daemon would then render over
	// /work, which this cutover never backed up.
	if got := fake.Installs[0].Roots; len(got) != 1 || got[0] != root {
		t.Fatalf("unit roots %v, want [%s]", fake.Installs[0].Roots, root)
	}

	if _, err := Restore(Request{Roots: []string{root}, Home: root, Commit: true, Installer: fake}); err != nil {
		t.Fatal(err)
	}
	after := fileTriples(t, root)
	if after != before {
		t.Fatalf("round trip mutated the tree\n before:\n%s\n after:\n%s", before, after)
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(d.Name(), BackupSuffix) {
			t.Errorf("left %s behind", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.Uninstalls != 1 {
		t.Fatalf("Uninstalls = %d, want 1", fake.Uninstalls)
	}
}

func TestCutoverDryRunSurfacesBackupConflict(t *testing.T) {
	// Succeeding on dry-run when commit would refuse an existing backup is
	// the change that makes this red.
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "CLAUDE.md")
	mustWrite(t, live, "original\n")
	mustWrite(t, live+BackupSuffix, "earlier\n")
	_, err := Cutover(Request{
		Roots:     []string{root},
		Commit:    false,
		Installer: &FakeInstaller{},
		Files:     []Replacement{{Path: live, Content: renderedClaude("2026-09-07T00:00:00Z")}},
	})
	if err == nil || !strings.Contains(err.Error(), BackupSuffix) {
		t.Fatalf("got %v, want dry-run to surface the backup conflict", err)
	}
}

func plannedSet(rep *Report) string {
	if rep == nil {
		return ""
	}
	var lines []string
	for _, n := range rep.Renames {
		lines = append(lines, "R "+filepath.Base(n.From)+" -> "+filepath.Base(n.To))
	}
	for _, w := range rep.Writes {
		lines = append(lines, "W "+filepath.Base(w.Path)+" "+w.WouldSHA)
	}
	if rep.Install {
		lines = append(lines, "INSTALL")
	}
	for _, p := range rep.Created {
		lines = append(lines, "C "+filepath.Base(p))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func fileTriples(t *testing.T, root string) string {
	t.Helper()
	return snapshotTree(t, root)
}
