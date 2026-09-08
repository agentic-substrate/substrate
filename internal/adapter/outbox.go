package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Observation is a PostToolUse capture (EDD §7.3): tool, files, exit status,
// and a body truncated to MaxObservationBytes.
type Observation struct {
	Tool   string   `json:"tool"`
	Files  []string `json:"files"`
	Status int      `json:"status"`
	Body   string   `json:"body"`
}

type outboxRow struct {
	ID        int64
	ClientID  string
	Payload   string
	CreatedAt int64
	Attempts  int
}

// Enqueue persists payload and assigns a UUIDv7 client_id at insert time.
// Drain never regenerates that id (Gotcha 10).
func (db *DB) Enqueue(payload []byte, now time.Time) (string, error) {
	return db.EnqueueContext(context.Background(), payload, now)
}

// EnqueueContext is Enqueue bound to ctx so a hook deadline can fail open
// instead of waiting on SQLite (Gotcha 8).
func (db *DB) EnqueueContext(ctx context.Context, payload []byte, now time.Time) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cid, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("adapter: client_id: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return "", fmt.Errorf("adapter: outbox payload: %w", err)
	}
	m["client_id"] = cid.String()
	body, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("adapter: outbox payload: %w", err)
	}
	if now.IsZero() {
		now = time.Now()
	}
	_, err = db.sql.ExecContext(ctx, `
		INSERT INTO outbox (client_id, payload, created_at, attempts, last_error, next_attempt_at)
		VALUES (?, ?, ?, 0, '', 0)
	`, cid.String(), string(body), now.Unix())
	if err != nil {
		return "", fmt.Errorf("adapter: outbox enqueue: %w", err)
	}
	return cid.String(), nil
}

// EnqueueObservation queues a compact PostToolUse observation (EDD §7.3).
func EnqueueObservation(db *DB, cfg Config, obs Observation) (string, error) {
	body := clipUTF8(obs.Body, MaxObservationBytes)
	title := obs.Tool
	if title == "" {
		title = "observation"
	}
	if obs.Status != 0 {
		title = fmt.Sprintf("%s (exit %d)", title, obs.Status)
	}
	sc, err := cfg.observationScope()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{
		"kind":         "observation",
		"title":        title,
		"body":         body,
		"identifiers":  obs.Files,
		"scope":        sc,
		"visibility":   "team",
		"verification": map[string]string{"type": "agent_inference"},
		"source":       map[string]string{"machine": cfg.Machine},
		"status":       "unverified",
	})
	if err != nil {
		return "", fmt.Errorf("adapter: observation: %w", err)
	}
	return db.Enqueue(payload, cfg.now())
}

func backoff(attempts int) time.Duration {
	if attempts < 1 {
		return 0
	}
	// 1s, 2s, 4s, … capped at 10 min. A large shift overflows to 0.
	if attempts > 10 {
		return MaxOutboxBackoff
	}
	d := time.Second << (attempts - 1)
	if d > MaxOutboxBackoff || d <= 0 {
		return MaxOutboxBackoff
	}
	return d
}

func clipUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func failOpen(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "busy") || strings.Contains(msg, "locked")
}

// Drain POSTs at most MaxOutboxBatch due rows to /v1/memory/batch. Rows are
// deleted only after a 2xx; duplicate:true is success (Gotcha 10). Drain does
// not sleep — not-due rows stay until next_attempt_at (Gotcha 8). A 4xx
// isolates remaining items one at a time so a poison row cannot pin the batch.
func Drain(ctx context.Context, db *DB, cfg Config) error {
	return drain(ctx, db, cfg, newAPI(cfg))
}

func drain(ctx context.Context, db *DB, cfg Config, api *api) error {
	if db == nil {
		return fmt.Errorf("adapter: nil db")
	}
	now := cfg.now()
	rows, err := db.dueOutbox(ctx, now, MaxOutboxBatch)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	items := make([]json.RawMessage, 0, len(rows))
	for _, r := range rows {
		items = append(items, json.RawMessage(r.Payload))
	}

	results, err := api.postMemoryBatch(ctx, items)
	receipted := receiptIDs(results)
	if len(receipted) > 0 {
		if delErr := db.deleteOutbox(ctx, receipted); delErr != nil {
			return delErr
		}
	}
	if err == nil {
		return nil
	}
	remaining := rowsWithout(rows, receipted)
	if isClientError(err) {
		return isolateDrain(ctx, db, api, remaining, now)
	}
	ids := make([]string, 0, len(remaining))
	for _, r := range remaining {
		ids = append(ids, r.ClientID)
	}
	if markErr := db.markOutboxFailure(ctx, ids, err.Error(), now); markErr != nil {
		return fmt.Errorf("adapter: drain: %w", errors.Join(err, markErr))
	}
	return err
}

func isolateDrain(ctx context.Context, db *DB, api *api, rows []outboxRow, now time.Time) error {
	var receipted []string
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		res, err := api.postMemoryBatch(ctx, []json.RawMessage{json.RawMessage(row.Payload)})
		if err != nil {
			if isClientError(err) {
				if noteErr := db.note4xx(ctx, row, err.Error(), now); noteErr != nil {
					return noteErr
				}
				continue
			}
			if markErr := db.markOutboxFailure(ctx, []string{row.ClientID}, err.Error(), now); markErr != nil {
				return markErr
			}
			continue
		}
		receipted = append(receipted, receiptIDs(res)...)
	}
	return db.deleteOutbox(ctx, receipted)
}

func receiptIDs(results []batchResult) []string {
	out := make([]string, 0, len(results))
	for _, res := range results {
		if res.ClientID != "" {
			out = append(out, res.ClientID)
		}
	}
	return out
}

func rowsWithout(rows []outboxRow, skip []string) []outboxRow {
	drop := map[string]struct{}{}
	for _, id := range skip {
		drop[id] = struct{}{}
	}
	var out []outboxRow
	for _, r := range rows {
		if _, ok := drop[r.ClientID]; ok {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (db *DB) dueOutbox(ctx context.Context, now time.Time, limit int) ([]outboxRow, error) {
	q, err := db.sql.QueryContext(ctx, `
		SELECT id, client_id, payload, created_at, attempts
		FROM outbox
		WHERE next_attempt_at <= ?
		ORDER BY id
		LIMIT ?
	`, now.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("adapter: outbox due: %w", err)
	}
	defer func() { _ = q.Close() }()
	var out []outboxRow
	for q.Next() {
		var r outboxRow
		if err := q.Scan(&r.ID, &r.ClientID, &r.Payload, &r.CreatedAt, &r.Attempts); err != nil {
			return nil, fmt.Errorf("adapter: outbox scan: %w", err)
		}
		out = append(out, r)
	}
	if err := q.Err(); err != nil {
		return nil, fmt.Errorf("adapter: outbox rows: %w", err)
	}
	return out, nil
}

func (db *DB) markOutboxFailure(ctx context.Context, ids []string, lastErr string, now time.Time) error {
	for _, id := range ids {
		var attempts int
		if err := db.sql.QueryRowContext(ctx, `SELECT attempts FROM outbox WHERE client_id = ?`, id).Scan(&attempts); err != nil {
			return fmt.Errorf("adapter: outbox attempts: %w", err)
		}
		attempts++
		next := now.Add(backoff(attempts)).Unix()
		if _, err := db.sql.ExecContext(ctx, `
			UPDATE outbox SET attempts = ?, consecutive_4xx = 0, last_error = ?, next_attempt_at = ?
			WHERE client_id = ?
		`, attempts, lastErr, next, id); err != nil {
			return fmt.Errorf("adapter: outbox backoff: %w", err)
		}
	}
	return nil
}

func (db *DB) note4xx(ctx context.Context, row outboxRow, lastErr string, now time.Time) error {
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT consecutive_4xx FROM outbox WHERE client_id = ?`, row.ClientID).Scan(&n)
	if err != nil {
		return fmt.Errorf("adapter: outbox 4xx: %w", err)
	}
	n++
	if n >= MaxConsecutive4xx {
		return db.deadLetter(ctx, row.ClientID, lastErr, now)
	}
	attempts := row.Attempts + 1
	next := now.Add(backoff(attempts)).Unix()
	if _, err := db.sql.ExecContext(ctx, `
		UPDATE outbox SET attempts = ?, consecutive_4xx = ?, last_error = ?, next_attempt_at = ?
		WHERE client_id = ?
	`, attempts, n, lastErr, next, row.ClientID); err != nil {
		return fmt.Errorf("adapter: outbox 4xx: %w", err)
	}
	return nil
}

func (db *DB) deadLetter(ctx context.Context, cid, lastErr string, now time.Time) error {
	if _, err := db.sql.ExecContext(ctx, `
		INSERT INTO outbox_dead (client_id, payload, created_at, attempts, last_error, dead_at)
		SELECT client_id, payload, created_at, attempts + 1, ?, ?
		FROM outbox WHERE client_id = ?
		ON CONFLICT(client_id) DO UPDATE SET
			last_error = excluded.last_error,
			attempts = excluded.attempts,
			dead_at = excluded.dead_at
	`, lastErr, now.Unix(), cid); err != nil {
		return fmt.Errorf("adapter: outbox dead-letter: %w", err)
	}
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM outbox WHERE client_id = ?`, cid); err != nil {
		return fmt.Errorf("adapter: outbox dead-letter delete: %w", err)
	}
	return nil
}

func (db *DB) deleteOutbox(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if _, err := db.sql.ExecContext(ctx, `DELETE FROM outbox WHERE client_id = ?`, id); err != nil {
			return fmt.Errorf("adapter: outbox delete: %w", err)
		}
	}
	return nil
}

// HookWrite enqueues first, then best-effort drains with the 1.5s hook
// timeout. A hung server leaves the row in the outbox (Gotcha 8). SQLite
// lock timeout fails open so the harness is not stalled.
func HookWrite(ctx context.Context, db *DB, cfg Config, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, HookDeadline)
	defer cancel()
	if _, err := db.EnqueueContext(ctx, payload, cfg.now()); err != nil {
		if failOpen(err) {
			return nil
		}
		return err
	}
	dctx, cancelDrain := context.WithTimeout(ctx, cfg.hookTimeout())
	defer cancelDrain()
	_ = drain(dctx, db, cfg, newHookAPI(cfg))
	return nil
}
