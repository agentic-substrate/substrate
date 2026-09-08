package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseRemoteIsLayoutIndependent(t *testing.T) {
	ssh, ok := ParseRemote(NormalizeRemote("git@github.com:unrounded/adapter-sdk.git"))
	if !ok {
		t.Fatal("ssh remote did not parse")
	}
	https, ok := ParseRemote(NormalizeRemote("https://github.com/unrounded/adapter-sdk"))
	if !ok {
		t.Fatal("https remote did not parse")
	}
	if ssh != https {
		t.Fatalf("ssh %+v and https %+v are the same repo and must derive the same identity", ssh, https)
	}
	if ssh.Org != "unrounded" || ssh.Repo != "adapter-sdk" || ssh.Key != "github.com/unrounded/adapter-sdk" {
		t.Fatalf("identity = %+v, want org unrounded / repo adapter-sdk", ssh)
	}
}

func TestParseRemoteRefusesToGuess(t *testing.T) {
	for _, in := range []string{"", "   ", "github.com", "github.com/unrounded", "/", "not-a-host/org/repo"} {
		if id, ok := ParseRemote(in); ok {
			t.Fatalf("ParseRemote(%q) = %+v, true; an unparseable remote must be skipped, not guessed at", in, id)
		}
	}
}

// A drift proposal about a file in a checkout names that checkout's repo key
// and nothing else. The chain behind the key -- org, team, project -- is the
// server's to resolve and is never sent to, nor accepted from, the adapter.
// A home file has no repo and carries its explicitly configured scope instead.
func TestPostReviewNamesRepoNotChain(t *testing.T) {
	for _, tc := range []struct {
		name       string
		scope      string
		repo       string
		wantFields map[string]string
		absent     string
	}{
		{
			name:       "repo target",
			repo:       "github.com/acme/api",
			wantFields: map[string]string{"repo": "github.com/acme/api"},
			absent:     "scope",
		},
		{
			name:       "home target",
			scope:      "global:/org:acme/team:core",
			wantFields: map[string]string{"scope": "global:/org:acme/team:core"},
			absent:     "repo",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decode: %v", err)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			t.Cleanup(srv.Close)
			a := newAPI(Config{Server: srv.URL})
			if err := a.postReview(t.Context(), tc.scope, tc.repo, "/tmp/AGENTS.md", "diff"); err != nil {
				t.Fatalf("postReview: %v", err)
			}
			for k, want := range tc.wantFields {
				if got[k] != want {
					t.Fatalf("body[%q] = %v, want %q (body: %v)", k, got[k], want, got)
				}
			}
			if _, ok := got[tc.absent]; ok {
				t.Fatalf("body carries %q = %v; the adapter must name a target exactly one way", tc.absent, got[tc.absent])
			}
		})
	}
}
