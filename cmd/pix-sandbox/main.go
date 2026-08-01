// Command pix-sandbox runs the Pix emulator: a single, zero-config binary
// serving the BACEN-compatible API Pix surface over an embedded SQLite store.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/arinelliquebec/pix-sandbox/internal/api"
	"github.com/arinelliquebec/pix-sandbox/internal/rng"
	"github.com/arinelliquebec/pix-sandbox/internal/store"
)

// version is injected at build time via -ldflags.
var version = "dev"

type config struct {
	addr   string
	dbPath string
	seed   uint64
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	st, err := store.Open(cfg.dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	src := rng.New(cfg.seed)

	// The seed is printed on every boot: any run of the sandbox is
	// reproducible by starting it again with the same value (ADR-007).
	log.Info("pix-sandbox starting",
		"version", version,
		"addr", cfg.addr,
		"db", cfg.dbPath,
		"seed", src.Seed(),
	)

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           api.New(st, src, log).Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func loadConfig() (config, error) {
	cfg := config{
		addr:   envOr("PIX_SANDBOX_ADDR", ":8080"),
		dbPath: envOr("PIX_SANDBOX_DB", "./data/sandbox.db"),
		seed:   rng.DefaultSeed,
	}
	if raw := os.Getenv("PIX_SANDBOX_SEED"); raw != "" {
		seed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return cfg, errors.New("PIX_SANDBOX_SEED must be an unsigned integer")
		}
		cfg.seed = seed
	}

	flag.StringVar(&cfg.addr, "addr", cfg.addr, "listen address")
	flag.StringVar(&cfg.dbPath, "db", cfg.dbPath, "path to the SQLite database file")
	flag.Uint64Var(&cfg.seed, "seed", cfg.seed, "seed for the deterministic random source")
	flag.Parse()

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
