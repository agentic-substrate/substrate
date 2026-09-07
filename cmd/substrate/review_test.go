package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewUsage(t *testing.T) {
	// Returning nil from reviewCmd with no args is the one-line change that makes this red.
	err := reviewCmd(nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "list") {
		t.Fatalf("got %v, want usage naming list", err)
	}
}

func TestReviewUnknownCommand(t *testing.T) {
	// Treating unknown subcommands as list is the one-line change that makes this red.
	err := reviewCmd([]string{"frob"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("got %v, want unknown command", err)
	}
}

func TestReviewListShowsHostnameSlotAndBodies(t *testing.T) {
	// Omitting a side's hostname from formatReviewList is the one-line change that makes this red.
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

	stdout := &bytes.Buffer{}
	err := reviewCmd([]string{"list", "-server", srv.URL, "-token", "lead"}, stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, "wsl") {
		t.Fatalf("listing omitted hostname wsl:\n%s", out)
	}
	if !strings.Contains(out, "mac") {
		t.Fatalf("listing omitted hostname mac:\n%s", out)
	}
	if !strings.Contains(out, ".claude/CLAUDE.md#Indent") {
		t.Fatalf("listing omitted slot:\n%s", out)
	}
	if !strings.Contains(out, "Prefer tabs.") {
		t.Fatalf("listing omitted wsl body:\n%s", out)
	}
	if !strings.Contains(out, "Prefer spaces.") {
		t.Fatalf("listing omitted mac body:\n%s", out)
	}
	if !strings.Contains(out, "0199a000-0000-7000-8000-000000000001") {
		t.Fatalf("listing omitted id:\n%s", out)
	}
}

func TestReviewListTruncationIsObvious(t *testing.T) {
	// Raising reviewBodyPreview above the fixture length is the one-line change that makes this red.
	long := strings.Repeat("Prefer a very long indent rule. ", 40) // well over the preview cap
	payload, err := json.Marshal(map[string]any{
		"slot":     ".claude/CLAUDE.md#Indent",
		"hostname": "wsl",
		"pair": []map[string]any{
			{"hash": "aaa", "hostnames": []string{"wsl"}, "body": long},
			{"hash": "bbb", "hostnames": []string{"mac"}, "body": "Prefer spaces."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"0199a000-0000-7000-8000-000000000002","kind":"import_conflict","status":"open","payload":` + string(payload) + `}]}`))
	}))
	t.Cleanup(srv.Close)

	stdout := &bytes.Buffer{}
	if err := reviewCmd([]string{"list", "-server", srv.URL, "-token", "lead"}, stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
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

func TestReviewListEmptyQueueIsExplicit(t *testing.T) {
	// Printing nothing when items is empty is the one-line change that makes this red.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)

	stdout := &bytes.Buffer{}
	if err := reviewCmd([]string{"list", "-server", srv.URL, "-token", "lead"}, stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if strings.TrimSpace(out) == "" {
		t.Fatal("empty queue printed nothing")
	}
	if !strings.Contains(out, "0") {
		t.Fatalf("empty queue must show a zero count:\n%s", out)
	}
}

func TestReviewListRequiresServerAndToken(t *testing.T) {
	t.Setenv("SUBSTRATE_URL", "")
	t.Setenv("SUBSTRATE_TOKEN", "")
	err := reviewCmd([]string{"list"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-server") {
		t.Fatalf("got %v, want -server required", err)
	}
	err = reviewCmd([]string{"list", "-server", "http://127.0.0.1:9"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-token") {
		t.Fatalf("got %v, want -token required", err)
	}
}

func TestReviewListDefaultStatusOpen(t *testing.T) {
	// Dropping status=open from the GET is the one-line change that makes this red.
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)

	if err := reviewCmd([]string{"list", "-server", srv.URL, "-token", "lead"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "status=open") {
		t.Fatalf("list query %q, want status=open", gotQuery)
	}
}
