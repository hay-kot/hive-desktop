// Command hive-desktop is the desktop app's entrypoint. It does four things:
// apply the bootstrap overrides, build the headless core, mount the Wails
// adapter over it, and run. Everything else belongs to internal/app (what the
// app does) or internal/adapter/wailsui (how a window talks to it).
package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/hay-kot/hive-desktop/internal/adapter/httpapi"
	"github.com/hay-kot/hive-desktop/internal/adapter/wailsui"
	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/configmigrate"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/report"
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

// The same mark rendered white: Linux panels take raw pixmaps, not tintable
// templates (see wailsui.applyTrayIcon).
//
//go:embed build/linux/tray-icon.png
var trayIconLinux []byte

func main() {
	bootstrap, err := settings.LoadBootstrap()
	if err != nil {
		log.Fatal(err)
	}
	paths := settings.ResolvePaths(bootstrap, "")
	level, err := settings.ResolveLogLevel()
	if err != nil {
		log.Fatal(err)
	}
	logger, logCloser, logErr := settings.NewLogger(paths.LogFile, level)
	if logErr != nil {
		logger.Warn().Err(logErr).Msg("desktop log file unavailable; logging to stderr only")
	}

	backupDir := filepath.Join(paths.StateDir, "migration-backups")
	if _, _, err := configmigrate.MigrateFile(configmigrate.SettingsSet, paths.SettingsPath, backupDir, &logger); err != nil {
		log.Fatal(err) // preserve settings' fail-startup semantics
	}

	settingsStore := settings.NewStore(paths.SettingsPath)
	cfg, err := settingsStore.Reload()
	if err != nil {
		log.Fatal(err)
	}
	// Mock mode can select an isolated flows directory, so finalize the path
	// snapshot only after settings and environment precedence are resolved.
	paths = settings.ResolvePaths(bootstrap, cfg.MockMode())

	// Migrate flows/*.yaml and actions.yml in place before app.New constructs the
	// stores and starts the watchers. Non-fatal: mirror each type's last-good
	// semantics rather than failing startup.
	if err := flow.MigrateDir(paths.FlowsDir, backupDir, &logger); err != nil {
		logger.Warn().Err(err).Msg("flow migration sweep failed")
	}
	if _, _, err := configmigrate.MigrateFile(configmigrate.ActionsSet, paths.ActionsPath, backupDir, &logger); err != nil {
		logger.Warn().Err(err).Msg("actions.yml migration failed; using last-good")
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

	version, commit, date := resolvedBuildInfo()
	core, err := app.New(ctx, app.Config{
		SettingsStore:  settingsStore,
		Paths:          paths,
		MockMode:       cfg.MockMode(),
		Logger:         logger,
		Notifier:       ui.Notifier(),
		Gate:           ui.Gate(),
		Build:          report.Build{Version: version, Commit: commit, Date: date},
		ReportUploader: ui.ReportUploader(),
	})
	if err != nil {
		log.Fatal(err)
	}
	ui.SeedMock(core)

	// The terminal surface is the one part of the API that authenticates, so its
	// token and CORS allowlist are minted here and handed to the two adapters
	// that need them — the core carries neither (ADR 0036). Terminal mode ships
	// dark behind experimental.terminal (ADR 0037): when off, no token is minted
	// and neither terminal surface — the control-plane routes or the stream
	// mount — exists on the loopback server.
	terminalToken := ""
	var origins []string
	if cfg.Experimental.Terminal {
		terminalToken, err = httpapi.MintTerminalToken()
		if err != nil {
			log.Fatal(err)
		}
		origins = webviewOrigins()
	}

	// The agent HTTP API shares the loopback HTTP server with the webhook
	// listener (ADR 0021); mount it before Start whenever that server is up.
	if core.MountAPI(httpapi.PathPrefix, httpapi.New(core, logger, terminalToken, origins).Handler()) {
		logger.Info().Msg("agent HTTP API mounted at /api/")
	}
	terminal := wailsui.TerminalTransport{}
	if terminalToken != "" {
		if path, handler := httpapi.TerminalStreamHandler(core, terminalToken, origins, logger); core.MountAPI(path, handler) {
			terminal = wailsui.TerminalTransport{Token: terminalToken, StreamPath: path}
			logger.Info().Str("path", path).Msg("terminal WebSocket stream mounted")
		}
	}
	// pprof shares the same server when enabled (ADR 0023).
	if cfg.Development.Pprof.Enabled && core.MountAPI(httpapi.PprofPathPrefix, httpapi.PprofHandler()) {
		logger.Info().Str("path", httpapi.PprofPathPrefix).Msg("pprof debug endpoint mounted")
	}

	ui.Mount(ctx, core, wailsui.MountOptions{
		Assets:          assets,
		AppIcon:         appIcon,
		TrayIcon:        trayIcon,
		TrayIconLinux:   trayIconLinux,
		Build:           wailsui.Build{Version: version, Commit: commit, Date: date},
		Terminal:        terminal,
		TerminalEnabled: cfg.Experimental.Terminal,
		AutoUpdate:      cfg.Updates.Enabled,
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

// webviewOrigins is the CORS allowlist for the terminal surface: the packaged
// webview's own origin, plus the dev servers when they are running. In dev the
// webview may load from the Vite server or from the Wails dev server that
// proxies it, and Wails builds its URLs with localhost while the servers bind
// 127.0.0.1 — so both hosts are listed for both ports. All are loopback; the
// bearer token is the actual gate.
func webviewOrigins() []string {
	origins := []string{"wails://localhost"}
	for _, portKey := range []string{"WAILS_VITE_PORT", "WAILS_SERVER_PORT"} {
		port := os.Getenv(portKey)
		if port == "" {
			continue
		}
		for _, host := range []string{"localhost", "127.0.0.1"} {
			origins = append(origins, "http://"+host+":"+port)
		}
	}
	return origins
}
