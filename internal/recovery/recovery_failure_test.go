package recovery

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRestoreWriteFailureCleansStageAndPreservesInputs(t *testing.T) {
	const childBackup = "SUBSTRATE_RESTORE_WRITE_FAILURE_BACKUP"
	const childDestination = "SUBSTRATE_RESTORE_WRITE_FAILURE_DESTINATION"
	if backup := os.Getenv(childBackup); backup != "" {
		signal.Ignore(syscall.SIGXFSZ)
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 4096, Max: 4096}); err != nil {
			t.Fatal(err)
		}
		if err := Restore(backup, os.Getenv(childDestination)); !errors.Is(err, syscall.EFBIG) {
			t.Fatalf("expected staged write failure: %v", err)
		}
		return
	}
	f := setup(t)
	if err := Backup(f.auth.Dir, f.backup); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, path := range []string{
		filepath.Join(f.auth.Dir, "authority.json"),
		filepath.Join(f.auth.Dir, "artifacts.db"),
		filepath.Join(f.backup, "authority.json"),
		filepath.Join(f.backup, "artifacts.db"),
		filepath.Join(f.backup, "manifest.json"),
	} {
		var err error
		before[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(f.root, "restored")
	command := exec.Command(os.Args[0], "-test.run=^TestRestoreWriteFailureCleansStageAndPreservesInputs$")
	command.Env = append(os.Environ(), childBackup+"="+f.backup, childDestination+"="+dest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("staged write fixture: %v %s", err, output)
	}
	if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed destination advertised: %v", err)
	}
	stages, err := filepath.Glob(filepath.Join(f.root, ".substrate-recovery-*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("failed stages remain: %v %v", stages, err)
	}
	for path, want := range before {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(want, got) {
			t.Fatalf("input changed after failed restore: %s %v", path, err)
		}
	}
}

func TestRestoreAbruptInterruptionLeavesOnlyPrivateUnpublishedStage(t *testing.T) {
	const childBackup = "SUBSTRATE_RESTORE_INTERRUPTED_BACKUP"
	const childDestination = "SUBSTRATE_RESTORE_INTERRUPTED_DESTINATION"
	if backup := os.Getenv(childBackup); backup != "" {
		if err := Restore(backup, os.Getenv(childDestination)); err != nil {
			t.Fatal(err)
		}
		return
	}
	f := setup(t)
	if err := Backup(f.auth.Dir, f.backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(f.backup, "artifacts.db"), 64*1024*1024); err != nil {
		t.Fatal(err)
	}
	rehash(t, f.backup)
	before := map[string]fileEntry{}
	for _, path := range []string{
		filepath.Join(f.auth.Dir, "authority.json"),
		filepath.Join(f.auth.Dir, "artifacts.db"),
		filepath.Join(f.backup, "authority.json"),
		filepath.Join(f.backup, "artifacts.db"),
	} {
		var err error
		before[path], err = hashFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	manifestBefore, err := os.ReadFile(filepath.Join(f.backup, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	watcher := os.NewFile(uintptr(fd), "recovery stage watcher")
	defer watcher.Close()
	if err := watcher.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.InotifyAddWatch(fd, f.root, unix.IN_CREATE|unix.IN_ONLYDIR); err != nil {
		t.Fatal(err)
	}
	type event struct {
		name string
		err  error
	}
	events := make(chan event, 1)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := watcher.Read(buffer)
			if err != nil {
				events <- event{err: err}
				return
			}
			for offset := 0; offset+unix.SizeofInotifyEvent <= n; {
				size := int(binary.NativeEndian.Uint32(buffer[offset+12 : offset+16]))
				end := offset + unix.SizeofInotifyEvent + size
				if end > n {
					events <- event{err: errors.New("incomplete staging event")}
					return
				}
				name := strings.TrimRight(string(buffer[offset+unix.SizeofInotifyEvent:end]), "\x00")
				if strings.HasPrefix(name, ".substrate-recovery-") {
					events <- event{name: name}
					return
				}
				offset = end
			}
		}
	}()
	dest := filepath.Join(f.root, "restored")
	command := exec.Command(os.Args[0], "-test.run=^TestRestoreAbruptInterruptionLeavesOnlyPrivateUnpublishedStage$")
	command.Env = append(os.Environ(), childBackup+"="+f.backup, childDestination+"="+dest)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer command.Process.Kill()
	var stage string
	select {
	case e := <-events:
		if e.err != nil {
			t.Fatal(e.err)
		}
		stage = filepath.Join(f.root, e.name)
	case err := <-done:
		t.Fatalf("restore completed before interruption: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("restore did not create a stage")
	}
	if err := command.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("interrupted recovery reported completion")
	}
	if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted destination advertised: %v", err)
	}
	info, err := os.Stat(stage)
	if err != nil || privateInfo(info, true) != nil {
		t.Fatalf("abandoned stage is not private: %v", err)
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || privateInfo(info, false) != nil {
			t.Fatalf("abandoned staged file is not private: %s %v", entry.Name(), err)
		}
	}
	for path, want := range before {
		got, err := hashFile(path)
		if err != nil || got != want {
			t.Fatalf("input changed after interruption: %s %v", path, err)
		}
	}
	manifestAfter, err := os.ReadFile(filepath.Join(f.backup, "manifest.json"))
	if err != nil || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Fatalf("backup manifest changed after interruption: %v", err)
	}
}
