// Command devserver fronts the GitHub API for local development: it caches
// responses so several desktop instances share one rate-limit budget, rewrites
// them from a config-driven overlay so workflows can be simulated against real
// data, and pushes webhook payloads at a running instance's local listener.
//
// It is development tooling. It binds loopback only, holds no credentials of
// its own, and nothing in the shipped app depends on it.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v3"
)

//go:embed dashboard.html
var dashboardHTML []byte

// shutdownTimeout bounds graceful shutdown; in-flight proxy calls are bounded
// by upstreamTimeout, so anything longer is hung.
const shutdownTimeout = 5 * time.Second

func main() {
	command := newDevserverCommand()
	if err := command.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "devserver:", err)
		os.Exit(1)
	}
}

func newDevserverCommand() *cli.Command {
	return &cli.Command{
		Name:  "devserver",
		Usage: "cache and simulate the GitHub API for desktop development",
		Description: "Runs a loopback proxy in front of the GitHub API. Responses are cached in SQLite and " +
			"shared across every desktop instance pointed at it, so N dev instances and their restarts cost " +
			"one upstream request per unique call per TTL. Config-driven overlays rewrite those responses to " +
			"simulate lifecycle events, and a webhook pusher delivers synthetic payloads to a running " +
			"instance's local webhook listener. A dashboard at the listen address drives both.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "path to devserver.yaml (default: $XDG_CONFIG_HOME/hive/desktop/devserver.yaml)",
			},
			&cli.StringFlag{Name: "listen", Usage: "override the configured bind address"},
			&cli.StringFlag{Name: "upstream", Usage: "override the configured GitHub API base URL"},
			&cli.DurationFlag{Name: "ttl", Usage: "override the configured cache TTL"},
			&cli.StringFlag{Name: "log-level", Value: "info", Usage: "trace, debug, info, warn, or error"},
		},
		Action: run,
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	configPath := cmd.String("config")
	if configPath == "" {
		configPath = DefaultConfigPath()
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	if listen := cmd.String("listen"); listen != "" {
		cfg.Listen = listen
	}
	if upstream := cmd.String("upstream"); upstream != "" {
		cfg.Upstream = upstream
	}
	if ttl := cmd.Duration("ttl"); ttl > 0 {
		cfg.Cache.TTL = ttl
	}

	level, err := zerolog.ParseLevel(cmd.String("log-level"))
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", cmd.String("log-level"), err)
	}
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly}).
		Level(level).With().Timestamp().Logger()

	cache, err := OpenCache(cfg.Cache.Path, cfg.Cache.TTL)
	if err != nil {
		return err
	}
	defer cache.Close() //nolint:errcheck // best-effort close on shutdown

	store := NewStore(cfg.Overlays)
	proxy := NewProxy(cfg.Upstream, cache, store, logger)
	pusher := NewPusher(cfg.Webhooks, logger)
	control := NewControl(cfg, store, cache, proxy, pusher, logger)

	mux := http.NewServeMux()
	// {$} matches the root path exactly, leaving every other path to the
	// proxy — GitHub's own API lives under paths the desktop actually calls.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(dashboardHTML); err != nil {
			logger.Debug().Err(err).Msg("writing dashboard")
		}
	})
	mux.Handle("/_ctl/", control.Handler())
	mux.Handle("/", proxy)

	server := &http.Server{Addr: cfg.Listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	logger.Info().
		Str("listen", cfg.Listen).
		Str("upstream", cfg.Upstream).
		Str("cache", cfg.Cache.Path).
		Dur("ttl", cfg.Cache.TTL).
		Int("overlays", len(cfg.Overlays)).
		Int("scenarios", len(cfg.Scenarios)).
		Int("targets", len(cfg.Webhooks.Targets)).
		Msg("devserver started")
	logger.Info().Msgf("dashboard: http://%s", cfg.Listen)
	logger.Info().Msgf("point a desktop instance at it: HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE=http://%s mise run desktop:dev", cfg.Listen)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return fmt.Errorf("devserver: %w", err)
	case <-ctx.Done():
		logger.Info().Msg("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
