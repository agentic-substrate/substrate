package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	_, err = db.sql.Exec(`
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
	body := obs.Body
	if len(body) > MaxObservationBytes {
		body = body[:MaxObservationBytes]
	}
	title := obs.Tool
	if title == "" {
		title = "observation"
	}
	if obs.Status != 0 {
		title = fmt.Sprintf("%s (exit %d)", title, obs.Status)
	}
	payload, err := json.Marshal(map[string]any{
		"kind":         "observation",
		"title":        title,
		"body":         body,
		"identifiers":  obs.Files,
		"scope":        cfg.scope(),
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

// Drain POSTs at most MaxOutboxBatch due rows to /v1/memory/batch. Rows are
// deleted only after a 2xx; duplicate:true is success (Gotcha 10). Drain does
// not sleep — not-due rows stay until next_attempt_at (Gotcha 8).
func Drain(ctx context.Context, db *DB, cfg Config) error {
	if db == nil {
		return fmt.Errorf("adapter: nil db")
	}
	now := cfg.now()
	rows, err := db.dueOutbox(now, MaxOutboxBatch)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	items := make([]json.RawMessage, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		items = append(items, json.RawMessage(r.Payload))
		ids = append(ids, r.ClientID)
	}

	results, err := newAPI(cfg).postMemoryBatch(ctx, items)
	if err != nil {
		if markErr := db.markOutboxFailure(ids, err.Error(), now); markErr != nil {
			return fmt.Errorf("adapter: drain: %w", errors.Join(err, markErr))
		}
		return err
	}

	confirmed := make([]string, 0, len(results))
	for _, res := range results {
		if res.ClientID == "" {
			continue
		}
		confirmed = append(confirmed, res.ClientID)
	}
	return db.deleteOutbox(confirmed)
}

func (db *DB) dueOutbox(now time.Time, limit int) ([]outboxRow, error) {
	q, err := db.sql.Query(`
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

func (db *DB) markOutboxFailure(ids []string, lastErr string, now time.Time) error {
	for _, id := range ids {
		var attempts int
		if err := db.sql.QueryRow(`SELECT attempts FROM outbox WHERE client_id = ?`, id).Scan(&attempts); err != nil {
			return fmt.Errorf("adapter: outbox attempts: %w", err)
		}
		attempts++
		next := now.Add(backoff(attempts)).Unix()
		if _, err := db.sql.Exec(`
			UPDATE outbox SET attempts = ?, last_error = ?, next_attempt_at = ?
			WHERE client_id = ?
		`, attempts, lastErr, next, id); err != nil {
			return fmt.Errorf("adapter: outbox backoff: %w", err)
		}
	}
	return nil
}

func (db *DB) deleteOutbox(ids []string) error {
	for _, id := range ids {
		if _, err := db.sql.Exec(`DELETE FROM outbox WHERE client_id = ?`, id); err != nil {
			return fmt.Errorf("adapter: outbox delete: %w", err)
		}
	}
	return nil
}

// HookWrite enqueues first, then best-effort drains with the 1.5s hook
// timeout. A hung server leaves the row in the outbox (Gotcha 8).
func HookWrite(ctx context.Context, db *DB, cfg Config, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, HookDeadline)
	defer cancel()
	if _, err := db.Enqueue(payload, cfg.now()); err != nil {
		return err
	}
	dctx, cancelDrain := context.WithTimeout(ctx, cfg.hookTimeout())
	defer cancelDrain()
	_ = Drain(dctx, db, cfg)
	return nil
}
