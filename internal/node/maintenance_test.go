package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agentic-substrate/substrate/internal/artifacts"
	"github.com/agentic-substrate/substrate/internal/authority"
)

func maintenanceNode(t *testing.T) (*authority.Store, string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "node-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	auth := &authority.Store{Dir: filepath.Join(dir, "state")}
	checkout := filepath.Join(dir, "repo")
	if err := os.Mkdir(checkout, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", checkout, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git: %v %s", err, out)
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
	return auth, checkout, token
}

func TestMaintenancePauseSurvivesRestartAndReceipt(t *testing.T) {
	auth, checkout, token := maintenanceNode(t)
	n, err := Start(auth, new(false))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	paused := true
	if r := Call(context.Background(), auth.Dir, Request{Action: "index", Pause: &paused}); r.Error != "" {
		t.Fatal(r.Error)
	}
	req := Request{Action: "capture", Token: token, Checkout: checkout, Contribution: artifacts.Contribution{OperationID: "paused-save", Kind: "memory", Content: "offline narwhal", Provenance: "test"}}
	saved := Call(context.Background(), auth.Dir, req)
	if saved.Error != "" {
		t.Fatal(saved.Error)
	}
	n.Close()
	n, err = Start(auth, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	state := Call(context.Background(), auth.Dir, Request{Action: "index"})
	var result struct {
		Processed int  `json:"processed"`
		Paused    bool `json:"paused"`
	}
	if err := json.Unmarshal(state.Result, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Paused || result.Processed != 0 {
		t.Fatalf("restart discarded durable pause: %s", state.Result)
	}
	if retried := Call(context.Background(), auth.Dir, req); string(retried.Result) != string(saved.Result) {
		t.Fatalf("receipt changed: %+v %+v", saved, retried)
	}
	paused = false
	if r := Call(context.Background(), auth.Dir, Request{Action: "index", Pause: &paused}); r.Error != "" {
		t.Fatal(r.Error)
	}
	searched := Call(context.Background(), auth.Dir, Request{Action: "search", Token: token, Checkout: checkout, Search: artifacts.SearchRequest{Query: "narwhal"}})
	var found artifacts.SearchResponse
	if err := json.Unmarshal(searched.Result, &found); err != nil || len(found.Results) != 1 {
		t.Fatalf("resume lost observation: %s %v", searched.Result, err)
	}
}

func TestCloseCancelsIncompleteAcceptedFrame(t *testing.T) {
	auth, _, _ := maintenanceNode(t)
	n, err := Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	conn, err := net.Dial("unix", filepath.Join(auth.Dir, "node.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"action":`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(n.slots) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	done := make(chan struct{})
	go func() { n.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		conn.Close()
		<-done
		t.Fatal("shutdown waited for incomplete frame deadline")
	}
}

func TestMaintenanceControlsDoNotResumeOnStatusOrRebuild(t *testing.T) {
	auth, checkout, token := maintenanceNode(t)
	n, err := Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	req := Request{Action: "capture", Token: token, Checkout: checkout, Contribution: artifacts.Contribution{OperationID: "controls", Kind: "memory", Content: "paused control", Provenance: "test"}}
	if r := Call(context.Background(), auth.Dir, req); r.Error != "" {
		t.Fatal(r.Error)
	}
	for _, control := range []string{"status", "rebuild", "status", "run", "retry", "status"} {
		response := Call(context.Background(), auth.Dir, Request{Action: "index", Control: control})
		var state struct {
			artifacts.Maintenance
			Processed int
		}
		if err := json.Unmarshal(response.Result, &state); err != nil || response.Error != "" || !state.Paused || state.Processed != 0 {
			t.Fatalf("%s changed pause/work: %+v %v", control, response, err)
		}
	}
	if r := Call(context.Background(), auth.Dir, Request{Action: "index", Control: "status", Pause: new(false)}); r.Code != "invalid" {
		t.Fatalf("mixed controls accepted %+v", r)
	}
	n.Close()
	n, err = Start(auth, new(false))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	response := Call(context.Background(), auth.Dir, Request{Action: "index", Control: "status"})
	var state artifacts.Maintenance
	if err := json.Unmarshal(response.Result, &state); err != nil || state.Paused || state.Deferred != 1 {
		t.Fatalf("explicit startup override lost bulk %+v %v", state, err)
	}
	if r := Call(context.Background(), auth.Dir, Request{Action: "index", Control: "run"}); r.Error != "" {
		t.Fatal(r.Error)
	}
}

func TestScopedMaintenanceDenialAndUnavailableRemainDistinct(t *testing.T) {
	auth, checkout, token := maintenanceNode(t)
	n, err := Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	req := Request{Action: "browser-inventory", Checkout: checkout, Token: "invalid"}
	if r := Call(context.Background(), auth.Dir, req); r.Code != "denied" || len(r.Result) != 0 {
		t.Fatalf("invalid credential exposed maintenance %+v", r)
	}
	if err := auth.RevokeSession(token); err != nil {
		t.Fatal(err)
	}
	req.Token = token
	if r := Call(context.Background(), auth.Dir, req); r.Code != "denied" || len(r.Result) != 0 {
		t.Fatalf("revoked credential exposed maintenance %+v", r)
	}
	req.Token, err = auth.CreateSession(checkout, "Personal")
	if err != nil {
		t.Fatal(err)
	}
	n.Close()
	if r := Call(context.Background(), auth.Dir, req); r.Code != "unavailable" || len(r.Result) != 0 {
		t.Fatalf("unavailable node reported readiness %+v", r)
	}
}

func TestStatusPollingCannotDelayScheduledIncrementalWork(t *testing.T) {
	auth, checkout, token := maintenanceNode(t)
	n, err := Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	for i := 0; i < 105; i++ {
		req := Request{Action: "capture", Token: token, Checkout: checkout, Contribution: artifacts.Contribution{OperationID: fmt.Sprint("poll-", i), Kind: "memory", Content: "scheduled indexing", Provenance: "test"}}
		if r := Call(context.Background(), auth.Dir, req); r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	if r := Call(context.Background(), auth.Dir, Request{Action: "index", Pause: new(false)}); r.Error != "" {
		t.Fatal(r.Error)
	}
	deadline := time.Now().Add(time.Second)
	var state artifacts.Maintenance
	for time.Now().Before(deadline) {
		response := Call(context.Background(), auth.Dir, Request{Action: "index", Control: "status"})
		if err := json.Unmarshal(response.Result, &state); err != nil {
			t.Fatal(err)
		}
		if state.Queued == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("status polling postponed scheduled work: %+v", state)
}

func TestConcurrentPauseCaptureIndexAndShutdownPreserveAcknowledgements(t *testing.T) {
	auth, checkout, token := maintenanceNode(t)
	n, err := Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	var wg sync.WaitGroup
	acknowledged := make(chan Request, 16)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%3 == 0 {
				Call(context.Background(), auth.Dir, Request{Action: "index", Pause: new(i%2 == 0)})
				return
			}
			req := Request{Action: "capture", Token: token, Checkout: checkout, Contribution: artifacts.Contribution{OperationID: fmt.Sprint("race-", i), Kind: "memory", Content: "durable race observation", Provenance: "test"}}
			if r := Call(context.Background(), auth.Dir, req); r.Error == "" {
				acknowledged <- req
			}
		}(i)
	}
	wg.Wait()
	n.Close()
	close(acknowledged)
	n, err = Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	count := 0
	for req := range acknowledged {
		count++
		if r := Call(context.Background(), auth.Dir, req); r.Error != "" {
			t.Fatalf("lost acknowledged operation %+v", r)
		}
	}
	if count != 8 {
		t.Fatalf("capture failed before shutdown: %d acknowledgements", count)
	}
}
