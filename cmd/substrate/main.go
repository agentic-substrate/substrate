package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentic-substrate/substrate/internal/server"
	"github.com/agentic-substrate/substrate/internal/webui"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		if len(os.Args) != 2 {
			return errors.New("version does not accept arguments")
		}
		fmt.Println("substrate 0.0.0 (bootstrap)")
		return nil
	}
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		return errors.New("usage: substrate serve [-listen 127.0.0.1:9842] | version")
	}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	address := flags.String("listen", "127.0.0.1:9842", "loopback address for the local browser interface")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("serve does not accept positional arguments")
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return fmt.Errorf("listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("the bootstrap interface requires a numeric loopback listen address")
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return err
	}
	app := &http.Server{
		Handler: server.New(webui.Assets()), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("Local browser interface: http://%s", listener.Addr())
	return serve(ctx, app, listener)
}
