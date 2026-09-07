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

func observationJSON(i int) []byte {
	return []byte(fmt.Sprintf(`{"kind":"observation","title":"obs-%d","body":"body-%d","scope":"global:/org:acme/team:core","visibility":"team","verification":{"type":"agent_inference"},"source":{"machine":"test"}}`, i, i))
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
	mu         sync.Mutex
	fail       bool
	hang       <-chan struct{}
	received   []map[string]any
	seen       map[string]int
	posts      int
	dups       int
	lastSize   int
	cache      []map[string]any
	cacheCalls []string
	URL        string
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
	results := make([]map[string]any, 0, len(items))
	for _, it := range items {
		cid, _ := it["client_id"].(string)
		dup := f.seen[cid] > 0
		f.seen[cid]++
		if dup {
			f.dups++
		} else {
			f.received = append(f.received, it)
		}
		results = append(results, map[string]any{
			"client_id":  cid,
			"subject_id": "mem-" + cid,
			"duplicate":  dup,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
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
