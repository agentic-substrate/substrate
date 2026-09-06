package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/policy"
	"github.com/agentic-substrate/substrate/internal/scope"
)

// ErrNoPrincipal is returned when Tx runs without identity.FromContext.
var ErrNoPrincipal = errors.New("store: no principal on context")

// Store is a migrated Postgres pool. /readyz pings it and nothing else.
// The pool operates as substrate_app so REVOKEs on DELETE and on audit bind.
type Store struct {
	pool *pgxpool.Pool
}

// Open applies goose migrations under an advisory lock as the bootstrap DSN,
// then opens a pool that assumes substrate_app on connect and on every acquire.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if err := Migrate(ctx, dsn); err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pool config: %w", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return assumeAppRole(ctx, conn)
	}
	cfg.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		if err := assumeAppRole(ctx, conn); err != nil {
			return false, err
		}
		return true, nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// assumeAppRole makes the connection operate as substrate_app. Superusers keep
// their privileges across SET ROLE, so they need SET SESSION AUTHORIZATION.
func assumeAppRole(ctx context.Context, conn *pgx.Conn) error {
	var role string
	if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return fmt.Errorf("current_user: %w", err)
	}
	if role == "substrate_app" {
		return nil
	}
	var super bool
	if err := conn.QueryRow(ctx, `SELECT current_setting('is_superuser') = 'on'`).Scan(&super); err != nil {
		return fmt.Errorf("is_superuser: %w", err)
	}
	if super {
		if _, err := conn.Exec(ctx, `SET SESSION AUTHORIZATION substrate_app`); err != nil {
			return fmt.Errorf("session authorization substrate_app: %w", err)
		}
		return nil
	}
	if _, err := conn.Exec(ctx, `SET ROLE substrate_app`); err != nil {
		return fmt.Errorf("set role substrate_app: %w", err)
	}
	return nil
}

// Pool is the request-scoped pgx pool. It operates as substrate_app.
func (s *Store) Pool() *pgxpool.Pool {
	if s == nil {
		return nil
	}
	return s.pool
}

// Tx runs fn in one transaction with RLS session settings applied from ctx
// (EDD §8.2). Identity is loaded by middleware; this only SET LOCALs.
func (s *Store) Tx(ctx context.Context, fn func(pgx.Tx) error) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store not configured")
	}
	p := identity.FromContext(ctx)
	if p == nil {
		return ErrNoPrincipal
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := identity.ApplySession(ctx, tx, p); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
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

// TxChecked is the request-path gate: policy.Check first so denials carry a
// machine-readable code, then Tx as the RLS backstop (EDD §4.3).
func (s *Store) TxChecked(ctx context.Context, action string, sc scope.Path, fn func(pgx.Tx) error) error {
	p := identity.FromContext(ctx)
	if p == nil {
		return ErrNoPrincipal
	}
	if err := policy.Check(action, sc, *p); err != nil {
		return err
	}
	return s.Tx(ctx, fn)
}
