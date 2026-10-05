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
	runtime, err := node.Start(&authority.Store{Dir: state}, new(false))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
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
	runtime.Close()
	if err := os.Chmod(filepath.Join(state, "artifacts.db"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runArtifact(args, strings.NewReader("survives a CLI process restart\n"), &output); err == nil || output.Len() != 0 {
		t.Fatalf("unavailable storage acknowledged: %s %v", output.String(), err)
	}
}

func TestCaptureRequiresExplicitNodeWithoutDatabaseFallback(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	state := filepath.Join(home, "state")
	credential := filepath.Join(home, "credential")
	os.Mkdir(root, 0700)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git %v %s", err, out)
	}
	for _, args := range [][]string{{"init", "-state-dir", state, "-owner", "Owner"}, {"register", "-state-dir", state, "-path", root, "-space", "Personal"}, {"session", "-state-dir", state, "-path", root, "-space", "Personal", "-out", credential}} {
		if err := runAuthority(args, new(bytes.Buffer)); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := runArtifact([]string{"capture", "-state-dir", state, "-path", root, "-credential", credential, "-operation", "absent", "-provenance", "synthetic"}, strings.NewReader("not acknowledged"), &out); err == nil || out.Len() != 0 {
		t.Fatalf("node absence saved %+v %s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(state, "artifacts.db")); !os.IsNotExist(err) {
		t.Fatal("node absence opened database")
	}
}

func TestSearchCLIForwardsQueryLimitAndRelatedOrigin(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	state := filepath.Join(home, "state")
	credential := filepath.Join(home, "credential")
	os.Mkdir(root, 0700)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git %v %s", err, out)
	}
	for _, args := range [][]string{{"init", "-state-dir", state, "-owner", "Owner"}, {"register", "-state-dir", state, "-path", root, "-space", "Personal"}, {"session", "-state-dir", state, "-path", root, "-space", "Personal", "-out", credential}} {
		if err := runAuthority(args, new(bytes.Buffer)); err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := node.Start(&authority.Store{Dir: state}, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	common := []string{"--state-dir", state, "--path", root, "--credential", credential}
	save := func(operation, content string, related ...string) artifacts.Receipt {
		t.Helper()
		args := append(append([]string{"capture"}, common...), "--operation", operation, "--provenance", "synthetic")
		for _, id := range related {
			args = append(args, "--related", id)
		}
		var output bytes.Buffer
		if err := runArtifact(args, strings.NewReader(content), &output); err != nil {
			t.Fatal(err)
		}
		var receipt artifacts.Receipt
		if err := json.Unmarshal(output.Bytes(), &receipt); err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	target := save("target", "otter target")
	save("other", "zebra unrelated")
	origin := save("origin", "otter source", target.ArtifactID)
	if err := runRuntime([]string{"index", "--state-dir", state, "--pause=false"}, strings.NewReader(""), new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		flags []string
		count int
		want  string
	}{{"negative query", []string{"--query", "absent-marker"}, 0, ""}, {"query and limit", []string{"--query", "otter", "--limit", "1"}, 1, ""}, {"related origin", []string{"--related-to", origin.ArtifactID}, 1, target.ArtifactID}} {
		t.Run(test.name, func(t *testing.T) {
			args := append(append([]string{"search"}, common...), test.flags...)
			var output bytes.Buffer
			if err := runArtifact(args, strings.NewReader(""), &output); err != nil {
				t.Fatal(err)
			}
			var result artifacts.SearchResponse
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Results) != test.count {
				t.Fatalf("search flags ignored: %+v", result)
			}
			if test.want != "" && result.Results[0].ArtifactID != test.want {
				t.Fatalf("wrong relationship result %+v", result)
			}
		})
	}
	var output bytes.Buffer
	if err := runArtifact(append(append([]string{"search"}, common...), "--limit", "101"), strings.NewReader(""), &output); err == nil || output.Len() != 0 {
		t.Fatalf("invalid limit bypassed: %v %s", err, output.String())
	}
}
