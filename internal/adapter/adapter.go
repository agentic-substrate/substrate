// Package adapter is the per-machine render loop (EDD §7.2, SYNC-1, SYNC-4, SYNC-6).
package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// DefaultInterval is the render tick from EDD §7.2.
const DefaultInterval = 5 * time.Minute

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
}

func (cfg Config) interval() time.Duration {
	if cfg.Interval > 0 {
		return cfg.Interval
	}
	return DefaultInterval
}

func (cfg Config) scope() string {
	if cfg.Scope != "" {
		return cfg.Scope
	}
	return "global:"
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

func (cfg Config) validate() error {
	if cfg.Home == "" {
		return fmt.Errorf("adapter: home is required")
	}
	if cfg.Server == "" {
		return fmt.Errorf("adapter: server URL is required")
	}
	return nil
}

// Run is the daemon: Sync on start, every Interval, and on SSE wake-up.
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

	wake := make(chan struct{}, 1)
	go listenSSE(ctx, cfg, wake)

	ticker := time.NewTicker(cfg.interval())
	defer ticker.Stop()
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
		}
	}
}
