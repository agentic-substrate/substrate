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
			"updated_at":  time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		},
		{
			"id":          "22222222-2222-4222-8222-222222222222",
			"title":       "probable note",
			"body":        "maybe related to widgets",
			"identifiers": []string{},
			"status":      "probable",
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
