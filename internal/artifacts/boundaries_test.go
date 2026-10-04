package artifacts

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/agentic-substrate/substrate/internal/authority"
)

func TestInvalidUTF8CannotAliasRetryPayload(t *testing.T) {
	f := setup(t)
	for _, content := range []string{"original \xff observation", "original \xfe observation"} {
		r, err := f.session.Contribute(memory("invalid-retry", content))
		if !errors.Is(err, ErrInvalidText) || r != (Receipt{}) {
			t.Fatalf("invalid byte contribution acknowledged: %+v %v", r, err)
		}
	}
	valid := memory("valid-replacement", "original \ufffd observation")
	r := capture(t, f.session, valid)
	if retry := capture(t, f.session, valid); retry != r {
		t.Fatal("valid replacement character changed identity")
	}
	valid.Content = "original \xff observation"
	if _, err := f.session.Contribute(valid); !errors.Is(err, ErrInvalidText) {
		t.Fatalf("invalid bytes aliased supported replacement character: %v", err)
	}
	a, err := f.session.Inspect(r.ArtifactID)
	if err != nil || len(a.Revisions) != 1 || a.Revisions[0].Content != "original \ufffd observation" {
		t.Fatalf("valid text changed: %+v %v", a, err)
	}
}

func TestAllContributionStringsRejectInvalidUTF8(t *testing.T) {
	f := setup(t)
	for _, field := range []string{"operation", "artifact", "expected", "space", "repository", "kind", "provenance", "commit", "path", "blob"} {
		t.Run(field, func(t *testing.T) {
			c := memory("invalid-field", "valid content")
			switch field {
			case "operation":
				c.OperationID = "invalid-\xff"
			case "artifact":
				c.ArtifactID = "\xff"
			case "expected":
				c.ExpectedRevision = "\xff"
			case "space":
				c.SpaceID = "\xff"
			case "repository":
				c.RepositoryID = "\xff"
			case "kind":
				c.Kind = "\xff"
			case "provenance":
				c.Provenance = "\xff"
			default:
				c.Kind, c.Content = "skill", ""
				c.Source = &Source{Commit: strings.Repeat("0", 40), Path: "source.md"}
				if field == "commit" {
					c.Source.Commit = "\xff"
				}
				if field == "path" {
					c.Source.Path = "\xff"
				}
				if field == "blob" {
					c.Source.Blob = "\xff"
				}
			}
			if r, err := f.session.Contribute(c); !errors.Is(err, ErrInvalidText) || r != (Receipt{}) {
				t.Fatalf("invalid %s not rejected as text: %+v %v", field, r, err)
			}
		})
	}
	for _, args := range [][3]string{{"\xff", "", "retire"}, {"missing", "\xff", "retire"}, {"missing", "", "retire-\xff"}} {
		if _, err := f.session.Retire(args[0], args[1], args[2]); !errors.Is(err, ErrInvalidText) {
			t.Fatalf("invalid retirement text: %v", err)
		}
	}
	if _, err := f.session.Inspect("\xff"); !errors.Is(err, ErrInvalidText) {
		t.Fatalf("invalid lookup text: %v", err)
	}
}

func TestGitSourceTextRequiresUTF8(t *testing.T) {
	for _, scenario := range []string{"content", "path"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			name, content := "source.md", []byte("valid source\n")
			if scenario == "content" {
				content = []byte("invalid source \xff\n")
			} else {
				name = "source-\xff.md"
			}
			if err := os.WriteFile(filepath.Join(f.checkout, name), content, 0600); err != nil {
				t.Fatal(err)
			}
			git(t, f.checkout, "add", "--", name)
			git(t, f.checkout, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "synthetic text")
			commit := git(t, f.checkout, "rev-parse", "HEAD")
			if r, err := f.session.Contribute(Contribution{OperationID: "invalid-source", Kind: "skill", Source: &Source{Commit: commit, Path: name}}); !errors.Is(err, ErrInvalidText) || r != (Receipt{}) {
				t.Fatalf("invalid Git %s persisted: %+v %v", scenario, r, err)
			}
		})
	}
}

func TestGitSourceRejectsDotPath(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d-regular-files", count), func(t *testing.T) {
			f := setup(t)
			for i := range count {
				if err := os.WriteFile(filepath.Join(f.checkout, fmt.Sprintf("source%d.md", i)), []byte("synthetic source\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			git(t, f.checkout, "add", ".")
			git(t, f.checkout, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "synthetic regular files")
			commit := git(t, f.checkout, "rev-parse", "HEAD")
			if r, err := f.session.Contribute(Contribution{OperationID: "dot-source", Kind: "skill", Source: &Source{Commit: commit, Path: "."}}); err == nil || r != (Receipt{}) {
				t.Fatalf("dot path acknowledged with false source provenance: %+v %v", r, err)
			}
			pending, err := f.session.Pending()
			if err != nil || len(pending) != 0 {
				t.Fatalf("rejected dot path persisted pending work: %+v %v", pending, err)
			}
		})
	}
}

func TestGitSourceRequiresOneExactLiteralFile(t *testing.T) {
	f := setup(t)
	files := map[string]string{
		"one.md": "one source\n", "two.md": "two source\n",
		"dir/item.md": "ordinary nested source\n", "dir/*.md": "literal wildcard source\n",
		"tab\tname.md": "tab filename source\n", "trailing.md ": "trailing space filename source\n",
	}
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(f.checkout, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.checkout, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, f.checkout, "add", ".")
	git(t, f.checkout, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "synthetic literal paths")
	commit := git(t, f.checkout, "rev-parse", "HEAD")
	for i, name := range []string{"dir", "*.md", "dir/i*.md"} {
		t.Run(name, func(t *testing.T) {
			r, err := f.session.Contribute(Contribution{OperationID: fmt.Sprintf("reject-%d", i), Kind: "skill", Source: &Source{Commit: commit, Path: name}})
			if err == nil || r != (Receipt{}) {
				t.Fatalf("non-file or expanding path acknowledged: %+v %v", r, err)
			}
		})
	}
	operation := 0
	for name, content := range files {
		operation++
		t.Run("literal-"+name, func(t *testing.T) {
			r := capture(t, f.session, Contribution{OperationID: fmt.Sprintf("literal-%d", operation), Kind: "skill", Source: &Source{Commit: commit, Path: name}})
			a, err := f.session.Inspect(r.ArtifactID)
			if err != nil || len(a.Revisions) != 1 || a.Revisions[0].Source == nil || a.Revisions[0].Source.Path != name || a.Revisions[0].Content != content {
				t.Fatalf("literal path provenance changed: %+v %v", a, err)
			}
			blob, err := sourceGit(f.checkout, "rev-parse", commit+":"+name)
			if err != nil || a.Revisions[0].Source.Blob != strings.TrimSpace(string(blob)) {
				t.Fatalf("literal blob provenance changed: %+v %v", a, err)
			}
		})
	}
}

func TestLiveJournalPrivateAndIndependentOpenWaits(t *testing.T) {
	f := setup(t)
	tx, err := f.store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO artifacts VALUES('live','owner','space','repo','memory','active','')"); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(f.auth.Dir, "artifacts.db-journal")
	info, err := os.Stat(journal)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("live rollback journal must be private: %v %v", info, err)
	}
	cmd := storageChild(t, "open", f.auth.Dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "opening\n" {
		t.Fatalf("open child not ready: %q %v", line, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("independent Open did not wait for transaction: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("independent Open rejected active journal: %v", err)
	}
}

func TestHotJournalRecoveryPreservesAcknowledgedRevision(t *testing.T) {
	f := setup(t)
	r := capture(t, f.session, memory("before-crash", "acknowledged before interrupted write"))
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := storageChild(t, "crash", f.auth.Dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crash fixture: %v %s", err, out)
	}
	journal := filepath.Join(f.auth.Dir, "artifacts.db-journal")
	info, err := os.Stat(journal)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("hot journal must be private: %v %v", info, err)
	}
	header, err := os.ReadFile(journal)
	if err != nil || len(header) < 8 || !bytes.Equal(header[:8], []byte{0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7}) {
		t.Fatalf("fixture did not leave a hot journal: %v", err)
	}
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatalf("hot journal blocked recovery: %v", err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.session.Inspect(r.ArtifactID)
	if err != nil || len(a.Revisions) != 1 || a.Revisions[0].Content != "acknowledged before interrupted write" {
		t.Fatalf("acknowledged state lost: %+v %v", a, err)
	}
	var count int
	if err := f.store.db.QueryRow("SELECT count(*) FROM artifacts WHERE id LIKE 'interrupted-%'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("uncommitted state survived: %d %v", count, err)
	}
}

func storageChild(t *testing.T, mode, dir string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStorageChild$")
	cmd.Env = append(os.Environ(), "SUBSTRATE_STORAGE_TEST="+mode, "SUBSTRATE_STORAGE_DIR="+dir)
	return cmd
}

func TestStorageChild(t *testing.T) {
	mode := os.Getenv("SUBSTRATE_STORAGE_TEST")
	if mode == "" {
		return
	}
	syscall.Umask(022)
	if mode == "open" {
		fmt.Println("opening")
	}
	store, err := Open(&authority.Store{Dir: os.Getenv("SUBSTRATE_STORAGE_DIR")})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "open" {
		store.Close()
		return
	}
	if _, err := store.db.Exec("PRAGMA cache_size=10; PRAGMA cache_spill=ON"); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 128 {
		id := fmt.Sprintf("interrupted-%d", i)
		if _, err := tx.Exec("INSERT INTO artifacts VALUES(?,?,?,?,?,?,?)", id, "owner", "space", "repo", "memory", "active", strings.Repeat("x", 4096)); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(0)
}

func TestMissingPromisorObjectsCannotInvokeRepositoryHelper(t *testing.T) {
	f := setup(t)
	file := filepath.Join(f.checkout, "source.md")
	if err := os.WriteFile(file, []byte("synthetic source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", "source.md")
	git(t, f.checkout, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "synthetic source")
	commit := git(t, f.checkout, "rev-parse", "HEAD")
	blob := git(t, f.checkout, "rev-parse", "HEAD:source.md")
	marker := filepath.Join(filepath.Dir(f.checkout), "helper-called")
	helper := filepath.Join(filepath.Dir(f.checkout), "uploadpack")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\n: > '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "config", "remote.origin.url", f.checkout)
	git(t, f.checkout, "config", "remote.origin.promisor", "true")
	git(t, f.checkout, "config", "remote.origin.partialclonefilter", "blob:none")
	git(t, f.checkout, "config", "remote.origin.uploadpack", helper)
	git(t, f.checkout, "config", "protocol.file.allow", "always")
	if err := os.Remove(filepath.Join(f.checkout, ".git", "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}
	if r, err := f.session.Contribute(Contribution{OperationID: "missing-object", Kind: "skill", Source: &Source{Commit: commit, Path: "source.md"}}); err == nil || r != (Receipt{}) {
		t.Fatalf("missing source acknowledged: %+v %v", r, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source lookup executed repository remote helper: %v", err)
	}
	remoteHelper := filepath.Join(filepath.Dir(f.checkout), "git-remote-substrate-marker")
	if err := os.WriteFile(remoteHelper, []byte("#!/bin/sh\n: > '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(f.checkout)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ALLOW_PROTOCOL", "file:substrate-marker:http")
	git(t, f.checkout, "config", "remote.origin.url", "substrate-marker::synthetic")
	git(t, f.checkout, "config", "protocol.substrate-marker.allow", "always")
	if _, err := f.session.Contribute(Contribution{OperationID: "missing-external-helper", Kind: "skill", Source: &Source{Commit: commit, Path: "source.md"}}); err == nil {
		t.Fatal("missing helper source accepted")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source lookup executed external remote helper: %v", err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNotFound) }))
	defer server.Close()
	git(t, f.checkout, "config", "remote.origin.url", server.URL)
	git(t, f.checkout, "config", "protocol.http.allow", "always")
	if _, err := f.session.Contribute(Contribution{OperationID: "missing-network", Kind: "skill", Source: &Source{Commit: commit, Path: "source.md"}}); err == nil {
		t.Fatal("missing network source accepted")
	}
	if requests.Load() != 0 {
		t.Fatalf("source lookup made %d network requests", requests.Load())
	}
}
