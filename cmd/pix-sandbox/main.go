// Command pix-sandbox runs the Pix emulator: a single, zero-config binary
// serving the BACEN-compatible API Pix surface over an embedded SQLite store.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/arinellidu/pix-sandbox-pay/internal/api"
	"github.com/arinellidu/pix-sandbox-pay/internal/buildinfo"
	"github.com/arinellidu/pix-sandbox-pay/internal/emv"
	"github.com/arinellidu/pix-sandbox-pay/internal/rng"
	"github.com/arinellidu/pix-sandbox-pay/internal/store"
	"github.com/arinellidu/pix-sandbox-pay/internal/webhook"
	"github.com/arinellidu/pix-sandbox-pay/web/console"
)

// version is injected at build time via -ldflags "-X main.version=$TAG".
//
// It stays empty by default on purpose: buildinfo treats an empty value as
// "the linker said nothing" and falls through to what the module system and
// the VCS stamp know. A default of "dev" here would win that cascade and
// every `go install` build would misreport itself.
var version string

type config struct {
	addr         string
	dbPath       string
	seed         uint64
	baseURL      string
	merchantName string
	merchantCity string
	showVersion  bool
	// webhookSecret comes from the environment only: a secret never belongs
	// on a command line, where every process on the box can read it.
	webhookSecret string
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

	build := buildinfo.Resolve(version)
	if cfg.showVersion {
		fmt.Println(build)
		return nil
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
		"version", build.Version,
		"addr", cfg.addr,
		"db", cfg.dbPath,
		"seed", src.Seed(),
	)

	ui := console.New(st, console.Config{Version: build.Version, Seed: src.Seed(), Log: log})

	apiServer := api.New(st, src, log, api.Config{
		Version:      build.Version,
		BaseURL:      cfg.baseURL,
		MerchantName: cfg.merchantName,
		MerchantCity: cfg.merchantCity,
		Webhook:      webhook.Config{Secret: cfg.webhookSecret},
		Console:      ui.Router(),
	})

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           apiServer.Router(),
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
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	// Webhooks outlive the request that triggered them, so they are drained
	// after the listener closes rather than with it.
	return apiServer.Close(shutdownCtx)
}

func loadConfig() (config, error) {
	cfg := config{
		addr:          envOr("PIX_SANDBOX_ADDR", ":8080"),
		dbPath:        envOr("PIX_SANDBOX_DB", "./data/sandbox.db"),
		seed:          rng.DefaultSeed,
		baseURL:       envOr("PIX_SANDBOX_BASE_URL", api.DefaultBaseURL),
		merchantName:  envOr("PIX_SANDBOX_MERCHANT_NAME", emv.DefaultMerchantName),
		merchantCity:  envOr("PIX_SANDBOX_MERCHANT_CITY", emv.DefaultMerchantCity),
		webhookSecret: envOr("WEBHOOK_SECRET", webhook.DefaultSecret),
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
	flag.StringVar(&cfg.baseURL, "base-url", cfg.baseURL, "scheme-less host used to build charge locations")
	flag.StringVar(&cfg.merchantName, "merchant-name", cfg.merchantName, "merchant name in the BR Code (field 59)")
	flag.StringVar(&cfg.merchantCity, "merchant-city", cfg.merchantCity, "merchant city in the BR Code (field 60)")
	flag.BoolVar(&cfg.showVersion, "version", false, "print the build and exit")
	flag.Parse()

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
