// Command hive-desktop is the desktop app's entrypoint. It does four things:
// apply the bootstrap overrides, build the headless core, mount the Wails
// adapter over it, and run. Everything else belongs to internal/app (what the
// app does) or internal/adapter/wailsui (how a window talks to it).
package main

import (
	"context"
	"embed"
	"log"

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
	// Seed HIVE_DATA_DIR / HIVE_DESKTOP_CONFIG from the bootstrap pointer file
	// before any path is resolved, so a data/config directory override chosen
	// in System settings applies to a dock-launched app. Must precede every
	// path resolution below. An explicit env var still wins.
	bootstrapErr := settings.ApplyBootstrap()

	logger, logCloser, logErr := settings.NewLogger()
	if logErr != nil {
		logger.Warn().Err(logErr).Msg("desktop log file unavailable; logging to stderr only")
	}
	if bootstrapErr != nil {
		logger.Warn().Err(bootstrapErr).Msg("desktop bootstrap overrides ignored")
	}

	cfg, err := settings.LoadSettings()
	if err != nil {
		logger.Warn().Err(err).Msg("desktop settings load failed; using defaults")
	}

	// Cancelled by shutdown rather than deferred: log.Fatal below would skip a
	// defer, and shutdown is the one path both exits take.
	ctx, cancel := context.WithCancel(context.Background())

	// The adapter is built first because the core takes two driven ports from
	// it — where a notification is delivered, and whether it may be.
	ui := wailsui.New(settings.MockMode(), appIcon, logger)

	core, err := app.New(ctx, app.Config{
		Settings: cfg,
		MockMode: settings.MockMode(),
		Logger:   logger,
		Notifier: ui.Notifier(),
		Gate:     ui.Gate(),
	})
	if err != nil {
		log.Fatal(err)
	}
	ui.SeedMock(core)

	version, commit, date := resolvedBuildInfo()
	ui.Mount(ctx, core, wailsui.MountOptions{
		Assets:        assets,
		AppIcon:       appIcon,
		TrayIcon:      trayIcon,
		Build:         wailsui.Build{Version: version, Commit: commit, Date: date},
		AutoUpdate:    cfg.AutoUpdateOrDefault(),
		UpdateChannel: cfg.UpdateChannelOrDefault,
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
