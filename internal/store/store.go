package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is a migrated Postgres pool. /readyz pings it and nothing else.
type Store struct {
	pool *pgxpool.Pool
}

// Open applies goose migrations under an advisory lock, then pings the pool.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if err := Migrate(ctx, dsn); err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// Ping reports whether Postgres is reachable.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store not configured")
	}
	return s.pool.Ping(ctx)
}
