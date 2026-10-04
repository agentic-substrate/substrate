package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustedCLISetupAndDeniedContext(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	state := filepath.Join(home, "state")
	credential := filepath.Join(home, "session")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	var output bytes.Buffer
	calls := [][]string{
		{"init", "-state-dir", state, "-owner", "Owner"},
		{"space", "-state-dir", state, "-name", "Work"},
		{"register", "-state-dir", state, "-path", root, "-space", "Work"},
		{"session", "-state-dir", state, "-path", root, "-space", "Work", "-out", credential},
		{"context", "-state-dir", state, "-path", root, "-credential", credential},
	}
	for _, args := range calls {
		output.Reset()
		if err := runAuthority(args, &output); err != nil {
			t.Fatalf("%s: %v", args[0], err)
		}
	}
	if !strings.Contains(output.String(), `"space_name":"Work"`) {
		t.Fatalf("missing explicit space: %s", output.String())
	}
	output.Reset()
	if err := runAuthority([]string{"context", "-state-dir", state, "-path", home, "-credential", credential}, &output); err == nil {
		t.Fatal("ambiguous directory accepted")
	}
	if output.Len() != 0 {
		t.Fatalf("denied request revealed metadata: %s", output.String())
	}
	output.Reset()
	if err := runAuthority([]string{"session", "-state-dir", state, "-path", root, "-space", "Work", "-out", filepath.Join(root, "secret")}, &output); err == nil {
		t.Fatal("unsafe credential destination accepted")
	}
	if output.Len() != 0 {
		t.Fatal("failed credential write acknowledged success")
	}
}
