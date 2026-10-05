package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
)

func TestRecoveryCLICompletesOwnerBackupAndFreshRestore(t *testing.T) {
	root := t.TempDir()
	auth := &authority.Store{Dir: filepath.Join(root, "state")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	store, err := artifacts.Open(auth)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup")
	destination := filepath.Join(root, "restored")
	for _, args := range [][]string{{"backup", "-state-dir", auth.Dir, "-out", backup}, {"restore", "-backup", backup, "-state-dir", destination}} {
		var output bytes.Buffer
		if err := runRecovery(args, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), args[0]+" complete") {
			t.Fatalf("missing completion: %s", output.String())
		}
	}
	if _, err := os.Stat(filepath.Join(destination, "artifacts.db")); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryCLIDoesNotAcceptScopedCredentialsOrAcknowledgeFailure(t *testing.T) {
	for _, args := range [][]string{{"backup"}, {"restore"}, {"backup", "unexpected"}, {"restore", "-credential", "session"}, {"backup", "-path", "repo"}, {"backup", "-state-dir", "missing", "-out", filepath.Join(t.TempDir(), "backup")}} {
		var output bytes.Buffer
		if err := runRecovery(args, &output); err == nil || output.Len() != 0 {
			t.Fatalf("invalid command acknowledged: %v %q %v", args, output.String(), err)
		}
	}
}
