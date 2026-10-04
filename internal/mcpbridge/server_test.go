package mcpbridge

import (
	"context"
	"encoding/json"
	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/node"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMCPToolsSaveReadDenyAndLeaveNodeAlive(t *testing.T) {
	dir := t.TempDir()
	auth := &authority.Store{Dir: filepath.Join(dir, "state")}
	checkout := filepath.Join(dir, "repo")
	os.Mkdir(checkout, 0700)
	if out, err := exec.Command("git", "-C", checkout, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git %v %s", err, out)
	}
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
	credential := filepath.Join(dir, "credential")
	if err := authority.WriteCredential(credential, token); err != nil {
		t.Fatal(err)
	}
	n, err := node.Start(auth, false)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connect := func() (*mcp.ClientSession, *mcp.ServerSession) {
		ct, st := mcp.NewInMemoryTransports()
		ss, err := New(Connection{auth.Dir, checkout, credential}).Connect(ctx, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "synthetic integration", Version: "1"}, nil).Connect(ctx, ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		return cs, ss
	}
	client, server := connect()
	defer client.Close()
	defer server.Close()
	tools, err := client.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 4 {
		t.Fatalf("tool discovery %+v %v", tools, err)
	}
	call := func(client *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
		t.Helper()
		r, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	result := call(client, "capture", map[string]any{"operation_id": "mcp-save", "content": "synthetic otter observation", "provenance": "integration test"})
	if result.IsError {
		t.Fatalf("capture %+v", result)
	}
	var saved struct {
		ArtifactID string `json:"artifact_id"`
		RevisionID string `json:"revision_id"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &saved); err != nil {
		t.Fatal(err)
	}
	second, secondServer := connect()
	defer second.Close()
	defer secondServer.Close()
	result = call(second, "read", map[string]any{"revision_id": saved.RevisionID})
	if result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "synthetic otter observation") {
		t.Fatalf("second session read %+v", result)
	}
	result = call(second, "read", map[string]any{"artifact_id": "unknown"})
	if !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "denied") {
		t.Fatalf("missing denial %+v", result)
	}
	if err := auth.RevokeSession(token); err != nil {
		t.Fatal(err)
	}
	if result := call(second, "search", map[string]any{"query": "otter"}); !result.IsError {
		t.Fatal("revoked search accepted")
	}
	client.Close()
	server.Close()
	lock, err := node.Lock(auth)
	if err == nil {
		lock.Close()
		t.Fatal("bridge close stopped runtime")
	}
	n.Close()
	if result := call(second, "discovery", map[string]any{}); !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "unavailable") {
		t.Fatalf("node absence %+v", result)
	}
}
func TestFramesRejectInvalidUnicodeAndBounds(t *testing.T) {
	for _, input := range []string{"{\"x\":\"\xff\"}\n", "{\"x\":\"\\ud800\"}\n", "{\"x\":\"\\udc00\"}\n", strings.Repeat("x", node.MaxFrame) + "\n"} {
		if _, err := io.ReadAll(newFrameReader(strings.NewReader(input))); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
	for _, input := range []string{"{\"x\":\"😀\"}\n", "{\"x\":\"\\ud83d\\ude00\"}\n", "{\"x\":\"\\\\ud800\"}\n"} {
		if _, err := io.ReadAll(newFrameReader(strings.NewReader(input))); err != nil {
			t.Fatalf("valid frame rejected %q %v", input, err)
		}
	}
}
