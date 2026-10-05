package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/agentic-substrate/substrate/internal/authority"
)

type Options struct {
	Authority *authority.Store
	Host      string
}

func (options Options) allowedRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.Host)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || r.Host != options.Host {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) > 1 || (len(origins) == 1 && origins[0] != "http://"+options.Host) {
		return false
	}
	if len(r.Header.Values("Sec-Fetch-Site")) > 1 {
		return false
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "" || site == "same-origin" || site == "none"
}
func (options Options) context(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !options.allowedRequest(r) {
		http.Error(w, "Request origin or host denied.", http.StatusForbidden)
		return
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		http.Error(w, "Session credential required. Create one through trusted local setup.", http.StatusUnauthorized)
		return
	}
	ctx, err := options.Authority.Authenticate(strings.TrimPrefix(values[0], "Bearer "), "")
	if err != nil {
		if errors.Is(err, authority.ErrUnavailable) {
			http.Error(w, "Local authority unavailable. Repair or initialize trusted local setup.", http.StatusServiceUnavailable)
		} else {
			http.Error(w, "Context denied. Use a trusted credential for a registered checkout and space.", http.StatusForbidden)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ctx)
}
