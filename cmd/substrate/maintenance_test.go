package main

import (
	"bytes"
	"encoding/json"
	"github.com/agentic-substrate/substrate/internal/authority"
	"path/filepath"
	"testing"

	"github.com/agentic-substrate/substrate/internal/node"
)

func TestIndexCLIStatusPauseAndExclusiveControls(t *testing.T) {
	auth := &authority.Store{Dir: filepath.Join(t.TempDir(), "state")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	runtime, err := node.Start(auth, new(true))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	for _, args := range [][]string{{"-status"}, {"-rebuild"}, {"-run"}, {"-retry"}, {"-pause=true"}} {
		var output bytes.Buffer
		command := append([]string{"index", "-state-dir", auth.Dir}, args...)
		if err := runRuntime(command, new(bytes.Buffer), &output); err != nil {
			t.Fatal(err)
		}
		var status struct {
			Paused    bool
			Processed int
		}
		if err := json.Unmarshal(output.Bytes(), &status); err != nil || !status.Paused || status.Processed != 0 {
			t.Fatalf("control lost persistent pause %s %v", output.String(), err)
		}
	}
	for _, args := range [][]string{{"-status", "-pause=false"}, {"-rebuild", "-run"}, {"-retry", "-status"}} {
		var output bytes.Buffer
		command := append([]string{"index", "-state-dir", auth.Dir}, args...)
		if err := runRuntime(command, new(bytes.Buffer), &output); err == nil || output.Len() != 0 {
			t.Fatalf("mixed controls accepted %v %s", args, output.String())
		}
	}
	var output bytes.Buffer
	if err := runRuntime([]string{"index", "-state-dir", auth.Dir}, new(bytes.Buffer), &output); err != nil {
		t.Fatal(err)
	}
	var state struct{ Paused bool }
	if err := json.Unmarshal(output.Bytes(), &state); err != nil || state.Paused {
		t.Fatalf("default index did not resume %s %v", output.String(), err)
	}
}
