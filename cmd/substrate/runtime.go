package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/agentic-substrate/substrate/internal/mcpbridge"
	"github.com/agentic-substrate/substrate/internal/node"
)

func runRuntime(args []string, in io.Reader, out io.Writer) error {
	defaultDir, err := defaultStateDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := flags.String("state-dir", defaultDir, "private local state directory outside Git")
	var checkout, credential string
	var paused bool
	var status, rebuild, run, retry bool
	switch args[0] {
	case "node":
		flags.BoolVar(&paused, "index-paused", false, "keep captures durable while discretionary indexing is paused")
	case "mcp":
		flags.StringVar(&checkout, "path", ".", "fixed explicitly registered checkout")
		flags.StringVar(&credential, "credential", "", "private scoped session credential file outside Git")
	case "index":
		flags.BoolVar(&paused, "pause", false, "pause without consuming queued work; false resumes and processes one bounded batch")
		flags.BoolVar(&status, "status", false, "inspect maintenance without changing or running work")
		flags.BoolVar(&rebuild, "rebuild", false, "queue a discretionary full lexical rebuild")
		flags.BoolVar(&run, "run", false, "process at most 100 incremental or bulk jobs when unpaused")
		flags.BoolVar(&retry, "retry", false, "clear recorded failures for an explicit retry")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("runtime commands do not accept positional arguments")
	}
	var pauseOverride *bool
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "pause" || f.Name == "index-paused" {
			pauseOverride = &paused
		}
	})
	control := ""
	controls := 0
	for _, c := range []struct {
		name     string
		selected bool
	}{{"status", status}, {"rebuild", rebuild}, {"run", run}, {"retry", retry}} {
		if c.selected {
			controls++
			control = c.name
		}
	}
	if pauseOverride != nil {
		controls++
	}
	if controls > 1 {
		return errors.New("index controls are mutually exclusive")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "node":
		runtime, err := node.Start(&authority.Store{Dir: *dir}, pauseOverride)
		if err != nil {
			return err
		}
		defer runtime.Close()
		<-ctx.Done()
		return nil
	case "mcp":
		return mcpbridge.Run(ctx, mcpbridge.Connection{StateDir: *dir, Checkout: checkout, Credential: credential}, in, out)
	case "index":
		if controls == 0 {
			pauseOverride = new(false)
		}
		response := node.Call(ctx, *dir, node.Request{Action: "index", Pause: pauseOverride, Control: control})
		if response.Error != "" {
			return errors.New(response.Error)
		}
		_, err := out.Write(append(response.Result, '\n'))
		return err
	}
	return errors.New("unsupported runtime command")
}
