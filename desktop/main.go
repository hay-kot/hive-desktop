package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/colonyops/hive/pkg/executil"
	"github.com/colonyops/hive/pkg/tmpl"
	"github.com/hay-kot/hive-desktop/internal/adapter/wailsui"
	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/jobs"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/app/sources/github/feed"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/desktop/auth"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/config"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/eventbus"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/git"
	coredb "github.com/hay-kot/hive-desktop/internal/hivecore/data/db"
	"github.com/hay-kot/hive-desktop/internal/hivecore/data/stores"
	"github.com/hay-kot/hive-desktop/internal/hivecore/github"
	"github.com/hay-kot/hive-desktop/internal/hivecore/hive"
	"github.com/hay-kot/hive-desktop/internal/hivecore/hive/scripts"
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

// Package-variable initialization instead of init(): this repo enables
// gochecknoinits.
var _ = registerEvents()

func registerEvents() struct{} {
	// auth:updated carries the new auth state string; log:appended carries the
	// pipeline event log's new tail offset after a producer tick appends at
	// least one row; flows:updated fires after a flows/*.yaml directory reload
	// (an external edit, or the app's own SaveFlow/SaveLayout — see
	// buildFlowsStore); actions:updated fires after an actions.yml reload.
	// All are wake-up signals: the frontend re-reads the relevant service on
	// receipt.
	application.RegisterEvent[string]("auth:updated")
	application.RegisterEvent[int64]("log:appended")
	application.RegisterEvent[string]("flows:updated")
	application.RegisterEvent[string]("actions:updated")
	application.RegisterEvent[string]("jobs:updated")
	// window:focus and window:blur carry the current focus state. Consumers use
	// them to update focus-sensitive UI without querying the native window.
	application.RegisterEvent[bool]("window:focus")
	application.RegisterEvent[bool]("window:blur")
	// activity:appended carries the new event's id after any subsystem (or the
	// frontend, via wailsui.ActivityService.Record) appends to the activity log. The
	// Activity view re-reads its latest page and advances its unseen marker.
	application.RegisterEvent[int64]("activity:appended")
	// update:available carries the latest UpdateInfo when a self-update check
	// finds a newer desktop release; update:none fires when the check confirms
	// the app is current. The title bar reacts to update:available.
	application.RegisterEvent[UpdateInfo]("update:available")
	application.RegisterEvent[UpdateInfo]("update:none")
	// notification:activated carries the workspace and inbox item behind a
	// native notification the user clicked. Unlike the wake-up signals above
	// its payload is the whole message: the window is already being raised by
	// the time it fires, and the frontend's job is only to route to that item.
	application.RegisterEvent[wailsui.NotificationActivation]("notification:activated")
	// notification:toast carries a flow notification the user chose to receive
	// inside Hive rather than as an OS banner (Settings -> Notifications ->
	// Delivery). The frontend surfaces it through the same toast stack every
	// other in-app notification uses.
	application.RegisterEvent[wailsui.NotificationToast]("notification:toast")
	return struct{}{}
}

// buildSourceFetcher builds the GitHub fetch layer the pipeline producer polls
// through, or nil in a mock mode (where the producer is skipped anyway — see
// buildPipelineProducer). Now that a profile is a flow, there is no profiles
// config to load or hot-reload here: source config lives in the flow's
// github-source nodes, and the producer enumerates them from the flow store.
func buildSourceFetcher(logger zerolog.Logger) *feed.LiveProvider {
	if settings.MockMode() != "" {
		return nil
	}
	return feed.NewLiveProvider(github.NewClient(), github.NewKeychainStore(), logger)
}

// buildPipelineProducer starts the pipeline event-log producer over every
// enabled github-source node across all flows (via flows), or returns nil when
// there is nothing to poll (mock mode, so fetcher is nil).
func buildPipelineProducer(db *store.DB, fetcher *feed.LiveProvider, flows ingest.FlowLister, recorder activity.Recorder, interval time.Duration, logger zerolog.Logger) *ingest.Producer {
	if fetcher == nil {
		return nil
	}
	producer := ingest.NewProducer(db, ghsource.NewFlowSourceLister(fetcher, flows), interval, emitLogAppended, logger)
	producer.SetRecorder(recorder)
	producer.SetPrefetcher(fetcher)
	producer.SetSourceAdapter(ghsource.NewGithubSourceAdapter(fetcher))
	return producer
}

// emitLogAppended pushes the pipeline event log's new tail offset to the
// frontend after a producer tick appends at least one row. Safe to call
// from the producer goroutine once the app is running.
func emitLogAppended(nextOffset int64) {
	if app := application.Get(); app != nil {
		app.Event.Emit("log:appended", nextOffset)
	}
}

func buildAuthBackend(onChange func()) auth.Backend {
	switch settings.MockMode() {
	case "feed", "pipeline", "action-smoke":
		return auth.NewMockBackend(true, onChange)
	case "onboarding":
		return auth.NewMockBackend(false, onChange)
	default:
		return auth.NewLiveBackend(github.NewClient(), github.NewKeychainStore(), onChange)
	}
}

// emitActivityAppended pushes the activity:appended wake-up (carrying the new
// event's id) to the frontend after any subsystem records an activity event.
// Safe to call from any goroutine once the app is running.
func emitActivityAppended(id int64) {
	if app := application.Get(); app != nil {
		app.Event.Emit("activity:appended", id)
	}
}

// emitJobsUpdated wakes frontend consumers after any successful job lifecycle
// transition. The payload is intentionally only a wake-up signal.
func emitJobsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("jobs:updated", "changed")
	}
}

// emitNotificationActivated tells the frontend which item a clicked
// notification came from, so it can route to it.
func emitNotificationActivated(activation wailsui.NotificationActivation) {
	if app := application.Get(); app != nil {
		app.Event.Emit("notification:activated", activation)
	}
}

// notificationActivation reads the item a clicked notification was sent
// about out of the payload the notify executor attached to it. The user info
// makes a native round trip, so its numbers come back in whatever shape the
// platform's serialization chose — hence the tolerant decode. App-level
// notifications carry no such payload and report false.
func notificationActivation(result wailsnotify.NotificationResult) (wailsui.NotificationActivation, bool) {
	profileID, _ := result.Response.UserInfo["profileId"].(string)
	if profileID == "" {
		return wailsui.NotificationActivation{}, false
	}
	activation := wailsui.NotificationActivation{ProfileID: profileID}
	switch id := result.Response.UserInfo["itemId"].(type) {
	case float64:
		activation.ItemID = int64(id)
	case int64:
		activation.ItemID = id
	case int:
		activation.ItemID = int64(id)
	case json.Number:
		activation.ItemID, _ = id.Int64()
	case string:
		activation.ItemID, _ = strconv.ParseInt(id, 10, 64)
	}
	return activation, true
}

// emitWindowFocus pushes the current focused state to the frontend. Safe to
// call from native window event callbacks once the app is running.
func emitWindowFocus() {
	if app := application.Get(); app != nil {
		app.Event.Emit("window:focus", true)
	}
}

// emitWindowBlur pushes the current unfocused state to the frontend. Safe to
// call from native window event callbacks once the app is running.
func emitWindowBlur() {
	if app := application.Get(); app != nil {
		app.Event.Emit("window:blur", false)
	}
}

// emitAuthUpdated pushes the auth:updated wake-up to the frontend. Safe to
// call from any goroutine once the app is running.
func emitAuthUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("auth:updated", "changed")
	}
}

// buildFlowsStore constructs the flow.FlowStore over settings.FlowsDir(),
// backed by a Refs adapter over actionStore. It also starts a FlowsWatcher
// that reloads the store and wakes the frontend on any flows/*.yaml change,
// including the app's own SaveFlow/SaveLayout writes. A watcher that fails to
// start degrades to no hot-reload: the app still works, edits just need a
// restart to pick up.
func buildFlowsStore(actionStore *actions.ActionStore, onUpdated func(), logger zerolog.Logger) (*flow.FlowStore, *flow.FlowsWatcher) {
	dir := settings.FlowsDir()
	store := flow.NewFlowStore(dir, actions.NewRefs(actionStore))

	watcher, err := flow.NewFlowsWatcher(dir, func() {
		if err := store.Reload(); err != nil {
			logger.Warn().Err(err).Msg("flows reload failed")
		}
		if onUpdated != nil {
			onUpdated()
		}
	}, logger)
	if err != nil {
		logger.Warn().Err(err).Msg("flows hot-reload unavailable")
		return store, nil
	}
	return store, watcher
}

// emitFlowsUpdated pushes the flows:updated wake-up to the frontend. Safe to
// call from any goroutine once the app is running.
func emitFlowsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("flows:updated", "changed")
	}
}

// emitActionsUpdated wakes frontend consumers after a successful catalog
// change or a watcher reload. Service mutations call it only after success.
func emitActionsUpdated() {
	if app := application.Get(); app != nil {
		app.Event.Emit("actions:updated", "changed")
	}
}

// buildActionStore constructs the actions.ActionStore over
// settings.ActionsPath(), loading it eagerly (rather than waiting for the
// first lazy List/Get) so a broken actions.yml is logged at startup instead
// of only surfacing silently as "no actions found" the first time something
// asks. It also starts an ActionsWatcher so hand edits to actions.yml apply
// live, matching flows hot-reload posture. A watcher that fails to start
// degrades to no hot-reload: the app still works, edits just need a restart
// to pick up.
func buildActionStore(recorder activity.Recorder, logger zerolog.Logger) (*actions.ActionStore, *actions.ActionsWatcher) {
	path := settings.ActionsPath()
	if _, err := actions.SeedDefaultsIfMissing(path); err != nil {
		logger.Warn().Err(err).Msg("actions seed failed")
	}
	store := actions.NewActionStore(path)
	if err := store.Reload(); err != nil {
		logger.Warn().Err(err).Msg("actions.yml load failed; using last-good (likely empty) action set")
	}

	watcher, err := actions.NewActionsWatcher(path, func() {
		if err := store.Reload(); err != nil {
			logger.Warn().Err(err).Msg("actions.yml reload failed")
		}
		emitActionsUpdated()
		// A hand edit (or the app's own write) reloaded actions.yml: record the
		// now-effective action count so the change is auditable.
		if recorder != nil {
			recorder.Record(context.Background(), activity.ConfigReloaded("actions.yml", len(store.List())))
		}
	}, logger)
	if err != nil {
		logger.Warn().Err(err).Msg("actions.yml hot-reload unavailable")
		return store, nil
	}
	return store, watcher
}

// hiveActionRuntime owns the Hive dependencies needed by desktop actions.
// The desktop pipeline keeps its own database, while sessions and internal
// events intentionally use Hive's shared state and event bus.
type hiveActionRuntime struct {
	db     *coredb.DB
	cancel context.CancelFunc

	launcher  *dispatch.HiveSessionLauncher
	publisher dispatch.MessagePublisher
}

func (r *hiveActionRuntime) Close() {
	r.cancel()
	if err := r.db.Close(); err != nil {
		log.Printf("close hive action database: %v", err)
	}
}

func buildHiveActionRuntime(recorder activity.Recorder, logger zerolog.Logger) (*hiveActionRuntime, error) {
	dataDir := filepath.Dir(settings.StateDir())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create hive data directory: %w", err)
	}

	configPath := os.Getenv("HIVE_CONFIG")
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}
	cfg, err := config.Load(configPath, dataDir)
	if err != nil {
		return nil, fmt.Errorf("load hive config for actions: %w", err)
	}
	if err := scripts.EnsureExtracted(dataDir, "desktop"); err != nil {
		logger.Warn().Err(err).Msg("extract hive action scripts failed")
	}

	database, err := coredb.Open(dataDir, coredb.OpenOptions{
		MaxOpenConns: cfg.Database.MaxOpenConns,
		MaxIdleConns: cfg.Database.MaxIdleConns,
		BusyTimeout:  cfg.Database.BusyTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("open hive action database: %w", err)
	}
	if err := stores.MigrateFromJSON(context.Background(), database, dataDir); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate hive action data: %w", err)
	}

	bus := eventbus.New(64)
	busCtx, cancel := context.WithCancel(context.Background())
	go bus.Start(busCtx)

	profile := cfg.Agents.DefaultProfile()
	renderer := tmpl.New(tmpl.Config{
		ScriptPaths:  scripts.ScriptPaths(dataDir),
		AgentCommand: profile.CommandOrDefault(cfg.Agents.Default),
		AgentWindow:  cfg.Agents.Default,
		AgentFlags:   profile.ShellFlags(),
	})
	exec := &executil.RealExecutor{}
	sessions := hive.NewSessionService(
		stores.NewSessionStore(database),
		git.NewExecutor(cfg.GitPath, exec),
		cfg,
		bus,
		exec,
		renderer,
		logger.With().Str("component", "hive-actions").Logger(),
		io.Discard,
		io.Discard,
	)

	launcher := dispatch.NewHiveSessionLauncher(sessions)
	launcher.SetRecorder(recorder)

	return &hiveActionRuntime{
		db:        database,
		cancel:    cancel,
		launcher:  launcher,
		publisher: dispatch.NewHiveMessagePublisher(hive.NewMessageService(stores.NewMessageStore(database, cfg.Messaging.MaxMessages), cfg, bus)),
	}, nil
}

// buildOutputWorker constructs the output worker over db and actionStore.
// Mock modes do not start its background loop because no fixture flow emits
// output commands, but they retain this worker for explicit detail-pane
// confirmation RPCs. That keeps the configured action path real in e2e while
// avoiding a background shell action from compromising fixture determinism.
//
// Actions resolve through FlowNotifyActions rather than the store directly:
// a notify node's config lives in its flow, not in actions.yml, so the
// worker resolves those ids from the live flow set and everything else from
// the authored catalog.
func buildOutputWorker(db *store.DB, actionStore *actions.ActionStore, flows dispatch.FlowLister, notifier dispatch.SystemNotifier, focus *wailsui.FocusState, launcher dispatch.SessionLauncher, publisher dispatch.MessagePublisher, recorder activity.Recorder, jobRecorder jobs.Recorder, logger zerolog.Logger) *dispatch.Worker {
	dispatcher := dispatch.NewDispatcher(map[string]dispatch.Executor{
		dispatch.ActionTypeLaunchSession: dispatch.NewLaunchSessionExecutor(launcher),
		"shell":                          dispatch.NewShellExecutor(logger),
		"publish-message":                dispatch.NewPublishMessageExecutor(publisher),
		dispatch.ActionTypeNotify:        dispatch.NewNotifyExecutor(notifier, wailsui.NewNotificationGate(focus, logger), db, logger),
	})
	worker := dispatch.NewWorker(db, dispatch.NewFlowNotifyActions(flows, actionStore), dispatcher, dispatch.DefaultOutputWorkerInterval, logger)
	worker.SetRecorder(recorder)
	worker.SetJobRecorder(jobRecorder)
	return worker
}

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

	interval := feed.DefaultPollInterval
	cfg, err := settings.LoadSettings()
	if err != nil {
		logger.Warn().Err(err).Msg("desktop settings load failed; using defaults")
	} else if resolved, err := cfg.PollIntervalOrDefault(feed.DefaultPollInterval); err != nil {
		logger.Warn().Err(err).Msg("desktop settings poll interval invalid; using defaults")
	} else {
		interval = resolved
		if raw, parseErr := time.ParseDuration(cfg.PollInterval); parseErr == nil && raw < settings.MinPollInterval {
			logger.Warn().Str("configured_interval", cfg.PollInterval).Dur("interval", interval).Msg("desktop poll interval below minimum; clamped")
		}
	}

	fetcher := buildSourceFetcher(logger)
	if fetcher != nil {
		fetcher.SetSearchTTL(interval)
	}

	pipelineDB, err := store.Open(settings.StateDir(), store.DefaultOpenOptions())
	if err != nil {
		log.Fatal(err)
	}

	// The activity recorder is shared by every subsystem that reports to the
	// Activity view (producer, worker, session launcher, config watcher) and by
	// the wailsui.ActivityService the frontend reads/writes. It emits activity:appended
	// on each append so open views refresh.
	activityStore := activity.NewStore(pipelineDB, activity.Options{Emit: emitActivityAppended})
	jobStore := jobs.NewStore(pipelineDB, jobs.Options{Emit: func(int64) { emitJobsUpdated() }})
	if fetcher != nil {
		fetcher.SetRecorder(activityStore)
	}

	actionRuntime, err := buildHiveActionRuntime(activityStore, logger)
	if err != nil {
		log.Fatal(err)
	}

	// Mock mode has no live producer, so seed deterministic inbox rows for the
	// fixture flow in desktop/e2e/fixtures/flows/frontend-triage.yaml.
	if settings.MockMode() == "feed" || settings.MockMode() == "action-smoke" {
		seedMockInboxItemsOrWarn(pipelineDB, logger)
	}

	actionStore, actionsWatcher := buildActionStore(activityStore, logger)
	if actionsWatcher != nil {
		actionsWatcher.Start()
	}

	var refreshProfileTray func()
	onFlowsUpdated := func() {
		emitFlowsUpdated()
		if refreshProfileTray != nil {
			refreshProfileTray()
		}
	}

	// The flows store must exist before the producer and retention maintenance:
	// both resolve enabled flow IDs live from it.
	flowsStore, flowsWatcher := buildFlowsStore(actionStore, onFlowsUpdated, logger)
	actionStore.SetUsageChecker(wailsui.NewActionUsageChecker(flowsStore, pipelineDB))

	// Mock/server builds deliberately do not start the native Wails
	// Focus feeds both the frontend's focus-sensitive UI and the notification
	// gate's automatic delivery mode, so it is built before the output worker
	// that gate belongs to.
	focus := wailsui.NewFocusState()
	// notification service: E2E verifies preference persistence without an OS
	// bus, banner, or permission prompt. The frontend still gets a descriptive
	// unavailable binding through wailsui.NotificationService. Built before the output
	// worker because a flow's notify node delivers through the same notifier.
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

	outputWorker := buildOutputWorker(pipelineDB, actionStore, flowsStore, wailsui.NewFlowNotifier(notificationService), focus, actionRuntime.launcher, actionRuntime.publisher, activityStore, jobStore, logger)
	if settings.MockMode() == "" {
		outputWorker.Start()
	}

	maintenance := ingest.NewMaintenance(
		pipelineDB,
		flowsStore,
		store.DefaultRetentionPolicy(),
		ingest.DefaultRetentionInterval,
		logger,
	)
	maintenance.Start()

	producer := buildPipelineProducer(pipelineDB, fetcher, flowsStore, activityStore, interval, logger)
	if producer != nil {
		producer.Start()
	}

	// The webhook listener is the push-driven counterpart to the poll
	// producer: it serves user-declared webhook-source endpoints on
	// 127.0.0.1 and ingests deliveries directly. It starts when settings
	// enable it (the default) and, in mock modes, only when a port is
	// explicitly claimed via HIVE_DESKTOP_WEBHOOK_PORT so parallel e2e server
	// instances never fight over one. The port is drawn at random on first
	// run and persisted; a failure to find one, like a bind failure, logs and
	// the app runs on without webhooks.
	webhookPort, err := settings.ResolveWebhookPort(cfg)
	if err != nil {
		logger.Warn().Err(err).Msg("webhook port unavailable")
	}
	webhookEnabled := cfg.WebhookEnabledOrDefault()
	var webhookListener *webhook.Listener
	if webhookEnabled && webhookPort > 0 && (settings.MockMode() == "" || os.Getenv(settings.EnvWebhookPort) != "") {
		webhookListener = webhook.NewListener(pipelineDB, flowsStore, webhookPort, emitLogAppended, logger)
		webhookListener.SetRecorder(activityStore)
		if err := webhookListener.Start(); err != nil {
			logger.Warn().Err(err).Int("port", webhookPort).Msg("webhook listener unavailable")
		}
	}

	// Every auth transition drops the fetch cache before the frontend is
	// notified: a different account must never be served items fetched with
	// the previous token.
	onAuthChange := func() {
		if fetcher != nil {
			fetcher.Invalidate()
		}
		emitAuthUpdated()
	}

	// The updater service is created before the app (it goes in the Services
	// slice) but its engine (app.Updater) only exists after application.New, so
	// the live Updater is attached below. Auto-update defaults on; the persisted
	// toggle seeds the initial state.
	updaterVersion, _, _ := resolvedBuildInfo()
	updaterService := NewUpdaterService(updaterVersion, cfg.AutoUpdateOrDefault(), defaultUpdateCheckInterval, logger)

	services := []application.Service{
		application.NewService(auth.NewService(buildAuthBackend(onAuthChange))),
		application.NewService(wailsui.NewPipelineService(pipelineDB, actionStore, outputWorker, actionRuntime.launcher)),
		application.NewService(wailsui.NewFlowsService(flowsStore, pipelineDB, onFlowsUpdated)),
		application.NewService(wailsui.NewActionsService(actionStore, emitActionsUpdated)),
		application.NewService(wailsui.NewActivityService(activityStore)),
		application.NewService(wailsui.NewJobService(jobStore)),
		application.NewService(NewSystemService()),
		application.NewService(wailsui.NewSettingsService(producer, fetcher, logger)),
		application.NewService(wailsui.NewWebhookService(pipelineDB, webhookListener, webhookPort)),
		application.NewService(wailsui.NewPromptsService(webhookListener, webhookPort)),
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
	// Built this late deliberately: buildActionStore has seeded actions.yml and
	// mock seeding has run, so the captured config baseline is the post-boot
	// state a reset must restore.
	resetHarness := newStateResetHarness(pipelineDB, actionRuntime.db, logger)

	options := application.Options{
		Name:        "Hive",
		Description: "Hive desktop application",
		Icon:        appIcon,
		Services:    services,
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: desktopSmokeMiddleware(pipelineDB, actionRuntime.db, resetHarness),
		},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyRegular,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	}
	app := application.New(options)

	// Configure self-update only for published release builds: releaseChannel
	// rejects source builds ("dev") and pseudo-versions, so the engine stays
	// nil there and the service degrades to Available:false with a no-op
	// ticker. A published build follows its own channel (a beta build tracks
	// beta, per docs/decisions/0004) unless settings.yaml's update_channel
	// overrides it.
	if channel, ok := releaseChannel(updaterVersion); ok {
		provider := newManifestProvider(defaultManifestBaseURL, cfg.UpdateChannelOrDefault(channel))
		if initErr := app.Updater.Init(updater.Config{
			CurrentVersion: updaterVersion,
			Providers:      []updater.Provider{provider},
		}); initErr != nil {
			logger.Warn().Err(initErr).Msg("desktop auto-update unavailable; updater init failed")
		} else {
			updaterService.attach(app.Updater)
		}
	}

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
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
			if activation, ok := notificationActivation(result); ok {
				emitNotificationActivated(activation)
			}
		})
	}

	window.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
		if focus.Set(true) {
			emitWindowFocus()
		}
	})
	window.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		if focus.Set(false) {
			emitWindowBlur()
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
			emitWindowBlur()
		}
		e.Cancel()
	})

	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		window.Show()
	})

	profilesTray := newProfileTray(
		app,
		flowsStore,
		logger,
		trayIcon,
		onFlowsUpdated,
		func() {
			window.Show()
			window.Focus()
		},
		app.Quit,
	)
	refreshProfileTray = profilesTray.Refresh
	if flowsWatcher != nil {
		// Start only after refreshProfileTray is published. The watcher invokes
		// onFlowsUpdated from its goroutine, so starting earlier would race the
		// callback assignment above.
		flowsWatcher.Start()
	}
	app.OnShutdown(func() {
		profilesTray.Close()
		if flowsWatcher != nil {
			flowsWatcher.Close()
		}
	})

	shutdown := func() {
		updaterService.stop()
		if webhookListener != nil {
			webhookListener.Stop()
		}
		maintenance.Stop()
		actionRuntime.Close()
		logCloser()
	}
	if err := app.Run(); err != nil {
		shutdown()
		log.Fatal(err)
	}
	shutdown()
}
