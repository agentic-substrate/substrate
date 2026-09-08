package adapter

import (
	"fmt"
	"strings"

	"github.com/agentic-substrate/substrate/internal/scope"
)

// RemoteIdentity is the org and repo a normalized remote names. It is the
// layout-independent identity of a checkout: `git@github.com:unrounded/api.git`
// and `https://github.com/unrounded/api` both yield {unrounded, api}, whether
// the checkout lives at /work/unrounded/api or ~/repos/api.
type RemoteIdentity struct {
	Host string
	Org  string
	Repo string
	// Key is the repo scope key the server binds this remote to: the
	// normalized remote itself (`scope.kind='repo' AND key=<remote_norm>`).
	Key string
}

// ParseRemote derives the org/repo identity of a normalized remote. It reports
// false for a remote it cannot parse; the caller must then skip the checkout
// rather than guess. A remote is never turned into a scope here — that binding
// belongs to the server (R18).
func ParseRemote(remote string) (RemoteIdentity, bool) {
	r := strings.Trim(strings.TrimSpace(remote), "/")
	if r == "" {
		return RemoteIdentity{}, false
	}
	parts := strings.Split(r, "/")
	if len(parts) < 3 {
		return RemoteIdentity{}, false
	}
	host, org, repo := parts[0], parts[len(parts)-2], parts[len(parts)-1]
	if host == "" || org == "" || repo == "" || !strings.Contains(host, ".") {
		return RemoteIdentity{}, false
	}
	return RemoteIdentity{Host: host, Org: org, Repo: repo, Key: r}, true
}

// homeScope is the scope a home-file drift proposal carries. A file like
// ~/.claude/CLAUDE.md has no repo, so it needs an explicitly configured scope;
// with none, the proposal is skipped and the file left alone rather than
// filed at a scope no agent token may write (#58).
func (cfg Config) homeScope() (string, error) {
	if cfg.Scope == "" {
		return "", fmt.Errorf("adapter: no -scope configured for home-scoped targets")
	}
	p, err := scope.Parse(cfg.Scope)
	if err != nil {
		return "", fmt.Errorf("adapter: -scope %q: %w", cfg.Scope, err)
	}
	return p.String(), nil
}
