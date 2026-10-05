package authority

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOwnerContextsHoldAuthorityUntilActionCompletes(t *testing.T) {
	store, root, _ := fixture(t)
	if _, err := store.Register(root, "Personal"); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.WithOwnerContexts([]string{root}, func(contexts []Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	defer close(release)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("owner callback did not start")
	}
	lock, err := os.OpenFile(filepath.Join(store.Dir, ".lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		t.Fatalf("callback released authority lock: %v", err)
	}
	started, mutated := make(chan struct{}), make(chan error, 1)
	other := &Store{Dir: store.Dir}
	go func() {
		close(started)
		mutated <- other.RevokeSession(token)
	}()
	<-started
	select {
	case err := <-mutated:
		t.Fatalf("authority mutation escaped held callback: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}
	for _, result := range []<-chan error{done, mutated} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("authority operation did not finish after callback")
		}
	}
	if _, err := store.Authenticate(token, root); !errors.Is(err, ErrDenied) {
		t.Fatalf("released mutation did not revoke session: %v", err)
	}
}
