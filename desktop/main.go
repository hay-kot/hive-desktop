package main

import (
	"context"
	"embed"
	"fmt"
	"log"

	"github.com/hay-kot/hive-desktop/internal/adapter/wailsui"
	"github.com/hay-kot/hive-desktop/internal/adapter/wailsui/e2e"
	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	wailsnotify "github.com/wailsapp/wails/v3/pkg/services/notifications"
	"github.com/wailsapp/wails/v3/pkg/updater"
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
	// in System settings applies to a dock-launched app. Must precede
	// StateDir/ConfigPath use below. An explicit env var still wins.
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

	// Cancelled by shutdown rather than deferred: log.Fatal below would skip
	// a defer, and shutdown is the one path both exits take.
	ctx, cancel := context.WithCancel(context.Background())

	// Focus feeds both the frontend's focus-sensitive UI and the notification
	// gate's automatic delivery mode, so it is built before the core that gate
	// belongs to.
	focus := wailsui.NewFocusState()

	// Mock/server builds deliberately do not start the native Wails
	// notification service: e2e verifies preference persistence without an OS
	// bus, banner, or permission prompt. The frontend still gets a descriptive
	// unavailable binding through NotificationService.
	notificationService := wailsui.NewUnavailableNotificationService(fmt.Errorf("native notifications unavailable in desktop mock mode"))
	var nativeNotifications *wailsnotify.NotificationService
	if settings.MockMode() == "" {
		nativeNotifications = wailsnotify.New()
		notifier, err := wailsui.NewNotifier(nativeNotifications, appIcon)
		if err != nil {
			logger.Warn().Err(err).Msg("native notifications unavailable")
			notificationService = wailsui.NewUnavailableNotificationService(err)
		} else {
			notificationService = wailsui.NewNotificationService(notifier)
		}
	}

	core, err := app.New(ctx, app.Config{
		Settings: cfg,
		MockMode: settings.MockMode(),
		Logger:   logger,
		Notifier: wailsui.NewFlowNotifier(notificationService),
		Gate:     wailsui.NewNotificationGate(focus, logger),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Mock mode has no live producer, so seed deterministic inbox rows for the
	// fixture flow in desktop/e2e/fixtures/flows/frontend-triage.yaml.
	if settings.MockMode() == "feed" || settings.MockMode() == "action-smoke" {
		e2e.SeedMockInboxItemsOrWarn(core.Store, logger)
	}

	var refreshProfileTray func()
	cancelEvents := wailsui.Subscribe(ctx, core.Events, func() {
		if refreshProfileTray != nil {
			refreshProfileTray()
		}
	})

	// The updater service is created before the app (it goes in the Services
	// slice) but its engine (app.Updater) only exists after application.New, so
	// the live Updater is attached below. Auto-update defaults on; the persisted
	// toggle seeds the initial state.
	updaterVersion, _, _ := resolvedBuildInfo()
	updaterService := wailsui.NewUpdaterService(updaterVersion, cfg.AutoUpdateOrDefault(), wailsui.DefaultUpdateCheckInterval, logger)

	services := []application.Service{
		application.NewService(wailsui.NewAuthService(core.AuthBackend)),
		application.NewService(wailsui.NewPipelineService(core.Store, core.ActionStore, core.Outputs, core.Launcher)),
		application.NewService(wailsui.NewFlowsService(core.FlowStore, core.Store, func() { core.PublishFlowsUpdated("save") })),
		application.NewService(wailsui.NewActionsService(core.ActionStore, wailsui.EmitActionsUpdated)),
		application.NewService(wailsui.NewActivityService(core.ActivityStore)),
		application.NewService(wailsui.NewJobService(core.JobStore)),
		application.NewService(wailsui.NewSystemService(resolvedBuildInfo())),
		application.NewService(wailsui.NewSettingsService(core.Producer, core.Fetcher, logger)),
		application.NewService(wailsui.NewWebhookService(core.Store, core.Webhook, core.WebhookPort)),
		application.NewService(wailsui.NewPromptsService(core.Webhook, core.WebhookPort)),
		application.NewService(updaterService),
	}
	if nativeNotifications != nil {
		services = append(services, application.NewService(nativeNotifications))
	}
	services = append(services,
		application.NewService(notificationService),
		application.NewService(wailsui.NewWindowService(focus)),
	)

	// Test-only /_e2e/reset harness (nil outside the Docker e2e mock modes).
	// Built this late deliberately: app.New has seeded actions.yml and mock
	// seeding has run, so the captured config baseline is the post-boot state
	// a reset must restore.
	resetHarness := e2e.NewStateResetHarness(core.Store, core.HiveDB, logger)

	options := application.Options{
		Name:        "Hive",
		Description: "Hive desktop application",
		Icon:        appIcon,
		Services:    services,
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: e2e.SmokeMiddleware(core.Store, core.HiveDB, resetHarness),
		},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyRegular,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	}
	wailsApp := application.New(options)

	// Configure self-update only for published release builds: ReleaseChannel
	// rejects source builds ("dev") and pseudo-versions, so the engine stays
	// nil there and the service degrades to Available:false with a no-op
	// ticker. A published build follows its own channel (a beta build tracks
	// beta, per docs/decisions/0004) unless settings.yaml's update_channel
	// overrides it.
	if channel, ok := wailsui.ReleaseChannel(updaterVersion); ok {
		provider := wailsui.NewManifestProvider(wailsui.DefaultManifestBaseURL, cfg.UpdateChannelOrDefault(channel))
		if initErr := wailsApp.Updater.Init(updater.Config{
			CurrentVersion: updaterVersion,
			Providers:      []updater.Provider{provider},
		}); initErr != nil {
			logger.Warn().Err(initErr).Msg("desktop auto-update unavailable; updater init failed")
		} else {
			updaterService.Attach(wailsApp.Updater)
		}
	}

	window := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Hive",
		Width:            1360,
		Height:           864,
		BackgroundColour: application.NewRGB(24, 26, 31),
		URL:              "/",
		Mac: application.MacWindow{
			// HiddenInset with an explicit compact toolbar style: the default
			// (Automatic) lets AppKit pick the toolbar height, which drifts
			// across macOS versions. UnifiedCompact pins it — 42pt as measured
			// on macOS Tahoe — so the traffic lights center on the 42px HTML
			// titlebar.
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				HideTitle:            true,
				FullSizeContent:      true,
				UseToolbar:           true,
				HideToolbarSeparator: true,
				ToolbarStyle:         application.MacToolbarStyleUnifiedCompact,
			},
			InvisibleTitleBarHeight: 42,
		},
	})

	// This is the sole owner of notification activation: a click brings the
	// existing window forward, and — for a notification a flow's notify node
	// sent — publishes which item it came from. Routing to that item stays in
	// the frontend, and out of the notification binding.
	if nativeNotifications != nil {
		nativeNotifications.OnNotificationResponse(func(result wailsnotify.NotificationResult) {
			window.Show()
			window.Focus()
			if activation, ok := wailsui.NotificationActivationFrom(result); ok {
				wailsui.EmitNotificationActivated(activation)
			}
		})
	}

	window.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
		if focus.Set(true) {
			wailsui.EmitWindowFocus()
		}
	})
	window.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		if focus.Set(false) {
			wailsui.EmitWindowBlur()
		}
	})

	// Closing the window keeps the app running in the dock and tray; it can be
	// reopened from either. Quitting is done via Cmd+Q or the tray menu.
	// This must be a hook, not OnWindowEvent: hooks run synchronously before
	// listeners, so Cancel() reliably aborts Wails' own window-destroy listener,
	// which otherwise races this callback in a separate goroutine.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		if focus.Set(false) {
			wailsui.EmitWindowBlur()
		}
		e.Cancel()
	})

	wailsApp.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		window.Show()
	})

	profilesTray := wailsui.NewProfileTray(
		wailsApp,
		core.FlowStore,
		logger,
		trayIcon,
		func() { core.PublishFlowsUpdated("tray") },
		func() {
			window.Show()
			window.Focus()
		},
		wailsApp.Quit,
	)
	refreshProfileTray = profilesTray.Refresh
	wailsApp.OnShutdown(profilesTray.Close)

	// Background work starts only after refreshProfileTray is published: the
	// flows watcher invokes the flows subscriber from its own goroutine, so
	// starting earlier would race the assignment above.
	if err := core.Start(ctx); err != nil {
		log.Fatal(err)
	}

	shutdown := func() {
		updaterService.Stop()
		cancelEvents()
		cancel()
		if err := core.Close(); err != nil {
			logger.Warn().Err(err).Msg("core shutdown reported an error")
		}
		logCloser()
	}
	if err := wailsApp.Run(); err != nil {
		shutdown()
		log.Fatal(err)
	}
	shutdown()
}
