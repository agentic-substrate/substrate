// Package adapter is the per-machine daemon: render, outbox drain, and
// offline memory cache (EDD §7.2, SYNC-1, SYNC-2, SYNC-4, SYNC-6).
package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Metrics is the adapter's observability sink. *observe.Runtime satisfies it.
// A machine that stopped reporting must not look like a machine reporting zero.
type Metrics interface {
	SetOutboxDepth(ctx context.Context, machine string, depth int64)
	RecordRenderDrift(ctx context.Context, machine string)
}

const (
	// DefaultInterval is the render tick from EDD §7.2.
	DefaultInterval = 5 * time.Minute
	// DefaultOutboxInterval is the outbox drain tick from EDD §7.2.
	DefaultOutboxInterval = 60 * time.Second
	// DefaultCacheInterval is the offline-cache refresh tick from EDD §7.2.
	DefaultCacheInterval = 15 * time.Minute
	// DefaultHookTimeout is the 1.5s server budget on the hook path (Gotcha 8).
	DefaultHookTimeout = 1500 * time.Millisecond
	// HookDeadline is the hard hook-process exit bound (Gotcha 8).
	HookDeadline = 2 * time.Second
	// MaxOutboxBatch is the POST /v1/memory/batch cap (EDD §7.2).
	MaxOutboxBatch = 50
	// MaxOutboxBackoff is the drain retry cap (EDD §7.2).
	MaxOutboxBackoff = 10 * time.Minute
	// MaxObservationBytes is the PostToolUse body cap (EDD §7.3).
	MaxObservationBytes = 500
	// MaxConsecutive4xx is how many isolated 4xx responses on the same
	// client_id dead-letter the row so a poison payload cannot pin the queue.
	MaxConsecutive4xx = 3
	// HookBusyTimeout is the SQLite busy_timeout on the hook path (Gotcha 8).
	HookBusyTimeout = 200 * time.Millisecond
	// DefaultBusyTimeout is the daemon SQLite busy_timeout.
	DefaultBusyTimeout = 5 * time.Second
)

// DefaultHTTPTimeout bounds GET /v1/render and POST /v1/review. It is not
// applied to the shared http.Client so SSE can stay open.
const DefaultHTTPTimeout = 15 * time.Second

// Config is the daemon's local wiring. Home and StatePath must be set; tests
// point both at t.TempDir() so the loop never touches the operator's files.
type Config struct {
	Server    string
	Token     string
	Machine   string
	Home      string
	StatePath string
	Roots     []string
	Interval  time.Duration
	Scope     string
	Client    *http.Client
	// HTTPTimeout bounds GET /v1/render and POST /v1/review. SSE is not
	// covered; that connection is long-lived.
	HTTPTimeout time.Duration
	// HookTimeout bounds hook-path server calls (Gotcha 8). Zero means
	// DefaultHookTimeout (1.5s). Render still uses HTTPTimeout.
	HookTimeout time.Duration
	// Now, if set, is the clock for outbox timestamps and backoff. Tests
	// inject a fake clock so an hour-long outage does not take an hour.
	Now func() time.Time
	// SkillsRepo is the git remote cloned into <home>/.substrate/skills.git.
	// Empty skips the skills loop so a machine without a mirror still renders.
	SkillsRepo string
	// Metrics reports outbox depth and drift. Nil disables export.
	Metrics Metrics
}

func (cfg Config) interval() time.Duration {
	if cfg.Interval > 0 {
		return cfg.Interval
	}
	return DefaultInterval
}

// observationScope is the scope an outbox observation is filed at. There is
// deliberately no default: `global:` was one, and internal/policy requires
// human_admin for any write at global or org, so every observation an agent
// enqueued was refused by the server and sat in the outbox retrying. A default
// that no agent token can ever use is not a default, it is a delayed failure.
//
// An empty scope is therefore an error at enqueue time, where the caller can
// still see it, rather than a 403 discovered later by the drain loop.
func (cfg Config) observationScope() (string, error) {
	if cfg.Scope == "" {
		return "", fmt.Errorf("adapter: observation scope is required (-scope); refusing to file at global:, which no agent token may write")
	}
	return cfg.Scope, nil
}

func (cfg Config) httpClient() *http.Client {
	if cfg.Client != nil {
		return cfg.Client
	}
	return &http.Client{}
}

func (cfg Config) httpTimeout() time.Duration {
	if cfg.HTTPTimeout > 0 {
		return cfg.HTTPTimeout
	}
	return DefaultHTTPTimeout
}

func (cfg Config) hookTimeout() time.Duration {
	if cfg.HookTimeout > 0 {
		return cfg.HookTimeout
	}
	return DefaultHookTimeout
}

func (cfg Config) now() time.Time {
	if cfg.Now != nil {
		return cfg.Now()
	}
	return time.Now()
}

func (cfg Config) validate() error {
	if cfg.Home == "" {
		return fmt.Errorf("adapter: home is required")
	}
	if cfg.Server == "" {
		return fmt.Errorf("adapter: server URL is required")
	}
	return nil
}

// ReportOutbox exports substrate_outbox_depth for this machine, including
// zero. Skipping zero would make a live empty queue look like a dead adapter.
func ReportOutbox(ctx context.Context, db *DB, cfg Config) error {
	if cfg.Metrics == nil || db == nil {
		return nil
	}
	n, err := db.OutboxDepth(ctx)
	if err != nil {
		return err
	}
	cfg.Metrics.SetOutboxDepth(ctx, cfg.Machine, int64(n))
	return nil
}

// OutboxDepth is the number of undrained outbox rows, including those not yet due.
func (db *DB) OutboxDepth(ctx context.Context) (int, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		return 0, fmt.Errorf("adapter: outbox depth: %w", err)
	}
	return n, nil
}

// Run is the daemon: Sync on start, every Interval, and on SSE wake-up.
// Outbox drains on start and every 60s; the cache refreshes on start and
// every 15 min. Drain/cache errors are logged, never fatal — a down server
// must not stop render (EDD §7.2).
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if cfg.StatePath == "" {
		return fmt.Errorf("adapter: state path is required")
	}
	db, err := Open(cfg.StatePath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if _, err := Sync(ctx, db, cfg); err != nil {
		return err
	}
	if err := Drain(ctx, db, cfg); err != nil {
		slog.Error("outbox drain failed", "err", err)
	}
	if err := ReportOutbox(ctx, db, cfg); err != nil {
		slog.Error("outbox depth metric failed", "err", err)
	}
	if err := RefreshCache(ctx, db, cfg); err != nil {
		slog.Error("cache refresh failed", "err", err)
	}

	wake := make(chan struct{}, 1)
	go listenSSE(ctx, cfg, wake)

	ticker := time.NewTicker(cfg.interval())
	defer ticker.Stop()
	outboxTick := time.NewTicker(DefaultOutboxInterval)
	defer outboxTick.Stop()
	cacheTick := time.NewTicker(DefaultCacheInterval)
	defer cacheTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := Sync(ctx, db, cfg); err != nil {
				slog.Error("render sync failed", "err", err)
			}
		case <-wake:
			if _, err := Sync(ctx, db, cfg); err != nil {
				slog.Error("render sync failed", "err", err)
			}
		case <-outboxTick.C:
			if err := Drain(ctx, db, cfg); err != nil {
				slog.Error("outbox drain failed", "err", err)
			}
			if err := ReportOutbox(ctx, db, cfg); err != nil {
				slog.Error("outbox depth metric failed", "err", err)
			}
		case <-cacheTick.C:
			if err := RefreshCache(ctx, db, cfg); err != nil {
				slog.Error("cache refresh failed", "err", err)
			}
		}
	}
}
