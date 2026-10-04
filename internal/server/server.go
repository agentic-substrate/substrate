package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// New keeps API failures separate from client-side navigation.
func New(assets fs.FS, options ...Options) http.Handler {
	var config Options
	if len(options) > 0 {
		config = options[0]
	}
	mux := http.NewServeMux()
	if config.Authority != nil {
		mux.HandleFunc("GET /api/context", config.context)
	}
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]string{"stage": "bootstrap"})
	})
	mux.HandleFunc("/api", http.NotFound)
	mux.HandleFunc("/api/", http.NotFound)
	mux.HandleFunc("/mcp", http.NotFound)
	mux.HandleFunc("/mcp/", http.NotFound)
	files := http.FileServerFS(assets)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "api" || strings.HasPrefix(name, "api/") || name == "mcp" || strings.HasPrefix(name, "mcp/") {
			http.NotFound(w, r)
			return
		}
		if name != "" && name != "." {
			if info, err := fs.Stat(assets, name); err == nil && !info.IsDir() {
				w.Header().Set("Cache-Control", "no-cache")
				files.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		entry, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			http.Error(w, "Application assets unavailable. Rebuild the executable.", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(entry)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		mux.ServeHTTP(w, r)
	})
}
