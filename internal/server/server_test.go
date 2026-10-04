package server

import (
	"net/http"
	"net/http/httptest"
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
