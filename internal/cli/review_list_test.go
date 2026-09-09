package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// formatReviewList must name every side's hostname, the slot, the bodies and
// the id, because the listing is the only place a reviewer sees them before
// deciding (SYNC-5).
// Mutation that turns this red: dropping the hostname label from writePreview.
func TestReviewListShowsHostnameSlotAndBodies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/review" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer lead" {
			t.Errorf("auth %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{
			"id":"0199a000-0000-7000-8000-000000000001",
			"kind":"import_conflict",
			"status":"open",
			"team_id":"0199a000-0000-7000-8000-0000000000aa",
			"payload":{
				"slot":".claude/CLAUDE.md#Indent",
				"hostname":"wsl",
				"hostnames":["mac","wsl"],
				"pair":[
					{"hash":"aaa","hostnames":["wsl"],"body":"Prefer tabs."},
					{"hash":"bbb","hostnames":["mac"],"body":"Prefer spaces."}
				]
			}
		}]}`))
	}))
	t.Cleanup(srv.Close)

	out, _, err := run(t, Deps{HTTP: srv.Client()}, "review", "list", "--server", srv.URL, "--token", "lead")
	if err != nil {
		t.Fatalf("review list: %v", err)
	}
	for _, want := range []string{
		"wsl", "mac", ".claude/CLAUDE.md#Indent",
		"Prefer tabs.", "Prefer spaces.", "0199a000-0000-7000-8000-000000000001",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("listing omitted %q:\n%s", want, out)
		}
	}
}

// A body over the preview cap is cut and says so. A reviewer who cannot see
// that a body was cut cannot decide from the listing.
// Mutation that turns this red: raising reviewBodyPreview past the fixture, or
// dropping the "(truncated…)" line from writePreview.
func TestReviewListTruncation(t *testing.T) {
	long := strings.Repeat("Prefer a very long indent rule. ", 40)
	payload, err := json.Marshal(map[string]any{
		"slot":     ".claude/CLAUDE.md#Indent",
		"hostname": "wsl",
		"pair": []map[string]any{
			{"hash": "aaa", "hostnames": []string{"wsl"}, "body": long},
			{"hash": "bbb", "hostnames": []string{"mac"}, "body": "Prefer spaces."},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"0199a000-0000-7000-8000-000000000002","kind":"import_conflict","status":"open","payload":` + string(payload) + `}]}`))
	}))
	t.Cleanup(srv.Close)

	out, _, err := run(t, Deps{HTTP: srv.Client()}, "review", "list", "--server", srv.URL, "--token", "lead")
	if err != nil {
		t.Fatalf("review list: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "truncated") {
		t.Fatalf("long body had no truncation marker:\n%s", out)
	}
	if strings.Contains(out, long) {
		t.Fatal("listing printed the full body; truncation must be visible")
	}
	if !strings.Contains(out, "wsl") || !strings.Contains(out, "mac") {
		t.Fatalf("truncation hid a hostname:\n%s", out)
	}
}

// An empty queue prints a zero count rather than nothing, so "no output" can
// never be confused with "the command did not run".
// Mutation that turns this red: returning "" from formatReviewList on no items.
func TestReviewListEmptyQueue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)

	out, _, err := run(t, Deps{HTTP: srv.Client()}, "review", "list", "--server", srv.URL, "--token", "lead")
	if err != nil {
		t.Fatalf("review list: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("empty queue printed nothing")
	}
	if !strings.Contains(out, "0") {
		t.Fatalf("empty queue must show a zero count:\n%s", out)
	}
}

// list defaults to the open queue: a listing that silently included decided
// items would make the queue look permanently full.
// Mutation that turns this red: changing the --status default away from open.
func TestReviewListDefaultStatusOpen(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)

	if _, _, err := run(t, Deps{HTTP: srv.Client()},
		"review", "list", "--server", srv.URL, "--token", "lead"); err != nil {
		t.Fatalf("review list: %v", err)
	}
	if !strings.Contains(gotQuery, "status=open") {
		t.Fatalf("list query %q, want status=open", gotQuery)
	}
}
