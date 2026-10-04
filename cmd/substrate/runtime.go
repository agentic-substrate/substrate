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
	switch args[0] {
	case "node":
		flags.BoolVar(&paused, "index-paused", false, "keep captures durable while discretionary indexing is paused")
	case "mcp":
		flags.StringVar(&checkout, "path", ".", "fixed explicitly registered checkout")
		flags.StringVar(&credential, "credential", "", "private scoped session credential file outside Git")
	case "index":
		flags.BoolVar(&paused, "pause", false, "pause without consuming queued work; false resumes and processes one bounded batch")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("runtime commands do not accept positional arguments")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "node":
		runtime, err := node.Start(&authority.Store{Dir: *dir}, paused)
		if err != nil {
			return err
		}
		defer runtime.Close()
		<-ctx.Done()
		return nil
	case "mcp":
		return mcpbridge.Run(ctx, mcpbridge.Connection{StateDir: *dir, Checkout: checkout, Credential: credential}, in, out)
	case "index":
		response := node.Call(ctx, *dir, node.Request{Action: "index", Pause: &paused})
		if response.Error != "" {
			return errors.New(response.Error)
		}
		_, err := out.Write(append(response.Result, '\n'))
		return err
	}
	return errors.New("unsupported runtime command")
}
