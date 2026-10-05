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
	"io"
)

func TestSourceCLISeparatesProposalsFromOwnerApproval(t *testing.T) {
	home := t.TempDir()
	root, state, credential := filepath.Join(home, "repo"), filepath.Join(home, "state"), filepath.Join(home, "credential")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	cliGit := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	cliGit("init", "-q")
	for _, args := range [][]string{{"init", "-state-dir", state, "-owner", "Owner"}, {"register", "-state-dir", state, "-path", root, "-space", "Personal"}, {"session", "-state-dir", state, "-path", root, "-space", "Personal", "-out", credential}} {
		if err := runAuthority(args, new(bytes.Buffer)); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{"AGENT.md": "approved agent definition", "reference.md": "reference bytes"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cliGit("add", ".")
	cliGit("-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "source")
	runScoped := func(args []string, in io.Reader, out io.Writer) error {
		runtime, err := node.Start(&authority.Store{Dir: state}, new(false))
		if err != nil {
			return err
		}
		defer runtime.Close()
		return runArtifact(args, in, out)
	}
	common := []string{"-state-dir", state, "-path", root, "-credential", credential}
	ownerFlags := []string{"-state-dir", state, "-path", root}
	var output bytes.Buffer
	propose := append(append([]string{"propose"}, common...), "-operation", "proposal", "-kind", "agent-definition", "-commit", cliGit("rev-parse", "HEAD"), "-file", "AGENT.md", "-dependency", "reference.md")
	if err := runScoped(propose, strings.NewReader(""), &output); err != nil {
		t.Fatalf("propose source: %v", err)
	}
	var r artifacts.Receipt
	if err := json.Unmarshal(output.Bytes(), &r); err != nil || r.State != "candidate" {
		t.Fatalf("candidate: %s %v", output.String(), err)
	}
	output.Reset()
	registration := append(append([]string{"source-register"}, ownerFlags...), "-artifact", r.ArtifactID, "-source", "repository", "-name", "assistant", "-alias", "assistant")
	if err := runSource(append(registration, "-credential", credential), &output); err == nil || output.Len() != 0 {
		t.Fatalf("session credential accepted as owner authority: %v", err)
	}
	if err := runSource(registration, &output); err != nil {
		t.Fatal(err)
	}
	var choice artifacts.Choice
	if err := json.Unmarshal(output.Bytes(), &choice); err != nil || choice.Qualified == "" {
		t.Fatalf("source identity: %s %v", output.String(), err)
	}
	output.Reset()
	read := append(append([]string{"read"}, common...), "-selector", choice.Qualified)
	if err := runScoped(read, strings.NewReader(""), &output); err == nil || output.Len() != 0 {
		t.Fatalf("candidate delivered: %s %v", output.String(), err)
	}
	approval := append(append([]string{"approve"}, ownerFlags...), "-artifact", r.ArtifactID, "-revision", r.RevisionID, "-operation", "approval")
	if err := runScoped(append(append([]string{"approve"}, common...), "-artifact", r.ArtifactID), strings.NewReader(""), &output); err == nil {
		t.Fatal("scoped interface offered approval")
	}
	if err := runSource(approval, &output); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runScoped(read, strings.NewReader(""), &output); err != nil || !strings.Contains(output.String(), "approved agent definition") || !strings.Contains(output.String(), "reference bytes") || !strings.Contains(output.String(), "native_activation\":\"unsupported") {
		t.Fatalf("approved delivery: %s %v", output.String(), err)
	}
	output.Reset()
	if err := runScoped(append(append([]string{"choices"}, common...), "-selector", "assistant"), strings.NewReader(""), &output); err != nil || !strings.Contains(output.String(), "effective") {
		t.Fatalf("choices: %s %v", output.String(), err)
	}
	output.Reset()
	stale := append(append([]string{"propose"}, common...), "-operation", "stale-proposal", "-artifact", r.ArtifactID, "-kind", "agent-definition", "-commit", cliGit("rev-parse", "HEAD"), "-file", "AGENT.md", "-dependency", "reference.md")
	if err := runScoped(stale, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	var candidate artifacts.Receipt
	if err := json.Unmarshal(output.Bytes(), &candidate); err != nil || candidate.State != "conflict" {
		t.Fatalf("stale source: %s %v", output.String(), err)
	}
	output.Reset()
	resolution := append(append([]string{"approve"}, ownerFlags...), "-artifact", r.ArtifactID, "-revision", candidate.RevisionID, "-expected", r.RevisionID, "-operation", "resolve-source", "-resolve-conflict")
	if err := runSource(resolution, &output); err != nil {
		t.Fatalf("explicit owner source resolution: %v", err)
	}
	var resolved artifacts.Receipt
	if err := json.Unmarshal(output.Bytes(), &resolved); err != nil || resolved.State != "approved" || resolved.RevisionID == candidate.RevisionID {
		t.Fatalf("source resolution: %s %v", output.String(), err)
	}
	output.Reset()
	if err := runAuthority([]string{"revoke", "-state-dir", state, "-credential", credential}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	if err := runScoped(read, strings.NewReader(""), &output); err == nil || output.Len() != 0 {
		t.Fatalf("revoked content delivered: %s %v", output.String(), err)
	}
}
