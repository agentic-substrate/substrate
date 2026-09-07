package adapter

import "testing"

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

// A remote names an org and a repo and nothing between them, so a locally
// invented chain would skip team and project — which scope.Validate rejects.
// The chain therefore comes from the server, and is checked before use.
func TestScopeForRemoteRejectsWhatValidateRejects(t *testing.T) {
	const remote = "github.com/acme/api"
	good := "global:/org:acme/team:core/project:api/repo:github.com%2Facme%2Fapi"
	if got, err := scopeForRemote(remote, good); err != nil || got != good {
		t.Fatalf("scopeForRemote(good) = %q, %v", got, err)
	}
	bad := map[string]string{
		"skipped levels": "global:/org:acme/repo:github.com%2Facme%2Fapi",
		"global default": "global:",
		"org only":       "global:/org:acme",
		"other repo":     "global:/org:acme/team:core/project:api/repo:github.com%2Facme%2Fweb",
		"empty":          "",
	}
	for name, wire := range bad {
		t.Run(name, func(t *testing.T) {
			if got, err := scopeForRemote(remote, wire); err == nil {
				t.Fatalf("scopeForRemote(%q) = %q, nil; want an error", wire, got)
			}
		})
	}
}
