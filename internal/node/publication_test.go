package node

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
)

func TestOrdinarySessionCannotReviewPublication(t *testing.T) {
	home := t.TempDir()
	checkout := filepath.Join(home, "repo")
	if err := os.Mkdir(checkout, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", checkout, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git: %v %s", err, out)
	}
	auth := &authority.Store{Dir: filepath.Join(home, "state")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Register(checkout, "Personal"); err != nil {
		t.Fatal(err)
	}
	token, err := auth.CreateSession(checkout, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.Open(auth)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	n := &Node{auth: auth, store: store}
	for _, action := range []string{"publication-review", "publication-publish"} {
		response := n.dispatch(Request{Action: action, Token: token, Checkout: checkout})
		if response.Code != "denied" {
			t.Fatalf("scoped token accepted for %s: %+v", action, response)
		}
	}
}
