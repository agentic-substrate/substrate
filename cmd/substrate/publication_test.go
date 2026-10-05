package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
)

func TestPublicationCLIOwnerReviewBoundary(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "state")
	auth := &authority.Store{Dir: state}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	if err := auth.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	tokens := map[string]string{}
	contexts := map[string]authority.Context{}
	for _, space := range []string{"Work", "Personal"} {
		root := filepath.Join(home, space)
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git %v %s", err, out)
		}
		if _, err := auth.Register(root, space); err != nil {
			t.Fatal(err)
		}
		token, err := auth.CreateSession(root, space)
		if err != nil {
			t.Fatal(err)
		}
		paths[space] = root
		tokens[space] = token
		contexts[space], err = auth.Authenticate(token, root)
		if err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	for _, args := range [][]string{{"publication-policy", "-state-dir", state, "-path", paths["Work"], "-action", "export", "-decision", "allow"}, {"publication-policy", "-state-dir", state, "-path", paths["Personal"], "-action", "publish", "-decision", "allow"}} {
		if err := runSource(args, &out); err != nil {
			t.Fatalf("trusted policy command: %v", err)
		}
	}
	runtime, err := node.Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err := runSource([]string{"publication-policy", "-state-dir", state, "-path", paths["Work"], "-action", "export", "-decision", "deny"}, new(bytes.Buffer)); err == nil {
		t.Fatal("owner command bypassed active installation lock")
	}
	response := node.Call(t.Context(), state, node.Request{Action: "capture", Token: tokens["Work"], Contribution: artifacts.Contribution{OperationID: "source", Kind: "memory", Content: "Restricted source", Provenance: "private"}})
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	var receipt artifacts.Receipt
	if json.Unmarshal(response.Result, &receipt) != nil {
		t.Fatal("source receipt")
	}
	input := artifacts.PublicationInput{OperationID: "derive", Content: "A generalized lesson", Sources: []artifacts.PublicationSource{{ArtifactID: receipt.ArtifactID, RevisionID: receipt.RevisionID}}, Destination: artifacts.PublicationDestination{SpaceID: contexts["Personal"].SpaceID, RepositoryID: contexts["Personal"].RepositoryID}}
	response = node.Call(t.Context(), state, node.Request{Action: "publication-propose", Token: tokens["Work"], Publication: input})
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	var p artifacts.Publication
	if json.Unmarshal(response.Result, &p) != nil {
		t.Fatal("proposal")
	}
	runtime.Close()
	credential := filepath.Join(home, "human-review")
	grant := []string{"review-grant", "-state-dir", state, "-path", paths["Work"], "-proposal", p.ID, "-revision", p.RevisionID, "-destination-path", paths["Personal"], "-out", credential}
	out.Reset()
	if err := runSource(grant, &out); err != nil {
		t.Fatal(err)
	}
	token, err := authority.ReadCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), token) {
		t.Fatal("review token printed")
	}
	if _, err := auth.Authenticate(token, ""); err == nil {
		t.Fatal("review credential became harness authority")
	}
	if err := runSource(append(grant, "-credential", credential), new(bytes.Buffer)); err == nil {
		t.Fatal("review issuance accepted session authority flag")
	}
	if err := runSource([]string{"review-grant", "-state-dir", state, "-path", paths["Work"], "-proposal", p.ID, "-revision", p.RevisionID, "-destination-path", paths["Personal"], "-out", filepath.Join(paths["Work"], "secret")}, new(bytes.Buffer)); err == nil {
		t.Fatal("review credential written to Git")
	}
	if err := runSource([]string{"review-revoke", "-state-dir", state, "-path", paths["Work"], "-review-credential", credential}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	runtime, err = node.Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if r := node.Call(t.Context(), state, node.Request{Action: "publication-review", Token: token}); r.Code != "denied" {
		t.Fatalf("revoked review credential reused: %+v", r)
	}
}
