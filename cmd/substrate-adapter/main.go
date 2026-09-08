// Command substrate-adapter is the per-machine daemon. It renders instruction
// and preference files from the server, links approved skills, drains the
// hook-capture outbox, and refreshes the offline memory cache.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/agentic-substrate/substrate/internal/adapter"
	"github.com/agentic-substrate/substrate/internal/observe"
	"github.com/agentic-substrate/substrate/internal/version"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("adapter exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	server := flag.String("server", "", "control plane base URL; empty prints the version and exits")
	token := flag.String("token", os.Getenv("SUBSTRATE_TOKEN"), "bearer token")
	machine := flag.String("machine", hostname(), "machine name sent as GET /v1/render?machine=")
	home := flag.String("home", "", "directory ~ paths are resolved against; required with -server")
	state := flag.String("state", "", "sqlite path (default <home>/.substrate/adapter.sqlite)")
	roots := flag.String("roots", "", "comma-separated roots scanned two levels deep (<root>/*/*)")
	interval := flag.Duration("interval", adapter.DefaultInterval, "render tick (EDD §7.2)")
	// No default. `global:` was one, and internal/policy requires human_admin
	// for any write at global or org -- so the default silently defeated both
	// empty-scope guards (homeScope, observationScope) and every home-scoped
	// drift proposal and every observation was refused by the server.
	// Unset means "skip the targets that need an explicit scope", which the
	// guards handle and log; it must not mean "use one that cannot work".
	scope := flag.String("scope", "", "scope path for targets with no repo of their own (~/.claude/CLAUDE.md, observations); unset skips them")
	skills := flag.String("skills-repo", os.Getenv("SUBSTRATE_SKILLS_REPO"), "skills git remote; cloned under <home>/.substrate/skills.git")
	otlp := flag.String("otlp", os.Getenv("SUBSTRATE_OTLP_ENDPOINT"), "OTLP HTTP collector endpoint; empty disables export")
	flag.Parse()

	if *server == "" {
		fmt.Printf("substrate-adapter %s (%s)\n", version.Version, version.Revision())
		return nil
	}
	if *home == "" {
		return fmt.Errorf("adapter: -home is required with -server (refuses to guess $HOME)")
	}

	statePath := *state
	if statePath == "" {
		statePath = filepath.Join(*home, ".substrate", "adapter.sqlite")
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	obs, err := observe.Setup(ctx, observe.Config{
		Endpoint: *otlp,
		Service:  "substrate-adapter",
		Instance: *machine,
		Logger:   slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("otlp: %w", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = obs.Shutdown(shutCtx)
	}()

	return adapter.Run(ctx, adapter.Config{
		Server:     *server,
		Token:      *token,
		Machine:    *machine,
		Home:       *home,
		StatePath:  statePath,
		Roots:      splitCSV(*roots),
		Interval:   *interval,
		Scope:      *scope,
		SkillsRepo: *skills,
		Metrics:    obs,
	})
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
