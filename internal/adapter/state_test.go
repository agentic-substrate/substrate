package adapter

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenEnablesWAL(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "adapter.sqlite"))
	var mode string
	if err := db.sql.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q, want wal so a hook is not blocked 5s behind the daemon write lock (Gotcha 8)", mode)
	}
}

func TestOpenHookBusyTimeoutIsShort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adapter.sqlite")
	db, err := OpenHook(path)
	if err != nil {
		t.Fatalf("OpenHook: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var busy int
	if err := db.sql.QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if busy != int(HookBusyTimeout/time.Millisecond) {
		t.Fatalf("hook busy_timeout = %dms, want %dms (5000ms lets a hook blow Gotcha 8)", busy, HookBusyTimeout/time.Millisecond)
	}
}

func TestHookWriteFailsOpenWhenSQLiteIsLocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adapter.sqlite")
	daemon := openDB(t, path)
	hook, err := OpenHook(path)
	if err != nil {
		t.Fatalf("OpenHook: %v", err)
	}
	t.Cleanup(func() { _ = hook.Close() })

	tx, err := daemon.sql.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.Exec(`INSERT INTO kv (key, value) VALUES ('lock', 'held')`); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t.TempDir(), path, "http://127.0.0.1:1")
	start := time.Now()
	err = HookWrite(t.Context(), hook, cfg, observationJSON(0))
	elapsed := time.Since(start)
	if elapsed >= 2*time.Second {
		t.Fatalf("HookWrite took %s behind a write lock; Gotcha 8 requires fail-open in under 2s", elapsed)
	}
	if err != nil {
		t.Fatalf("HookWrite: %v (lock timeout must fail open, not block the harness)", err)
	}
}

func TestEnqueueContextHonorsCancel(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "adapter.sqlite"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := db.EnqueueContext(ctx, observationJSON(0), time.Unix(1, 0).UTC())
	if err == nil {
		t.Fatal("EnqueueContext succeeded with a canceled context; hook SQLite work ignored the deadline")
	}
}
