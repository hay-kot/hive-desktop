// Command hive-desktop is the desktop app's entrypoint. It does four things:
// apply the bootstrap overrides, build the headless core, mount the Wails
// adapter over it, and run. Everything else belongs to internal/app (what the
// app does) or internal/adapter/wailsui (how a window talks to it).
package main

import (
	"context"
	"embed"
	"log"

	"github.com/hay-kot/hive-desktop/internal/adapter/httpapi"
	"github.com/hay-kot/hive-desktop/internal/adapter/wailsui"
	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

// Wails accepts a single PNG for template icons; embed the retina asset.
//
//go:embed build/icons/tray-templateTemplate@2x.png
var trayIcon []byte

func main() {
	bootstrap, err := settings.LoadBootstrap()
	if err != nil {
		log.Fatal(err)
	}
	paths := settings.ResolvePaths(bootstrap, "")
	settingsStore := settings.NewStore(paths.SettingsPath)
	cfg, err := settingsStore.Effective()
	if err != nil {
		log.Fatal(err)
	}
	// Mock mode can select an isolated flows directory, so finalize the path
	// snapshot only after settings and environment precedence are resolved.
	paths = settings.ResolvePaths(bootstrap, cfg.MockMode())
	settingsStore = settings.NewStore(paths.SettingsPath)
	level, err := settings.ResolveLogLevel()
	if err != nil {
		log.Fatal(err)
	}
	logger, logCloser, logErr := settings.NewLogger(paths.LogFile, level)
	if logErr != nil {
		logger.Warn().Err(logErr).Msg("desktop log file unavailable; logging to stderr only")
	}

	// A redirected API base means every item this run shows may be stale or
	// deliberately rewritten by cmd/devserver. That is invisible in the UI, so
	// it is worth a line in the log before anything fetches.
	if base := cfg.GitHubAPIBase(); base != "" {
		logger.Warn().
			Str("api_base", base).
			Bool("from_env", cfg.EnvironmentOverridden(settings.EnvGitHubAPIBase)).
			Msg("GitHub API base overridden; not talking to api.github.com")
	}

	// Cancelled by shutdown rather than deferred: log.Fatal below would skip a
	// defer, and shutdown is the one path both exits take.
	ctx, cancel := context.WithCancel(context.Background())

	// The adapter is built first because the core takes two driven ports from
	// it — where a notification is delivered, and whether it may be.
	ui := wailsui.New(cfg.MockMode(), settingsStore, appIcon, logger)

	core, err := app.New(ctx, app.Config{
		Settings:      cfg,
		SettingsStore: settingsStore,
		Paths:         paths,
		MockMode:      cfg.MockMode(),
		Logger:        logger,
		Notifier:      ui.Notifier(),
		Gate:          ui.Gate(),
	})
	if err != nil {
		log.Fatal(err)
	}
	ui.SeedMock(core)

	// The agent HTTP API shares the loopback HTTP server with the webhook
	// listener (ADR 0021); mount it before Start whenever that server is up.
	if core.MountAPI(httpapi.PathPrefix, httpapi.New(core, logger).Handler()) {
		logger.Info().Msg("agent HTTP API mounted at /api/")
	}
	// pprof rides the same server when development.pprof is enabled (ADR 0023);
	// off by default, so nothing is mounted unless asked for.
	if cfg.Development.Pprof.Enabled && core.MountAPI(httpapi.PprofPathPrefix, httpapi.PprofHandler()) {
		logger.Info().Str("path", httpapi.PprofPathPrefix).Msg("pprof debug endpoint mounted")
	}

	version, commit, date := resolvedBuildInfo()
	ui.Mount(ctx, core, wailsui.MountOptions{
		Assets:     assets,
		AppIcon:    appIcon,
		TrayIcon:   trayIcon,
		Build:      wailsui.Build{Version: version, Commit: commit, Date: date},
		AutoUpdate: cfg.Updates.Enabled,
		UpdateChannel: func(buildChannel string) string {
			if cfg.Updates.Channel == "" {
				return buildChannel
			}
			return cfg.Updates.Channel
		},
	})

	// Background work starts after the adapter is mounted: the flows watcher
	// calls event subscribers from its own goroutine, and the tray subscriber
	// has to exist before it can fire.
	if err := core.Start(ctx); err != nil {
		log.Fatal(err)
	}

	shutdown := func() {
		ui.Close()
		cancel()
		if err := core.Close(); err != nil {
			logger.Warn().Err(err).Msg("core shutdown reported an error")
		}
		logCloser()
	}
	if err := ui.Run(); err != nil {
		shutdown()
		log.Fatal(err)
	}
	shutdown()
}
