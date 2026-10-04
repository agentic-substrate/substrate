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
)

func TestCaptureCLIReportsCommittedReceiptAndScopedInspection(t *testing.T) {
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
	common := []string{"-state-dir", state, "-path", root, "-credential", credential}
	args := append(append([]string{"capture"}, common...), "-operation", "capture-cli", "-provenance", "synthetic CLI evidence")
	var output bytes.Buffer
	if err := runArtifact(args, strings.NewReader("survives a CLI process restart\n"), &output); err != nil {
		t.Fatal(err)
	}
	var receipt artifacts.Receipt
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil || receipt.State != "pending-local" {
		t.Fatalf("receipt: %s %v", output.String(), err)
	}
	output.Reset()
	if err := runArtifact(append(append([]string{"artifact"}, common...), "-id", receipt.ArtifactID), strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "survives a CLI process restart") || !strings.Contains(output.String(), "unverified") {
		t.Fatalf("observation missing: %s", output.String())
	}
	output.Reset()
	if err := runArtifact(args, strings.NewReader("original \xff observation"), &output); err == nil || output.Len() != 0 {
		t.Fatalf("invalid UTF-8 capture acknowledged: %s %v", output.String(), err)
	}
	if err := runArtifact(args, strings.NewReader("changed retry payload"), &output); err == nil || output.Len() != 0 {
		t.Fatalf("failed retry acknowledged: %s %v", output.String(), err)
	}
	if err := os.Chmod(filepath.Join(state, "artifacts.db"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runArtifact(args, strings.NewReader("survives a CLI process restart\n"), &output); err == nil || output.Len() != 0 {
		t.Fatalf("unavailable storage acknowledged: %s %v", output.String(), err)
	}
}
