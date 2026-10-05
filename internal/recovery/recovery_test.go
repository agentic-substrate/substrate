package recovery

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
	"github.com/ncruces/go-sqlite3/driver"
)

type fixture struct {
	root, repo, token, backup string
	auth                      *authority.Store
	receipt                   artifacts.Receipt
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{root: t.TempDir()}
	f.repo, f.backup = filepath.Join(f.root, "repo"), filepath.Join(f.root, "backup")
	if err := os.Mkdir(f.repo, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", f.repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	f.auth = &authority.Store{Dir: filepath.Join(f.root, "private")}
	if err := f.auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.auth.Register(f.repo, "Personal"); err != nil {
		t.Fatal(err)
	}
	var err error
	f.token, err = f.auth.CreateSession(f.repo, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.Session(f.token, f.repo)
	if err != nil {
		t.Fatal(err)
	}
	f.receipt, err = session.Contribute(artifacts.Contribution{OperationID: "before", Kind: "memory", Content: "durable captured observation", Provenance: "synthetic fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return f
}

func rehash(t *testing.T, backup string) {
	t.Helper()
	path := filepath.Join(backup, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for i, entry := range m.Files {
		hashed, err := hashFile(filepath.Join(backup, entry.Name))
		if err != nil {
			t.Fatal(err)
		}
		m.Files[i] = hashed
	}
	data, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsInvalidBackupsAndCleansStages(t *testing.T) {
	mutations := map[string]func(*testing.T, *fixture){
		"incomplete": func(t *testing.T, f *fixture) {
			if err := os.Remove(filepath.Join(f.backup, "authority.json")); err != nil {
				t.Fatal(err)
			}
		},
		"extra": func(t *testing.T, f *fixture) {
			if err := os.WriteFile(filepath.Join(f.backup, "extra"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"checksum": func(t *testing.T, f *fixture) {
			if err := os.WriteFile(filepath.Join(f.backup, "authority.json"), []byte("corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"public file": func(t *testing.T, f *fixture) {
			if err := os.Chmod(filepath.Join(f.backup, "authority.json"), 0644); err != nil {
				t.Fatal(err)
			}
		},
		"public directory": func(t *testing.T, f *fixture) {
			if err := os.Chmod(f.backup, 0755); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, f *fixture) {
			p := filepath.Join(f.backup, "authority.json")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(f.auth.Dir, "authority.json"), p); err != nil {
				t.Fatal(err)
			}
		},
		"fifo": func(t *testing.T, f *fixture) {
			p := filepath.Join(f.backup, "authority.json")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(p, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"oversize authority": func(t *testing.T, f *fixture) {
			if err := os.Truncate(filepath.Join(f.backup, "authority.json"), authorityLimit+1); err != nil {
				t.Fatal(err)
			}
		},
		"oversize manifest": func(t *testing.T, f *fixture) {
			if err := os.Truncate(filepath.Join(f.backup, "manifest.json"), manifestLimit+1); err != nil {
				t.Fatal(err)
			}
		},
		"oversize database": func(t *testing.T, f *fixture) {
			if err := os.Truncate(filepath.Join(f.backup, "artifacts.db"), artifacts.MaxBackupDatabase+1); err != nil {
				t.Fatal(err)
			}
		},
		"duplicate manifest key": func(t *testing.T, f *fixture) {
			p := filepath.Join(f.backup, "manifest.json")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append([]byte(`{"format":1,`), data[1:]...), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"unsupported manifest field": func(t *testing.T, f *fixture) {
			p := filepath.Join(f.backup, "manifest.json")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append([]byte(`{"unsupported":true,`), data[1:]...), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"incompatible schema": func(t *testing.T, f *fixture) {
			p := filepath.Join(f.backup, "manifest.json")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			var m manifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			m.ArtifactSchema = 99
			data, err = json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"rehashed corrupt sqlite": func(t *testing.T, f *fixture) {
			if err := os.WriteFile(filepath.Join(f.backup, "artifacts.db"), []byte("not a database"), 0600); err != nil {
				t.Fatal(err)
			}
			rehash(t, f.backup)
		},
		"rehashed forged owner": func(t *testing.T, f *fixture) {
			db, err := driver.Open(filepath.Join(f.backup, "artifacts.db"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE artifacts SET owner_id='forged'"); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			rehash(t, f.backup)
		},
		"rehashed duplicate authority key": func(t *testing.T, f *fixture) {
			p := filepath.Join(f.backup, "authority.json")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append([]byte(`{"version":1,`), data[1:]...), 0600); err != nil {
				t.Fatal(err)
			}
			rehash(t, f.backup)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			if err := Backup(f.auth.Dir, f.backup); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(f.auth.Dir, "artifacts.db"))
			if err != nil {
				t.Fatal(err)
			}
			mutate(t, f)
			destination := filepath.Join(f.root, "restore")
			if err := Restore(f.backup, destination); err == nil {
				t.Fatal("invalid backup restored")
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed restore advertised: %v", err)
			}
			stages, err := filepath.Glob(filepath.Join(f.root, ".substrate-recovery-*"))
			if err != nil || len(stages) != 0 {
				t.Fatalf("failed stages remain: %v %v", stages, err)
			}
			after, err := os.ReadFile(filepath.Join(f.auth.Dir, "artifacts.db"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("current state damaged")
			}
		})
	}
}

func TestPromotionNeverReplacesNewlyCreatedDirectory(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "destination")
	stage, dest, err := newStage(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	if err := promoteStage(stage, dest); err == nil {
		t.Fatal("empty destination replaced during promotion race")
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("stage changed during failed promotion: %v", err)
	}
}

func TestPromotionReportsPublishedDestinationWhenParentSyncFails(t *testing.T) {
	parent := t.TempDir()
	stage, dest, err := newStage(filepath.Join(parent, "destination"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSynced(filepath.Join(stage, "captured"), []byte("complete state")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(parent, 0700); err != nil {
			t.Error(err)
		}
	})
	err = promoteStage(stage, dest)
	if err == nil || !strings.Contains(err.Error(), "was published, but syncing its parent directory failed") {
		t.Fatalf("publication outcome was not explicit: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "captured"))
	if err != nil || string(data) != "complete state" {
		t.Fatalf("published state was removed or changed: %q %v", data, err)
	}
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published stage remains at its old name: %v", err)
	}
}

func TestRecoveryRejectsGitPlacementAndSymlinkDirectory(t *testing.T) {
	f := setup(t)
	if err := Backup(f.auth.Dir, filepath.Join(f.repo, "backup")); err == nil {
		t.Fatal("backup placed inside Git")
	}
	if err := Backup(f.auth.Dir, f.backup); err != nil {
		t.Fatal(err)
	}
	if err := Restore(f.backup, filepath.Join(f.repo, "restored")); err == nil {
		t.Fatal("restore placed inside Git")
	}
	link := filepath.Join(f.root, "link")
	if err := os.Symlink(f.backup, link); err != nil {
		t.Fatal(err)
	}
	if err := Restore(link, filepath.Join(f.root, "restored")); err == nil {
		t.Fatal("symlink backup directory accepted")
	}
}

func TestRestoreCopiesTheOpenedBytesWhenPathIsReplaced(t *testing.T) {
	f := setup(t)
	if err := Backup(f.auth.Dir, f.backup); err != nil {
		t.Fatal(err)
	}
	dir, err := openDirectory(f.backup)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	entry, err := hashFile(filepath.Join(f.backup, "authority.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := openFileAt(dir, "authority.json", authorityLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	before, err := os.ReadFile(filepath.Join(f.backup, "authority.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(f.backup, "authority.json"), filepath.Join(f.backup, "old-authority")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.backup, "authority.json"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(f.root, "copied")
	if err := copyVerified(source, destination, entry); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("path replacement changed validated/captured bytes")
	}
}

func TestRestoreDoesNotPlaceNewStateInsideItsBackup(t *testing.T) {
	f := setup(t)
	if err := Backup(f.auth.Dir, f.backup); err != nil {
		t.Fatal(err)
	}
	if err := Restore(f.backup, filepath.Join(f.backup, "restored")); err == nil {
		t.Fatal("restore changed its fixed backup inventory")
	}
	names, err := os.ReadDir(f.backup)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("backup inventory changed: %+v", names)
	}
}

func TestRestoredBindingsDenyMissingAndSamePathReplacementCheckout(t *testing.T) {
	f := setup(t)
	if err := Backup(f.auth.Dir, f.backup); err != nil {
		t.Fatal(err)
	}
	oldGit, err := os.Open(filepath.Join(f.repo, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	defer oldGit.Close()
	if err := os.RemoveAll(f.repo); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(f.root, "restored")
	if err := Restore(f.backup, destination); err != nil {
		t.Fatal(err)
	}
	auth := &authority.Store{Dir: destination}
	if _, err := auth.CreateSession(f.repo, "Personal"); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("missing checkout authorized: %v", err)
	}
	if err := os.Mkdir(f.repo, 0700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", f.repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	if _, err := auth.CreateSession(f.repo, "Personal"); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("same-path replacement authorized: %v", err)
	}
}

func TestRecoverySnapshotAndFreshCredential(t *testing.T) {
	f := setup(t)
	if err := Backup(f.auth.Dir, f.backup); err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.Session(f.token, f.repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Contribute(artifacts.Contribution{OperationID: "after", Kind: "memory", Content: "post-snapshot observation"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(f.root, "restored?x=#")
	if err := Restore(f.backup, dest); err != nil {
		t.Fatal(err)
	}
	restored := &authority.Store{Dir: dest}
	if _, err := restored.Authenticate(f.token, f.repo); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("old credential accepted: %v", err)
	}
	token, err := restored.CreateSession(f.repo, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	store, err = artifacts.Open(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, err = store.Session(token, f.repo)
	if err != nil {
		t.Fatal(err)
	}
	list, err := session.Search(artifacts.SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Results) != 1 || list.Results[0].ArtifactID != f.receipt.ArtifactID {
		t.Fatalf("wrong snapshot state: %+v", list)
	}
	pending, err := session.Pending()
	if err != nil || len(pending) != 1 || pending[0].RevisionID != f.receipt.RevisionID {
		t.Fatalf("lost durable work: %+v %v", pending, err)
	}
}

func TestBackupRefusesRunningNodeAndExistingDestination(t *testing.T) {
	f := setup(t)
	lock, err := node.Lock(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	if err := Backup(f.auth.Dir, f.backup); !errors.Is(err, node.ErrRunning) {
		t.Fatalf("active node accepted: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.backup, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Backup(f.auth.Dir, f.backup); err == nil || !strings.Contains(err.Error(), "unused") {
		t.Fatalf("existing destination accepted: %v", err)
	}
}

func TestRestoreRefusesExistingStateWithoutChangingBytes(t *testing.T) {
	f := setup(t)
	if err := Backup(f.auth.Dir, f.backup); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(f.auth.Dir, "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Restore(f.backup, f.auth.Dir); err == nil || !strings.Contains(err.Error(), "unused") {
		t.Fatalf("existing state accepted: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(f.auth.Dir, "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("existing state changed")
	}
}
