package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTokenMintRequiresDSN(t *testing.T) {
	err := tokenCmd([]string{"mint", "--for", "agent", "--parent", "00000000-0000-0000-0000-000000000000", "--machine", "wsl"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("got %v, want DSN required", err)
	}
}

func TestTokenUnknownCommand(t *testing.T) {
	err := tokenCmd([]string{"frob"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("got %v, want unknown command", err)
	}
}

func TestTokenUsage(t *testing.T) {
	err := tokenCmd(nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "mint or revoke") {
		t.Fatalf("got %v, want usage", err)
	}
}
