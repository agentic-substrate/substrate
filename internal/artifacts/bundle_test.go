package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovalPinsExplicitDependencyBundle(t *testing.T) {
	f := setup(t)
	for path, content := range map[string]string{"SKILL.md": "Read references.md; script execution is separate.", "references.md": "approved reference", "run.sh": "echo approved"} {
		if err := os.WriteFile(filepath.Join(f.checkout, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, f.checkout, "add", ".")
	git(t, f.checkout, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "bundle")
	commit := git(t, f.checkout, "rev-parse", "HEAD")
	contribution := Contribution{OperationID: "bundle", Kind: "skill", Source: &Source{Commit: commit, Path: "SKILL.md", Files: []SourceFile{{Path: "references.md"}, {Path: "run.sh"}}}}
	receipt := capture(t, f.session, contribution)
	owner, choice := registered(t, f, receipt, "bundled", "build", false)
	approve(t, owner, receipt, "")
	if err := os.WriteFile(filepath.Join(f.checkout, "references.md"), []byte("unreviewed replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", ".")
	git(t, f.checkout, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "move source")
	d, err := f.session.Resolve(choice.Qualified)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Revision.Source.Files) != 2 || d.Revision.Source.Files[0].Content != "approved reference" || d.Revision.Source.Files[0].Blob == "" || d.Revision.Source.Commit != commit {
		t.Fatalf("approved dependency missing or replaced: %+v", d)
	}
	if retry := capture(t, f.session, contribution); retry != receipt {
		t.Fatal("bundle retry changed revision")
	}
	contribution.Source.Files = append(contribution.Source.Files, SourceFile{Path: "SKILL.md"})
	if _, err := f.session.Contribute(contribution); err == nil {
		t.Fatal("changed bundle retry accepted")
	}
}
func TestDependencyBundleRejectsUnsafeIncompleteAndUnboundedInputs(t *testing.T) {
	f := setup(t)
	r := gitCandidate(t, f, "source", "", "", "main")
	a, err := f.session.Inspect(r.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	commit := a.Revisions[0].Source.Commit
	cases := [][]SourceFile{{{Path: "../outside"}}, {{Path: "SKILL.md"}}, {{Path: "missing.md"}}, {{Path: "SKILL.md", Content: "caller bytes"}}}
	tooMany := make([]SourceFile, 33)
	for i := range tooMany {
		tooMany[i] = SourceFile{Path: "references.md"}
	}
	cases = append(cases, tooMany)
	for i, files := range cases {
		c := Contribution{OperationID: string(rune('a' + i)), Kind: "skill", Source: &Source{Commit: commit, Path: "SKILL.md", Files: files}}
		if _, err := f.session.Contribute(c); err == nil {
			t.Fatalf("invalid bundle %d accepted", i)
		}
	}
	if err := os.WriteFile(filepath.Join(f.checkout, "large.md"), []byte(strings.Repeat("a", 1024*1024)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(f.checkout, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.checkout, "bad.md"), []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", ".")
	git(t, f.checkout, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "unsafe dependencies")
	for _, path := range []string{"large.md", "link.md", "bad.md"} {
		c := Contribution{OperationID: path, Kind: "skill", Source: &Source{Commit: git(t, f.checkout, "rev-parse", "HEAD"), Path: "SKILL.md", Files: []SourceFile{{Path: path}}}}
		if _, err := f.session.Contribute(c); err == nil {
			t.Fatalf("unsafe dependency %s accepted", path)
		}
	}
}

func TestInvalidUTF8DependencyIdentityCannotBecomeNormalizedRetry(t *testing.T) {
	f := setup(t)
	if err := os.WriteFile(filepath.Join(f.checkout, "SKILL.md"), []byte("main"), 0600); err != nil {
		t.Fatal(err)
	}
	path := string([]byte{'r', 0xff})
	if err := os.WriteFile(filepath.Join(f.checkout, path), []byte("reference"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", ".")
	git(t, f.checkout, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "invalid filename")
	c := Contribution{OperationID: "bad-path", Kind: "skill", Source: &Source{Commit: git(t, f.checkout, "rev-parse", "HEAD"), Path: "SKILL.md", Files: []SourceFile{{Path: path}}}}
	if _, err := f.session.Contribute(c); err == nil {
		t.Fatal("invalid UTF-8 dependency identity acknowledged")
	}
}

func TestDependencyBundleRejectsRootDirectoryAlias(t *testing.T) {
	f := setup(t)
	receipt := gitCandidate(t, f, "root-file-only", "", "", "main document")
	a, err := f.session.Inspect(receipt.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	candidate := Contribution{OperationID: "dot-dependency", Kind: "skill", Source: &Source{Commit: a.Revisions[0].Source.Commit, Path: "SKILL.md", Files: []SourceFile{{Path: "."}}}}
	if r, err := f.session.Contribute(candidate); err == nil || r != (Receipt{}) {
		t.Fatalf("root directory alias captured as dependency: %+v %v", r, err)
	}
}
