package node

import (
	"context"
	"errors"
	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSingletonConcurrentSessionsAndUnavailableNode(t *testing.T) {
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
	n, err := Start(auth, new(false))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if _, err := Start(auth, new(false)); !errors.Is(err, ErrRunning) {
		t.Fatalf("duplicate startup: %v", err)
	}
	req := Request{Action: "capture", Token: token, Checkout: checkout, Contribution: artifacts.Contribution{OperationID: "one", Kind: "memory", Content: "shared otter observation", Provenance: "test"}}
	results := make(chan Response, 2)
	for range 2 {
		go func() { results <- Call(context.Background(), auth.Dir, req) }()
	}
	a, b := <-results, <-results
	if a.Error != "" || b.Error != "" || string(a.Result) != string(b.Result) {
		t.Fatalf("retry across connections %+v %+v", a, b)
	}
	search := Call(context.Background(), auth.Dir, Request{Action: "search", Token: token, Checkout: checkout, Search: artifacts.SearchRequest{Query: "otter"}})
	if search.Error != "" {
		t.Fatal(search.Error)
	}
	n.Close()
	start := time.Now()
	absent := Call(context.Background(), auth.Dir, req)
	if absent.Error == "" || time.Since(start) > 3*time.Second {
		t.Fatalf("unavailable node %+v", absent)
	}
	if _, err := os.Stat(filepath.Join(auth.Dir, "node.sock")); !os.IsNotExist(err) {
		t.Fatal("socket remains")
	}
	n, err = Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if err := auth.RevokeSession(token); err != nil {
		t.Fatal(err)
	}
	denied := Call(context.Background(), auth.Dir, req)
	if denied.Code != "denied" {
		t.Fatalf("revoked connection %+v", denied)
	}
}

func TestPrivateLockAndMalformedFramesFailClosed(t *testing.T) {
	dir := t.TempDir()
	auth := &authority.Store{Dir: filepath.Join(dir, "state")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(auth.Dir, "runtime.lock"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(auth, new(false)); err == nil {
		t.Fatal("unsafe lock accepted")
	}
	os.Chmod(filepath.Join(auth.Dir, "runtime.lock"), 0600)
	n, err := Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if lock, err := Lock(auth); err == nil {
		lock.Close()
		t.Fatal("offline administrator bypassed running node")
	}
	for _, frame := range []string{"{\"action\":\"capture\",\"unknown\":true}\n", "{\"action\":\"capture\"} {}\n", "{\"action\":\"\\ud800\"}\n", "{\"action\":\"\xff\"}\n"} {
		conn, err := net.Dial("unix", filepath.Join(auth.Dir, "node.sock"))
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		io.WriteString(conn, frame)
		var response Response
		err = decodeFrame(conn, &response)
		conn.Close()
		if err != nil || response.Code != "invalid" {
			t.Fatalf("malformed frame accepted %+v %v", response, err)
		}
	}
	pause := true
	response := Call(context.Background(), auth.Dir, Request{Action: "index", Pause: &pause})
	if response.Error != "" || !strings.Contains(string(response.Result), "\"processed\":0") {
		t.Fatalf("pause processed work %+v", response)
	}
}
