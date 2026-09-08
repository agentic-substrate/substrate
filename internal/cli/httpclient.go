package cli

import (
	"net/http"
	"time"
)

// HTTPTimeout is the deadline on every control-plane call. It replaces the
// three separately-maintained copies this package consolidated.
const HTTPTimeout = 30 * time.Second

// NewHTTPClient returns the client every substrate command talks to the
// control plane with.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: HTTPTimeout}
}
