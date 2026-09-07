package adapter

import (
	"context"
	"path/filepath"
	"testing"
)

type captureMetrics struct {
	machine string
	depth   int
	calls   int
	drift   int
}

func (c *captureMetrics) SetOutboxDepth(_ context.Context, machine string, depth int64) {
	c.machine = machine
	c.depth = int(depth)
	c.calls++
}

func (c *captureMetrics) RecordRenderDrift(_ context.Context, _, _ string) {
	c.drift++
}

// Goes red if ReportOutbox skips a zero depth (a live empty queue would then
// be indistinguishable from a machine that stopped reporting) or omits the
// machine label the alert interpolates.
func TestReportOutboxDepthIncludesMachineAndZero(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, "http://127.0.0.1:1")
	cfg.Machine = "wsl"
	got := &captureMetrics{}
	cfg.Metrics = got

	if err := ReportOutbox(t.Context(), db, cfg); err != nil {
		t.Fatalf("ReportOutbox empty: %v", err)
	}
	if got.calls != 1 {
		t.Fatalf("ReportOutbox calls = %d, want 1 (zero must still be exported)", got.calls)
	}
	if got.machine != "wsl" {
		t.Fatalf("machine = %q, want wsl", got.machine)
	}
	if got.depth != 0 {
		t.Fatalf("empty outbox depth = %d, want 0", got.depth)
	}

	if _, err := db.Enqueue([]byte(`{"title":"x","body":"y","kind":"observation","scope":"global:","visibility":"team","verification":{"type":"agent_inference"},"source":{"machine":"wsl"},"status":"unverified"}`), cfg.now()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := ReportOutbox(t.Context(), db, cfg); err != nil {
		t.Fatalf("ReportOutbox with row: %v", err)
	}
	if got.depth != 1 {
		t.Fatalf("outbox depth = %d, want 1", got.depth)
	}
	if got.machine != "wsl" {
		t.Fatalf("machine = %q, want wsl", got.machine)
	}
}
