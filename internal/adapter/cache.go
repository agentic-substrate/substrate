package adapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CachedMemory is one offline-cache row (EDD §7.1).
type CachedMemory struct {
	ID          string
	ScopePath   string
	Title       string
	Body        string
	Identifiers []string
	Status      string
	UpdatedAt   time.Time
}

const cacheSinceKey = "cache_since"

// RefreshCache pulls GET /v1/memory/cache for remotes present on this machine
// and upserts the FTS5 delta (EDD §7.2).
func RefreshCache(ctx context.Context, db *DB, cfg Config) error {
	return refreshCache(ctx, db, newAPI(cfg))
}

func refreshCache(ctx context.Context, db *DB, api *api) error {
	if db == nil {
		return fmt.Errorf("adapter: nil db")
	}
	repos, err := db.presentRepos()
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return db.pruneCache(ctx, repos)
	}
	since, err := db.kvTime(ctx, cacheSinceKey)
	if err != nil {
		return err
	}
	var maxUpdated time.Time
	for _, repo := range repos {
		items, err := api.getMemoryCache(ctx, []string{repo}, since)
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.Status != "confirmed" && it.Status != "probable" {
				continue
			}
			scopePath := it.ScopePath
			if scopePath == "" {
				scopePath = repo
			}
			row := CachedMemory{
				ID:          it.ID,
				ScopePath:   scopePath,
				Title:       it.Title,
				Body:        it.Body,
				Identifiers: it.Identifiers,
				Status:      it.Status,
				UpdatedAt:   it.UpdatedAt,
			}
			if err := db.upsertCacheContext(ctx, row); err != nil {
				return err
			}
			if it.UpdatedAt.After(maxUpdated) {
				maxUpdated = it.UpdatedAt
			}
		}
	}
	if err := db.pruneCache(ctx, repos); err != nil {
		return err
	}
	if maxUpdated.IsZero() {
		return nil
	}
	return db.setKV(ctx, cacheSinceKey, maxUpdated.UTC().Format(time.RFC3339))
}

func (db *DB) presentRepos() ([]string, error) {
	spaces, err := db.Workspaces()
	if err != nil {
		return nil, err
	}
	var repos []string
	seen := map[string]struct{}{}
	for _, ws := range spaces {
		if ws.Remote == "" {
			continue
		}
		if _, ok := seen[ws.Remote]; ok {
			continue
		}
		seen[ws.Remote] = struct{}{}
		repos = append(repos, ws.Remote)
	}
	return repos, nil
}

func (db *DB) upsertCache(m CachedMemory) error {
	return db.upsertCacheContext(context.Background(), m)
}

func (db *DB) upsertCacheContext(ctx context.Context, m CachedMemory) error {
	ids := strings.Join(m.Identifiers, " ")
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO memory_cache (id, scope_path, title, body, identifiers, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			scope_path = excluded.scope_path,
			title = excluded.title,
			body = excluded.body,
			identifiers = excluded.identifiers,
			status = excluded.status,
			updated_at = excluded.updated_at
	`, m.ID, m.ScopePath, m.Title, m.Body, ids, m.Status, m.UpdatedAt.Unix())
	if err != nil {
		return fmt.Errorf("adapter: cache upsert: %w", err)
	}
	return nil
}

func (db *DB) pruneCache(ctx context.Context, repos []string) error {
	rows, err := db.sql.QueryContext(ctx, `SELECT id, scope_path FROM memory_cache`)
	if err != nil {
		return fmt.Errorf("adapter: cache prune: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var drop []string
	for rows.Next() {
		var id, scopePath string
		if err := rows.Scan(&id, &scopePath); err != nil {
			return fmt.Errorf("adapter: cache prune: %w", err)
		}
		if scopePath == "" {
			continue
		}
		if !cacheInPresentChain(scopePath, repos) {
			drop = append(drop, id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("adapter: cache prune: %w", err)
	}
	for _, id := range drop {
		if _, err := db.sql.ExecContext(ctx, `DELETE FROM memory_cache WHERE id = ?`, id); err != nil {
			return fmt.Errorf("adapter: cache prune: %w", err)
		}
	}
	return nil
}

func cacheInPresentChain(scopePath string, repos []string) bool {
	for _, repo := range repos {
		if repo != "" && strings.Contains(scopePath, repo) {
			return true
		}
	}
	return false
}

// SearchCache runs an FTS5 query over the offline memory cache.
func (db *DB) SearchCache(query string) ([]CachedMemory, error) {
	return db.searchCache(context.Background(), query)
}

func (db *DB) searchCache(ctx context.Context, query string) ([]CachedMemory, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT id, scope_path, title, body, identifiers, status, updated_at
		FROM memory_cache
		WHERE rowid IN (
			SELECT rowid FROM memory_cache_fts WHERE memory_cache_fts MATCH ?
		)
	`, query)
	if err != nil {
		return nil, fmt.Errorf("adapter: cache search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CachedMemory
	for rows.Next() {
		var m CachedMemory
		var ids string
		var updated int64
		if err := rows.Scan(&m.ID, &m.ScopePath, &m.Title, &m.Body, &ids, &m.Status, &updated); err != nil {
			return nil, fmt.Errorf("adapter: cache scan: %w", err)
		}
		if ids != "" {
			m.Identifiers = strings.Fields(ids)
		}
		if updated > 0 {
			m.UpdatedAt = time.Unix(updated, 0).UTC()
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("adapter: cache rows: %w", err)
	}
	return out, nil
}

// HookSearch refreshes the cache with the 1.5s hook timeout, then searches
// locally. A hung server still returns the last FTS delta (Gotcha 8).
func HookSearch(ctx context.Context, db *DB, cfg Config, query string) ([]CachedMemory, error) {
	ctx, cancel := context.WithTimeout(ctx, HookDeadline)
	defer cancel()
	dctx, cancelRefresh := context.WithTimeout(ctx, cfg.hookTimeout())
	defer cancelRefresh()
	_ = refreshCache(dctx, db, newHookAPI(cfg))
	hits, err := db.searchCache(ctx, query)
	if err != nil && failOpen(err) {
		return nil, nil
	}
	return hits, err
}

func (db *DB) setKV(ctx context.Context, key, value string) error {
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO kv (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	if err != nil {
		return fmt.Errorf("adapter: kv: %w", err)
	}
	return nil
}

func (db *DB) kvTime(ctx context.Context, key string) (time.Time, error) {
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("adapter: kv: %w", err)
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("adapter: kv %s: %w", key, err)
	}
	return t, nil
}
