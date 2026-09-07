package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// TestOfflineHourTwentyObservationsZeroDuplicates is SYNC-2: 20 observations
// queued during a simulated hour-long outage all arrive within two simulated
// minutes of reconnection, with the server seeing each client_id exactly once.
//
// Goes red if Drain deletes outbox rows before the server confirms (the first
// failing POST would drop the 20, and reconnection would deliver none).
func TestOfflineHourTwentyObservationsZeroDuplicates(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)

	clk := &fakeClock{t: time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)}
	srv := newBatchFake(t)
	srv.setFail(true)
	cfg := testConfig(home, state, srv.URL)
	cfg.Now = clk.now

	for i := 0; i < 20; i++ {
		if _, err := db.Enqueue(observationJSON(i), clk.now()); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
		clk.advance(3 * time.Minute) // 20 * 3m = 1h of offline capture
	}
	if elapsed := clk.now().Sub(time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)); elapsed != time.Hour {
		t.Fatalf("simulated offline window = %s, want 1h", elapsed)
	}

	if err := Drain(t.Context(), db, cfg); err == nil {
		t.Fatal("Drain succeeded while the server was failing; the outage was not simulated")
	}
	if n := outboxCount(t, db); n != 20 {
		t.Fatalf("outbox depth after failed drain = %d, want 20 (delete-before-confirm drops rows here)", n)
	}

	srv.setFail(false)
	clk.advance(2 * time.Minute)
	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("Drain after reconnect: %v", err)
	}

	got := srv.uniqueClientIDs()
	if len(got) != 20 {
		t.Fatalf("server received %d distinct client_ids, want 20; posts=%d duplicates_flagged=%d",
			len(got), srv.postCount(), srv.duplicateCount())
	}
	if srv.duplicateCount() != 0 {
		t.Fatalf("server flagged %d duplicate:true results; want 0 on first successful drain", srv.duplicateCount())
	}
	if n := srv.receivedCount(); n != 20 {
		t.Fatalf("server stored %d memories, want 20 (a regenerated client_id would inflate this on retry)", n)
	}
	if n := outboxCount(t, db); n != 0 {
		t.Fatalf("outbox still has %d rows after a confirmed drain", n)
	}
}

func TestEnqueueClientIDIsUUIDv7AndStableAcrossRetry(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	clk := &fakeClock{t: time.Unix(1000, 0).UTC()}
	srv := newBatchFake(t)
	srv.setFail(true)
	cfg := testConfig(home, state, srv.URL)
	cfg.Now = clk.now

	cid, err := db.Enqueue(observationJSON(0), clk.now())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	id, err := uuid.Parse(cid)
	if err != nil {
		t.Fatalf("client_id %q: %v", cid, err)
	}
	if id.Version() != 7 {
		t.Fatalf("client_id version = %d, want 7 (Gotcha 10)", id.Version())
	}

	if err := Drain(t.Context(), db, cfg); err == nil {
		t.Fatal("Drain succeeded against a failing server")
	}
	after := outboxClientID(t, db)
	if after != cid {
		t.Fatalf("client_id changed across retry %q → %q; regenerating on drain duplicates memories", cid, after)
	}

	srv.setFail(false)
	clk.advance(time.Second)
	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	seen := srv.uniqueClientIDs()
	if len(seen) != 1 || seen[cid] != 1 {
		t.Fatalf("server client_ids = %v, want exactly the original %s once", seen, cid)
	}
}

func TestDrainTreatsDuplicateTrueAsSuccess(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newBatchFake(t)
	cfg := testConfig(home, state, srv.URL)

	cid, err := db.Enqueue(observationJSON(1), time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("first Drain: %v", err)
	}
	if n := outboxCount(t, db); n != 0 {
		t.Fatalf("outbox depth %d after first confirm", n)
	}

	// Replay the same client_id as if the delete never happened (network blip
	// after 2xx). The server returns duplicate:true; that is success, not an error.
	if _, err := db.sql.Exec(`INSERT INTO outbox (client_id, payload, created_at, attempts, last_error, next_attempt_at)
		VALUES (?, ?, 1, 0, '', 0)`, cid, string(mustInjectClientID(t, observationJSON(1), cid))); err != nil {
		t.Fatal(err)
	}
	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("replay Drain: %v (duplicate:true must be treated as success)", err)
	}
	if n := outboxCount(t, db); n != 0 {
		t.Fatalf("replay left %d outbox rows; duplicate:true should delete", n)
	}
	if n := srv.receivedCount(); n != 1 {
		t.Fatalf("server stored %d memories after a duplicate replay, want 1", n)
	}
}

func TestDrainSendsAtMost50(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newBatchFake(t)
	cfg := testConfig(home, state, srv.URL)

	for i := 0; i < 51; i++ {
		if _, err := db.Enqueue(observationJSON(i), time.Unix(int64(i+1), 0).UTC()); err != nil {
			t.Fatal(err)
		}
	}
	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if n := srv.lastBatchSize(); n != 50 {
		t.Fatalf("first drain batch = %d, want 50 (EDD §7.2)", n)
	}
	if n := outboxCount(t, db); n != 1 {
		t.Fatalf("outbox after first drain = %d, want 1 leftover", n)
	}
	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("second Drain: %v", err)
	}
	if n := outboxCount(t, db); n != 0 {
		t.Fatalf("outbox after second drain = %d, want 0", n)
	}
	if n := srv.receivedCount(); n != 51 {
		t.Fatalf("server stored %d, want 51", n)
	}
}

func TestBackoffSkipsRowsUntilDueAndCapsAtTenMinutes(t *testing.T) {
	if d := backoff(1); d != time.Second {
		t.Fatalf("backoff(1) = %s, want 1s", d)
	}
	if d := backoff(2); d != 2*time.Second {
		t.Fatalf("backoff(2) = %s, want 2s", d)
	}
	if d := backoff(20); d != 10*time.Minute {
		t.Fatalf("backoff(20) = %s, want 10m cap (EDD §7.2)", d)
	}

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	clk := &fakeClock{t: time.Unix(1000, 0).UTC()}
	srv := newBatchFake(t)
	srv.setFail(true)
	cfg := testConfig(home, state, srv.URL)
	cfg.Now = clk.now

	if _, err := db.Enqueue(observationJSON(0), clk.now()); err != nil {
		t.Fatal(err)
	}
	if err := Drain(t.Context(), db, cfg); err == nil {
		t.Fatal("Drain succeeded against a failing server")
	}
	if n := srv.postCount(); n != 1 {
		t.Fatalf("posts after first fail = %d, want 1", n)
	}

	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("Drain while backing off should be a no-op, not an error: %v", err)
	}
	if n := srv.postCount(); n != 1 {
		t.Fatalf("Drain retried immediately (posts=%d); backoff must skip not-due rows", n)
	}

	clk.advance(time.Second)
	if err := Drain(t.Context(), db, cfg); err == nil {
		t.Fatal("Drain succeeded against a failing server after backoff elapsed")
	}
	if n := srv.postCount(); n != 2 {
		t.Fatalf("posts after backoff elapsed = %d, want 2", n)
	}
}

func TestHookWriteReturnsUnderTwoSecondsWhenServerHangs(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	block := make(chan struct{})
	srv := newBatchFake(t)
	srv.setHang(block)
	t.Cleanup(func() {
		select {
		case <-block:
		default:
			close(block)
		}
	})
	cfg := testConfig(home, state, srv.URL)

	start := time.Now()
	err := HookWrite(t.Context(), db, cfg, observationJSON(0))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("HookWrite: %v", err)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("HookWrite took %s; Gotcha 8 requires return in under 2s (1.5s server timeout)", elapsed)
	}
	if n := outboxCount(t, db); n != 1 {
		t.Fatalf("outbox depth = %d, want 1 (timeout must fall back to the outbox, not drop the write)", n)
	}
}

// TestDrainIsolatesPoisonRowWithoutStallingTheBatch is SYNC-2 for a mixed
// batch: 30 valid observations, one AWS-key body (400), then 19 more. Goes red
// if a single 4xx backs off the whole 50-item batch so items 32–50 never POST.
func TestDrainIsolatesPoisonRowWithoutStallingTheBatch(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	clk := &fakeClock{t: time.Unix(1000, 0).UTC()}
	srv := newBatchFake(t)
	srv.commitBeforePoison = true
	cfg := testConfig(home, state, srv.URL)
	cfg.Now = clk.now

	var poisonCID string
	for i := 0; i < 50; i++ {
		payload := observationJSON(i)
		if i == 30 {
			payload = poisonObservationJSON(i)
		}
		cid, err := db.Enqueue(payload, clk.now())
		if err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
		if i == 30 {
			poisonCID = cid
		}
	}

	if err := Drain(t.Context(), db, cfg); err != nil {
		if n := srv.receivedCount(); n != 49 {
			t.Fatalf("Drain: %v; server received %d memories, want 49 (30 before the poison + 19 after); a batch-wide 4xx backoff strands the tail", err, n)
		}
		t.Fatalf("Drain: %v (a poison 4xx must isolate, not fail the whole batch)", err)
	}

	got := srv.uniqueClientIDs()
	if _, ok := got[poisonCID]; ok {
		t.Fatalf("server stored the poison client_id %s", poisonCID)
	}
	if n := srv.receivedCount(); n != 49 {
		t.Fatalf("server received %d memories, want 49 (30 before the poison + 19 after); a batch-wide 4xx backoff would leave 19 stranded", n)
	}
	if n := outboxCount(t, db); n != 1 {
		t.Fatalf("outbox depth = %d, want 1 poison row still at the head (or dead-lettered only after consecutive 4xx)", n)
	}
	if cid := outboxClientID(t, db); cid != poisonCID {
		t.Fatalf("remaining outbox client_id = %s, want the poison row %s", cid, poisonCID)
	}
}

func TestDrainDeadLettersAfterConsecutive4xx(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	clk := &fakeClock{t: time.Unix(2000, 0).UTC()}
	srv := newBatchFake(t)
	cfg := testConfig(home, state, srv.URL)
	cfg.Now = clk.now

	good, err := db.Enqueue(observationJSON(0), clk.now())
	if err != nil {
		t.Fatal(err)
	}
	poison, err := db.Enqueue(poisonObservationJSON(1), clk.now())
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < MaxConsecutive4xx; i++ {
		if err := Drain(t.Context(), db, cfg); err != nil {
			t.Fatalf("Drain %d: %v", i, err)
		}
		clk.advance(backoff(i + 1))
	}

	if n := srv.receivedCount(); n != 1 {
		t.Fatalf("server received %d, want the one valid row", n)
	}
	if _, ok := srv.uniqueClientIDs()[good]; !ok {
		t.Fatalf("valid client_id %s never landed", good)
	}
	if n := outboxCount(t, db); n != 0 {
		t.Fatalf("outbox still has %d rows; poison %s should have been dead-lettered after %d consecutive 4xx", n, poison, MaxConsecutive4xx)
	}
	errText := deadLetterError(t, db, poison)
	if errText == "" {
		t.Fatalf("dead-letter row for %s missing last_error", poison)
	}
}

// TestDrain200SubsetKeepsUnconfirmedAndReplays them: a 200 whose body lists
// only the first 30 of 50 client_ids must not delete the other 20 (SYNC-2).
// Goes red if Drain calls deleteOutbox(ids) on any 200 instead of the receipted set.
func TestDrain200SubsetKeepsUnconfirmedAndReplays(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	clk := &fakeClock{t: time.Unix(3000, 0).UTC()}
	srv := newBatchFake(t)
	srv.setConfirmLimit(30)
	cfg := testConfig(home, state, srv.URL)
	cfg.Now = clk.now

	ids := make([]string, 50)
	for i := 0; i < 50; i++ {
		cid, err := db.Enqueue(observationJSON(i), clk.now())
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = cid
	}

	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if n := srv.receivedCount(); n != 30 {
		t.Fatalf("server received %d, want exactly the 30 listed in the 200 body", n)
	}
	if n := outboxCount(t, db); n != 20 {
		t.Fatalf("outbox depth = %d, want 20 unconfirmed rows; deleting all ids on any 200 drops them here", n)
	}
	remaining := outboxClientIDs(t, db)
	for _, cid := range ids[:30] {
		if remaining[cid] {
			t.Fatalf("receipted %s still in the outbox", cid)
		}
	}
	for _, cid := range ids[30:] {
		if !remaining[cid] {
			t.Fatalf("unconfirmed %s missing from the outbox", cid)
		}
	}

	srv.setConfirmLimit(0)
	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("replay Drain: %v", err)
	}
	if n := srv.receivedCount(); n != 50 {
		t.Fatalf("after replay server received %d, want 50", n)
	}
	if n := outboxCount(t, db); n != 0 {
		t.Fatalf("outbox still has %d rows after the remaining 20 landed", n)
	}
}

func TestDrainTreats201AsSuccess(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newBatchFake(t)
	srv.successStatus = http.StatusCreated
	cfg := testConfig(home, state, srv.URL)

	if _, err := db.Enqueue(observationJSON(0), time.Unix(1, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if err := Drain(t.Context(), db, cfg); err != nil {
		t.Fatalf("Drain: %v (2xx must confirm; treating only 200 as success leaves the row queued)", err)
	}
	if n := outboxCount(t, db); n != 0 {
		t.Fatalf("outbox depth = %d after a 201, want 0", n)
	}
	if n := srv.receivedCount(); n != 1 {
		t.Fatalf("server received %d, want 1", n)
	}
}

func TestHookAPITimeoutMatchesHookBudget(t *testing.T) {
	cfg := testConfig(t.TempDir(), filepath.Join(t.TempDir(), "x.sqlite"), "http://127.0.0.1:1")
	cfg.HTTPTimeout = 15 * time.Second
	cfg.HookTimeout = DefaultHookTimeout
	a := newHookAPI(cfg)
	if a.timeout != DefaultHookTimeout {
		t.Fatalf("hook API timeout = %s, want %s (HTTPTimeout leaked onto the hook path)", a.timeout, DefaultHookTimeout)
	}
	if a.client == nil || a.client.Timeout != DefaultHookTimeout {
		got := time.Duration(0)
		if a.client != nil {
			got = a.client.Timeout
		}
		t.Fatalf("hook HTTP client Timeout = %s, want %s so the client itself bounds Gotcha 8", got, DefaultHookTimeout)
	}
}

func TestEnqueueObservationTruncatesBodyTo500(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, "http://127.0.0.1:1")
	body := strings.Repeat("x", 600)
	cid, err := EnqueueObservation(db, cfg, Observation{
		Tool: "bash", Files: []string{"a.go"}, Status: 1, Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := db.sql.QueryRow(`SELECT payload FROM outbox WHERE client_id = ?`, cid).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Body string `json:"body"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Body) != 500 {
		t.Fatalf("body len = %d, want 500 (EDD §7.3)", len(got.Body))
	}
	if got.Kind != "observation" {
		t.Fatalf("kind = %q, want observation", got.Kind)
	}
}

func TestEnqueueObservationDoesNotSplitUTF8Rune(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, "http://127.0.0.1:1")
	prefix := strings.Repeat("x", 497)
	body := prefix + "🎉" // 497 + 4-byte rune = 501 bytes
	cid, err := EnqueueObservation(db, cfg, Observation{
		Tool: "bash", Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := db.sql.QueryRow(`SELECT payload FROM outbox WHERE client_id = ?`, cid).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got.Body) {
		t.Fatalf("truncated body is invalid UTF-8 (%q); body[:500] split a rune", got.Body)
	}
	if got.Body != prefix {
		t.Fatalf("body = %q (len %d), want the 497-byte ASCII prefix without a split rune", got.Body, len(got.Body))
	}
}

func observationJSON(i int) []byte {
	return []byte(fmt.Sprintf(`{"kind":"observation","title":"obs-%d","body":"body-%d","scope":"global:/org:acme/team:core","visibility":"team","verification":{"type":"agent_inference"},"source":{"machine":"test"}}`, i, i))
}

// AWS example access key: triggers the server's 400 secret scanner (and the
// fake's poison path) without being a live credential.
func poisonObservationJSON(i int) []byte {
	return []byte(fmt.Sprintf(`{"kind":"observation","title":"obs-%d","body":"AKIAIOSFODNN7EXAMPLE leaked-%d","scope":"global:/org:acme/team:core","visibility":"team","verification":{"type":"agent_inference"},"source":{"machine":"test"}}`, i, i))
}

func mustInjectClientID(t *testing.T, payload []byte, cid string) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatal(err)
	}
	m["client_id"] = cid
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func outboxCount(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRow(`SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func outboxClientID(t *testing.T, db *DB) string {
	t.Helper()
	var cid string
	if err := db.sql.QueryRow(`SELECT client_id FROM outbox`).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	return cid
}

func outboxClientIDs(t *testing.T, db *DB) map[string]bool {
	t.Helper()
	rows, err := db.sql.Query(`SELECT client_id FROM outbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var cid string
		if err := rows.Scan(&cid); err != nil {
			t.Fatal(err)
		}
		out[cid] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func deadLetterError(t *testing.T, db *DB, cid string) string {
	t.Helper()
	var lastErr string
	err := db.sql.QueryRow(`SELECT last_error FROM outbox_dead WHERE client_id = ?`, cid).Scan(&lastErr)
	if err != nil {
		return ""
	}
	return lastErr
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type batchFake struct {
	mu                 sync.Mutex
	fail               bool
	hang               <-chan struct{}
	received           []map[string]any
	seen               map[string]int
	posts              int
	dups               int
	lastSize           int
	cache              []map[string]any
	cacheCalls         []string
	confirmLimit       int
	successStatus      int
	commitBeforePoison bool
	URL                string
}

func newBatchFake(t *testing.T) *batchFake {
	t.Helper()
	f := &batchFake{seen: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/memory/batch", f.handleBatch)
	mux.HandleFunc("GET /v1/memory/cache", f.handleCache)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

func (f *batchFake) setFail(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = v
}

func (f *batchFake) setHang(ch <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hang = ch
}

func (f *batchFake) setCache(items []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache = items
}

func (f *batchFake) setConfirmLimit(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmLimit = n
}

func (f *batchFake) handleBatch(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	hang := f.hang
	f.mu.Unlock()
	if hang != nil {
		<-hang
	}
	f.mu.Lock()
	f.posts++
	fail := f.fail
	f.mu.Unlock()
	if fail {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastSize = len(items)

	poisonAt := -1
	for i, it := range items {
		body, _ := it["body"].(string)
		if strings.Contains(body, "AKIAIOSFODNN7EXAMPLE") {
			poisonAt = i
			break
		}
	}
	if poisonAt >= 0 {
		if f.commitBeforePoison {
			for _, it := range items[:poisonAt] {
				f.receiveLocked(it)
			}
		}
		http.Error(w, `{"error":"SUBSTRATE_SECRET_DETECTED"}`, http.StatusBadRequest)
		return
	}

	results := make([]map[string]any, 0, len(items))
	for i, it := range items {
		if f.confirmLimit > 0 && i >= f.confirmLimit {
			break
		}
		cid := f.receiveLocked(it)
		results = append(results, map[string]any{
			"client_id":  cid,
			"subject_id": "mem-" + cid,
			"duplicate":  f.seen[cid] > 1,
		})
	}
	status := http.StatusOK
	if f.successStatus != 0 {
		status = f.successStatus
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(results)
}

func (f *batchFake) receiveLocked(it map[string]any) string {
	cid, _ := it["client_id"].(string)
	dup := f.seen[cid] > 0
	f.seen[cid]++
	if dup {
		f.dups++
	} else {
		f.received = append(f.received, it)
	}
	return cid
}

func (f *batchFake) handleCache(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	hang := f.hang
	fail := f.fail
	items := append([]map[string]any(nil), f.cache...)
	f.cacheCalls = append(f.cacheCalls, r.URL.RawQuery)
	f.mu.Unlock()
	if hang != nil {
		<-hang
	}
	if fail {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"memories": items})
}

func (f *batchFake) uniqueClientIDs() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for cid, n := range f.seen {
		out[cid] = n
	}
	return out
}

func (f *batchFake) receivedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.received)
}

func (f *batchFake) duplicateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dups
}

func (f *batchFake) postCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.posts
}

func (f *batchFake) lastBatchSize() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSize
}

func (f *batchFake) lastCacheQuery() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cacheCalls) == 0 {
		return ""
	}
	return f.cacheCalls[len(f.cacheCalls)-1]
}

func (f *batchFake) cacheCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cacheCalls)
}
