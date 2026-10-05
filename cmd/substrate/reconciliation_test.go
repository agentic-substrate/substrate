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

func TestReconciliationCLISelectsConflictAndRestoresFreshHead(t *testing.T) {
	home := t.TempDir()
	root, state, credential := filepath.Join(home, "repo"), filepath.Join(home, "state"), filepath.Join(home, "credential")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for _, args := range [][]string{{"init", "-state-dir", state, "-owner", "Owner"}, {"register", "-state-dir", state, "-path", root, "-space", "Personal"}, {"session", "-state-dir", state, "-path", root, "-space", "Personal", "-out", credential}} {
		if err := runAuthority(args, new(bytes.Buffer)); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := node.Start(&authority.Store{Dir: state}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	common := []string{"-state-dir", state, "-path", root, "-credential", credential}
	call := func(command, content string, flags ...string) artifacts.Receipt {
		t.Helper()
		args := append(append([]string{command}, common...), flags...)
		var output bytes.Buffer
		if err := runArtifact(args, strings.NewReader(content), &output); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		var receipt artifacts.Receipt
		if err := json.Unmarshal(output.Bytes(), &receipt); err != nil {
			t.Fatalf("receipt: %s %v", output.String(), err)
		}
		return receipt
	}
	first := call("capture", "original", "-operation", "first")
	current := call("capture", "accepted", "-artifact", first.ArtifactID, "-expected", first.RevisionID, "-operation", "current")
	candidate := call("capture", "competing", "-artifact", first.ArtifactID, "-expected", first.RevisionID, "-operation", "candidate")
	if candidate.State != "conflict" {
		t.Fatalf("stale replacement activated: %+v", candidate)
	}
	resolved := call("resolve-conflict", "", "-id", first.ArtifactID, "-revision", candidate.RevisionID, "-expected", current.RevisionID, "-operation", "choose")
	if resolved.State != "pending-local" || resolved.RevisionID == candidate.RevisionID || resolved.RevisionID == current.RevisionID {
		t.Fatalf("resolution did not create a fresh local revision: %+v", resolved)
	}
	call("retire", "", "-id", first.ArtifactID, "-expected", resolved.RevisionID, "-operation", "retire")
	runtime.Close()
	var output bytes.Buffer
	restore := []string{"restore-artifact", "-state-dir", state, "-path", root, "-artifact", first.ArtifactID, "-expected", resolved.RevisionID, "-operation", "restore"}
	if err := runSource(restore, &output); err != nil {
		t.Fatalf("explicit owner restoration: %v", err)
	}
	var restored artifacts.Receipt
	if err := json.Unmarshal(output.Bytes(), &restored); err != nil || restored.RevisionID == resolved.RevisionID || restored.State != "pending-local" {
		t.Fatalf("restoration reused retired head: %s %v", output.String(), err)
	}
	runtime, err = node.Start(&authority.Store{Dir: state}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	old := call("capture", "old edit", "-artifact", first.ArtifactID, "-expected", resolved.RevisionID, "-operation", "old-edit")
	if old.State != "conflict" {
		t.Fatalf("old edit replaced restored content: %+v", old)
	}
	output.Reset()
	if err := runArtifact(append(append([]string{"read"}, common...), "-id", first.ArtifactID), strings.NewReader(""), &output); err != nil || !strings.Contains(output.String(), "competing") {
		t.Fatalf("resolved content unavailable: %s %v", output.String(), err)
	}
	runtime.Close()
	output.Reset()
	if err := runArtifact(append(append([]string{"resolve-conflict"}, common...), "-id", first.ArtifactID, "-revision", old.RevisionID, "-expected", restored.RevisionID, "-operation", "unavailable"), strings.NewReader(""), &output); err == nil || output.Len() != 0 {
		t.Fatalf("resolution without node acknowledged: %s %v", output.String(), err)
	}
}
