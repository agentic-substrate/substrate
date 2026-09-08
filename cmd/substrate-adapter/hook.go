package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/agentic-substrate/substrate/internal/adapter"
)

// hookCmd runs a harness hook shim. It never returns a non-zero exit: a
// capture failure must not fail the user's tool call, so every problem is
// reported on stderr and the process exits 0 (issue #61 AC3).
func hookCmd(args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "substrate-adapter hook: want an event (posttooluse)")
		return 0
	}
	switch args[0] {
	case "posttooluse":
		if err := postToolUse(args[1:], stdin, stderr); err != nil {
			_, _ = fmt.Fprintf(stderr, "substrate-adapter hook posttooluse: %v\n", err)
		}
	default:
		_, _ = fmt.Fprintf(stderr, "substrate-adapter hook: unknown event %q\n", args[0])
	}
	return 0
}

func postToolUse(args []string, stdin io.Reader, stderr io.Writer) error {
	fs := flag.NewFlagSet("hook posttooluse", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	home := fs.String("home", "", "directory <home>/.substrate/adapter.sqlite is resolved against")
	state := fs.String("state", "", "sqlite path (default <home>/.substrate/adapter.sqlite)")
	server := fs.String("server", "", "control plane base URL; empty enqueues only and lets the daemon drain")
	token := fs.String("token", os.Getenv("SUBSTRATE_TOKEN"), "bearer token")
	machine := fs.String("machine", hostname(), "machine name recorded on the observation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *home == "" && *state == "" {
		return fmt.Errorf("-home is required (refuses to guess $HOME)")
	}
	statePath := *state
	if statePath == "" {
		statePath = filepath.Join(*home, ".substrate", "adapter.sqlite")
	}

	in, err := adapter.DecodePostToolUse(stdin)
	if err != nil {
		return err
	}
	obs := adapter.ObservationFrom(in)

	// The repo key comes from the checkout's git remote, never from the
	// filesystem path (#55). A cwd with no remote, an unparseable remote, or
	// one the server has not bound is skipped and logged: there is no
	// `global:` fallback, because no agent token may write there (#63, R18).
	key, ok := adapter.RepoKey(in.CWD)
	if !ok {
		_, _ = fmt.Fprintf(stderr, "substrate-adapter hook posttooluse: skipping observation: cwd %q has no parseable git remote\n", in.CWD)
		return nil
	}
	obs.Repo = key

	db, err := adapter.OpenHook(statePath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), adapter.HookDeadline)
	defer cancel()
	return adapter.HookObservation(ctx, db, adapter.Config{
		Server:    *server,
		Token:     *token,
		Machine:   *machine,
		Home:      *home,
		StatePath: statePath,
	}, obs)
}
