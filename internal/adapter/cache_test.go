package adapter

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCacheRefreshStoresFTSDeltaForPresentRepos(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	if err := db.upsertWorkspace(Workspace{Path: "/tmp/p/r", Remote: "github.com/acme/known", Branch: "main"}, 1); err != nil {
		t.Fatal(err)
	}

	srv := newBatchFake(t)
	srv.setCache([]map[string]any{
		{
			"id":          "11111111-1111-4111-8111-111111111111",
			"title":       "widget timeout",
			"body":        "the widget handler retries on 504",
			"identifiers": []string{"widget.go", "HandleWidget"},
			"status":      "confirmed",
			"scope_path":  "global:/org:acme/repo:github.com/acme/known",
			"updated_at":  time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		},
		{
			"id":          "22222222-2222-4222-8222-222222222222",
			"title":       "probable note",
			"body":        "maybe related to widgets",
			"identifiers": []string{},
			"status":      "probable",
			"scope_path":  "global:/org:acme/repo:github.com/acme/known",
			"updated_at":  time.Date(2026, 9, 7, 10, 1, 0, 0, time.UTC).Format(time.RFC3339),
		},
	})
	cfg := testConfig(home, state, srv.URL)

	if err := RefreshCache(t.Context(), db, cfg); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	values, err := url.ParseQuery(srv.lastCacheQuery())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(values.Get("repos"), "github.com/acme/known") {
		t.Fatalf("cache query %q did not include the repo present on this machine", srv.lastCacheQuery())
	}
	if _, ok := values["repos"]; !ok {
		t.Fatalf("cache query %q omitted repos=; an empty allow-list would leak every readable memory", srv.lastCacheQuery())
	}

	hits, err := db.SearchCache("widget")
	if err != nil {
		t.Fatalf("SearchCache: %v", err)
	}
	if len(hits) < 1 {
		t.Fatal("FTS cache missed 'widget'; a cache that skips the FTS insert would fail here")
	}
	found := false
	for _, h := range hits {
		if h.ID == "11111111-1111-4111-8111-111111111111" && h.Status == "confirmed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("hits = %+v, want the confirmed widget memory", hits)
	}
}

func TestRefreshCacheSkipsFetchWhenNoPresentRepos(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newBatchFake(t)
	srv.setCache([]map[string]any{
		{
			"id":         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			"title":      "other team secret",
			"body":       "must not land on this laptop",
			"status":     "confirmed",
			"updated_at": time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		},
	})
	cfg := testConfig(home, state, srv.URL)

	if err := RefreshCache(t.Context(), db, cfg); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	if q := srv.lastCacheQuery(); q != "" || srv.cacheCallCount() != 0 {
		t.Fatalf("GET /v1/memory/cache was issued (%q, calls=%d) with no present remotes; omitting repos= leaks every row the token can read", q, srv.cacheCallCount())
	}
	hits, err := db.SearchCache("secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("cached %d rows with no present remotes; want 0", len(hits))
	}
}

func TestRefreshCacheRejectsUnverifiedStatus(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	if err := db.upsertWorkspace(Workspace{Path: "/tmp/p/r", Remote: "github.com/acme/known", Branch: "main"}, 1); err != nil {
		t.Fatal(err)
	}
	srv := newBatchFake(t)
	srv.setCache([]map[string]any{
		{
			"id":         "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
			"title":      "unverified leak",
			"body":       "unverified gossip that must not be cached",
			"status":     "unverified",
			"scope_path": "global:/org:acme/repo:github.com/acme/known",
			"updated_at": time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		},
		{
			"id":         "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
			"title":      "confirmed fact",
			"body":       "safe to cache",
			"status":     "confirmed",
			"scope_path": "global:/org:acme/repo:github.com/acme/known",
			"updated_at": time.Date(2026, 9, 7, 10, 1, 0, 0, time.UTC).Format(time.RFC3339),
		},
	})
	cfg := testConfig(home, state, srv.URL)
	if err := RefreshCache(t.Context(), db, cfg); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	hits, err := db.SearchCache("gossip")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("upserted unverified status: %+v", hits)
	}
	hits, err = db.SearchCache("safe")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Status != "confirmed" {
		t.Fatalf("hits = %+v, want the confirmed row only", hits)
	}
}

func TestRefreshCacheDropsMemoriesForAbsentRemotes(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	if err := db.upsertWorkspace(Workspace{Path: "/tmp/p/keep", Remote: "github.com/acme/keep", Branch: "main"}, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.upsertWorkspace(Workspace{Path: "/tmp/p/gone", Remote: "github.com/acme/gone", Branch: "main"}, 1); err != nil {
		t.Fatal(err)
	}
	srv := newBatchFake(t)
	srv.setCache([]map[string]any{
		{
			"id":         "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
			"title":      "keep fact",
			"body":       "still on disk",
			"status":     "confirmed",
			"scope_path": "global:/org:acme/repo:github.com/acme/keep",
			"updated_at": time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		},
		{
			"id":         "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
			"title":      "gone fact",
			"body":       "deleted checkout memory",
			"status":     "confirmed",
			"scope_path": "global:/org:acme/repo:github.com/acme/gone",
			"updated_at": time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		},
	})
	cfg := testConfig(home, state, srv.URL)
	if err := RefreshCache(t.Context(), db, cfg); err != nil {
		t.Fatalf("first RefreshCache: %v", err)
	}
	if _, err := db.sql.Exec(`DELETE FROM workspace WHERE remote = ?`, "github.com/acme/gone"); err != nil {
		t.Fatal(err)
	}
	srv.setCache(nil)
	if err := RefreshCache(t.Context(), db, cfg); err != nil {
		t.Fatalf("second RefreshCache: %v", err)
	}
	hits, err := db.SearchCache("deleted")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("cache still has memories for a remote no longer on disk: %+v", hits)
	}
	hits, err = db.SearchCache("still")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("kept-remote memories = %+v, want 1", hits)
	}
}

func TestHookSearchFallsBackToCacheWhenServerHangs(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	if err := db.upsertCache(CachedMemory{
		ID: "33333333-3333-4333-8333-333333333333", Title: "cached fact",
		Body: "offline widget knowledge", Identifiers: []string{"cache.go"},
		Status: "confirmed", UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

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
	hits, err := HookSearch(t.Context(), db, cfg, "widget")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("HookSearch: %v", err)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("HookSearch took %s; Gotcha 8 requires return in under 2s", elapsed)
	}
	if len(hits) == 0 {
		t.Fatal("HookSearch returned no hits; timeout must fall back to the FTS cache")
	}
}
