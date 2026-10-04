package server

import (
	"github.com/agentic-substrate/substrate/internal/authority"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRoutingKeepsAPIErrorsSeparateFromClientRoutes(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    {Data: []byte("<html>client</html>")},
		"assets/app.js": {Data: []byte("console.log('loaded')")},
	}
	handler := New(assets)
	cases := []struct {
		path    string
		code    int
		content string
	}{
		{"/artifacts/example", http.StatusOK, "<html>client</html>"},
		{"/assets/app.js", http.StatusOK, "console.log('loaded')"},
		{"/assets/missing.js", http.StatusNotFound, ""},
		{"/api/missing", http.StatusNotFound, ""},
		{"/api%2Fmissing", http.StatusNotFound, ""},
		{"/mcp%2Ftools", http.StatusNotFound, ""},
		{"/mcp", http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", tc.path, nil))
			if response.Code != tc.code {
				t.Fatalf("status = %d, want %d", response.Code, tc.code)
			}
			body := response.Body.String()
			if tc.content != "" && !strings.Contains(body, tc.content) {
				t.Fatalf("missing served content: %q", body)
			}
			if tc.code == http.StatusNotFound && strings.Contains(body, "<html>client") {
				t.Fatal("API or missing asset fell through to SPA")
			}
		})
	}
}

func TestMissingEntryPointReturnsServerError(t *testing.T) {
	handler := New(fstest.MapFS{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing entry point: status = %d", response.Code)
	}
}

func TestCredentialedContextChecksOriginHostAndBinding(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", root, "init", "-q").Run(); err != nil {
		t.Fatal(err)
	}
	store := &authority.Store{Dir: filepath.Join(home, "state")}
	if err := store.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Register(root, "Personal"); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(root, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(fstest.MapFS{}, Options{Authority: store, Host: "127.0.0.1:9842"})
	cases := []struct {
		name, host, origin, credential, fetchSite string
		code                                      int
	}{
		{"valid browser", "127.0.0.1:9842", "http://127.0.0.1:9842", token, "same-origin", 200},
		{"CLI no origin", "127.0.0.1:9842", "", token, "", 200},
		{"no credential", "127.0.0.1:9842", "", "", "", 401},
		{"forged credential", "127.0.0.1:9842", "", "fake", "", 403},
		{"hostile website", "127.0.0.1:9842", "https://evil.invalid", token, "cross-site", 403},
		{"null origin", "127.0.0.1:9842", "null", token, "", 403},
		{"wrong port", "127.0.0.1:1234", "", token, "", 403},
		{"DNS rebinding", "evil.invalid:9842", "", token, "", 403},
		{"fetch site without origin", "127.0.0.1:9842", "", token, "cross-site", 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://"+tc.host+"/api/context", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.credential != "" {
				req.Header.Set("Authorization", "Bearer "+tc.credential)
			}
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.code {
				t.Fatalf("code %d want %d: %s", response.Code, tc.code, response.Body.String())
			}
			if tc.code != 200 && (strings.Contains(response.Body.String(), root) || strings.Contains(response.Body.String(), "owner_id")) {
				t.Fatal("denial exposed metadata")
			}
		})
	}
	if err := os.Remove(filepath.Join(store.Dir, "authority.json")); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1:9842/api/context", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 503 {
		t.Fatalf("unavailable authority: %d", response.Code)
	}
}
