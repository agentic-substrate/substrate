package adapter

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is written by the daemon goroutine and read by the test, so the
// handler's writes and the poll loop's reads must not race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// SyncResult is only worth populating if something reads it. Run discarded it
// entirely (`if _, err := Sync(...)`), so UnknownRemotes, UnscopedRemotes,
// UnproposedDrift and SkillsSkipped were all asserted by tests and thrown away
// by the shipped daemon -- the guard-one-layer-below-the-decision shape that
// produced #58, #62, #63 and the PostToolUse hook's dead -home.
//
// This asserts at the entrypoint that supplies the value, not only at the
// function that computes it. Changing Run back to `if _, err := Sync(...)`
// turns it red.
func TestRunReportsWhatSyncSkipped(t *testing.T) {
	var buf syncBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	srv := newFake(t)
	cfg := testConfig(home, state, srv.URL)
	cfg.Interval = time.Hour
	cfg.SkillsRepo = "" // the shipped default: skills entirely off

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(ctx, cfg)
	}()

	deadline := time.After(8 * time.Second)
	for {
		if strings.Contains(buf.String(), "not configured") {
			cancel()
			<-done
			return
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("Run never reported that skills were skipped; log was:\n%s", buf.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
