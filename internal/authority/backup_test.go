package authority

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupSnapshotPreservesMissingBindingsAndClearsSessions(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	auth := &Store{Dir: filepath.Join(dir, "private")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	b, err := auth.Register(repo, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.CreateSession(repo, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	var snapshot []byte
	err = auth.WithBackupSnapshot(func(data []byte, scope BackupScope) error {
		snapshot = data
		if len(scope.Bindings) != 1 || scope.Bindings[0] != b {
			t.Fatalf("binding lost: %+v", scope)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	clean, err := ResetBackupSessions(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var st state
	if err := json.Unmarshal(clean, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Sessions) != 0 || len(st.Bindings) != 1 || st.Bindings[0] != b {
		t.Fatalf("invalid restored authority: %+v", st)
	}
	if strings.Contains(string(clean), digest(token)) {
		t.Fatal("old credential survived")
	}
}

func TestBackupSnapshotHoldsAuthorityLockUntilSnapshotFinishes(t *testing.T) {
	auth := &Store{Dir: filepath.Join(t.TempDir(), "private")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	backupDone := make(chan error, 1)
	go func() {
		backupDone <- auth.WithBackupSnapshot(func(_ []byte, _ BackupScope) error { close(entered); <-release; return nil })
	}()
	<-entered
	mutationDone := make(chan error, 1)
	go func() { mutationDone <- auth.CreateSpace("Work") }()
	select {
	case err := <-mutationDone:
		close(release)
		t.Fatalf("authority changed during snapshot: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-backupDone; err != nil {
		t.Fatal(err)
	}
	if err := <-mutationDone; err != nil {
		t.Fatal(err)
	}
}

func TestBackupAuthorityRejectsForgedStructure(t *testing.T) {
	auth := &Store{Dir: filepath.Join(t.TempDir(), "private")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(auth.Dir, "authority.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*state){
		"owner":                  func(s *state) { s.OwnerID = "forged" },
		"duplicate space":        func(s *state) { s.Spaces = append(s.Spaces, s.Spaces[0]) },
		"missing personal space": func(s *state) { s.Spaces[0].Name = "Work" },
		"bad binding": func(s *state) {
			s.Bindings = []Binding{{SpaceID: s.Spaces[0].ID, RepositoryID: randomID(), Checkout: "relative", CommonDir: "relative", Inode: 1}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var changed state
			_ = json.Unmarshal(data, &changed)
			mutate(&changed)
			encoded, _ := json.Marshal(changed)
			if _, err := ValidateBackupSnapshot(encoded); err == nil {
				t.Fatal("invalid authority accepted")
			}
		})
	}
	duplicate := append([]byte(`{"version":1,`), data[1:]...)
	if _, err := ValidateBackupSnapshot(duplicate); err == nil {
		t.Fatal("duplicate key accepted")
	}
}
