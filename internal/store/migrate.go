// Package store is the Postgres access layer: goose migrations, sqlc queries,
// and the pool the server pings for /readyz.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/agentic-substrate/substrate/migrations"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver for goose
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Migrate applies all pending goose migrations under a session-level
// advisory lock so a second replica cannot double-apply (EDD §8.1).
func Migrate(ctx context.Context, dsn string) error {
	return withProvider(ctx, dsn, func(p *goose.Provider) error {
		_, err := p.Up(ctx)
		return err
	})
}

func migrateDown(ctx context.Context, dsn string) error {
	return withProvider(ctx, dsn, func(p *goose.Provider) error {
		_, err := p.DownTo(ctx, 0)
		return err
	})
}

func withProvider(_ context.Context, dsn string, fn func(*goose.Provider) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations.SQL,
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	defer func() { _ = provider.Close() }()

	if err := fn(provider); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
