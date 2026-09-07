// Command substrate-server is the control plane: MCP tools at /mcp, the
// adapter/CLI REST surface at /v1, and in-process scheduled jobs.
//
// /readyz is Postgres-only (EDD R27): a Git or skills-repo outage must never
// take the service out of rotation. The process stays up when Postgres is down;
// only /readyz goes 503. /healthz is 200 whenever the process is alive.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/agentic-substrate/substrate/internal/compiler"
	"github.com/agentic-substrate/substrate/internal/embed"
	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/mcpx"
	"github.com/agentic-substrate/substrate/internal/memory"
	"github.com/agentic-substrate/substrate/internal/observe"
	"github.com/agentic-substrate/substrate/internal/rest"
	"github.com/agentic-substrate/substrate/internal/store"
	"github.com/agentic-substrate/substrate/internal/version"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "listen address")
	dsn := flag.String("dsn", os.Getenv("SUBSTRATE_DSN"), "postgres DSN; migrations run under an advisory lock")
	ollama := flag.String("ollama", os.Getenv("SUBSTRATE_OLLAMA_URL"), "Ollama base URL for embeddings; empty means keyword-only retrieval")
	skills := flag.String("skills-repo", os.Getenv("SUBSTRATE_SKILLS_REPO"), "skills git remote; reachability is /v1/health/git, never /readyz")
	otlp := flag.String("otlp", os.Getenv("SUBSTRATE_OTLP_ENDPOINT"), "OTLP HTTP collector endpoint; empty disables export")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	// A nil embedder is a supported configuration, not a degraded one: search
	// must never fail because embeddings are unavailable (MEM-5). Constructing
	// it here rather than inside the handler keeps the "unwired in production
	// while every test passes" failure impossible to reach by accident.
	var embedder memory.Embedder
	if *ollama != "" {
		embedder = embed.New(*ollama)
		slog.Info("embeddings enabled", "model", embed.Model, "dim", embed.Dim)
	} else {
		slog.Info("embeddings disabled; retrieval is keyword-only")
	}
	obs, err := observe.Setup(ctx, observe.Config{
		Endpoint: *otlp,
		Service:  "substrate-server",
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
	if *otlp == "" {
		slog.Info("otlp export disabled")
	} else {
		slog.Info("otlp export enabled")
	}
	return serve(ctx, ln, *dsn, embedder, rest.SkillsRepo{URL: *skills}, obs)
}

type runtime struct {
	store atomic.Pointer[store.Store]
}

func serve(ctx context.Context, ln net.Listener, dsn string, embedder memory.Embedder, git rest.GitChecker, obs *observe.Runtime) error {
	rt := &runtime{}
	srv := &http.Server{
		Handler:           newHandler(rt.getStore, embedder, git, obs),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if dsn != "" {
		go connectLoop(ctx, dsn, rt)
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", ln.Addr().String(), "version", version.Version)
		errc <- srv.Serve(ln)
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		if st := rt.getStore(); st != nil {
			st.Close()
		}
		return err
	}
}

func (rt *runtime) getStore() *store.Store {
	if rt == nil {
		return nil
	}
	return rt.store.Load()
}

func connectLoop(ctx context.Context, dsn string, rt *runtime) {
	backoff := time.Second
	for {
		st, err := store.Open(ctx, dsn)
		if err == nil {
			if old := rt.store.Swap(st); old != nil {
				old.Close()
			}
			slog.Info("store ready")
			return
		}
		slog.Error("store unavailable; retrying", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
	}
}

func newHandler(getStore func() *store.Store, embedder memory.Embedder, git rest.GitChecker, obs *observe.Runtime) http.Handler {
	if getStore == nil {
		getStore = func() *store.Store { return nil }
	}
	return newHandlerLookup(getStore, embedder, git, func(ctx context.Context, tok string) (*identity.Principal, error) {
		st := getStore()
		if st == nil {
			return nil, identity.ErrUnauthorized
		}
		return identity.Lookup(ctx, st.Pool(), tok)
	}, obs)
}

func newHandlerLookup(getStore func() *store.Store, embedder memory.Embedder, git rest.GitChecker, lookup identity.LookupFunc, obs *observe.Runtime) http.Handler {
	if getStore == nil {
		getStore = func() *store.Store { return nil }
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":   "ok",
			"version":  version.Version,
			"revision": version.Revision(),
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		err := getStore().Ping(r.Context())
		if obs != nil {
			obs.SetReady(r.Context(), err == nil)
		}
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready",
				"reason": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mcpSrv := mcpx.New("substrate", version.Version)
	memSvc := memory.Register(mcpSrv, getStore, embedder)
	compiler.Register(mcpSrv, getStore, memSvc)
	mux.Handle("/mcp", mcpSrv.Handler())
	rest.New(rest.Options{Store: getStore, Memory: memSvc, Git: git, Observe: obs}).Mount(mux)
	return observe.Middleware(obs)(identity.Middleware(lookup)(mux))
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		fmt.Fprintln(os.Stderr, "write response:", err)
	}
}
