package app

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/colonyops/hive/pkg/executil"
	"github.com/colonyops/hive/pkg/tmpl"
	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/dispatch"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/ingest"
	"github.com/hay-kot/hive-desktop/internal/app/jobs"
	"github.com/hay-kot/hive-desktop/internal/app/runtime"
	"github.com/hay-kot/hive-desktop/internal/app/runtime/js"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/config"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/eventbus"
	"github.com/hay-kot/hive-desktop/internal/hivecore/core/git"
	coredb "github.com/hay-kot/hive-desktop/internal/hivecore/data/db"
	"github.com/hay-kot/hive-desktop/internal/hivecore/data/stores"
	"github.com/hay-kot/hive-desktop/internal/hivecore/hive"
	"github.com/hay-kot/hive-desktop/internal/hivecore/hive/scripts"
)

// Config is everything App needs that it cannot resolve itself.
type Config struct {
	Settings      settings.Settings
	SettingsStore *settings.Store
	Paths         settings.Paths
	MockMode      string
	Logger        zerolog.Logger

	// Notifier and Gate are driven ports the adapter fills. They are the one
	// place a GUI-owned dependency legitimately enters the core, and they
	// enter as interfaces defined by their consumer.
	Notifier dispatch.SystemNotifier
	Gate     dispatch.NotificationGate
}

// App is the headless core. Driving adapters hold *App and the concrete
// types on it; there are no driving-port interfaces.
type App struct {
	// The per-domain services. Driving adapters call these; the stores below
	// are what they are built over.
	Inbox    *InboxService
	Flows    *FlowsService
	Actions  *ActionsService
	Settings *SettingsService
	System   *SystemService
	Webhooks *WebhookService
	GitHub   *GitHubService
	// Integrations lists the connector registry with each entry's connection
	// state. Generic; GitHub above is the provider-specific acquisition half.
	Integrations *IntegrationsService
	Activity     *ActivityService
	Jobs         *JobService
	Prompts      *PromptsService

	Events *events.Bus
	Store  *store.DB
	Logger zerolog.Logger

	// Domain stores. The per-domain services that will front them are the
	// next commit; until then adapters hold these directly, exactly as
	// package main did.
	ActionStore   *actions.ActionStore
	FlowStore     *flow.FlowStore
	ActivityStore *activity.Store
	JobStore      *jobs.Store
	Fetchers      *ghsource.Fetchers
	Credentials   credentials.Store

	// GitHubConnection acquires and releases GitHub credentials. It is one
	// connector's, not the app's: nothing here is gated on it holding one.
	GitHubConnection ghsource.Connection

	// Sources resolves the current flow set into live connector instances.
	// Both ingress paths go through it — the poll producer takes its
	// pull-mode instances, the webhook listener its push-mode ones — so a
	// connector is constructed one way regardless of how it delivers.
	Sources *ingest.Resolver

	// Background subsystems, owned here so main.go stops holding them.
	// Uniform lifecycle through a plugs manager is a later phase.
	Producer    *ingest.Producer
	Engine      *runtime.Engine
	Outputs     *dispatch.Worker
	Retention   *ingest.Maintenance
	Webhook     *webhook.Listener
	WebhookHost string
	WebhookPort int

	// Hive integration: sessions and internal events use Hive's own shared
	// state and event bus, while this app keeps its own database.
	Launcher *dispatch.HiveSessionLauncher
	hiveDB   *coredb.DB

	// PollInterval is the validated, clamped interval the producer polls on.
	PollInterval time.Duration

	mock      string
	publisher dispatch.MessagePublisher

	// ctx is the application's lifetime, not a request's. Background
	// callbacks wired at construction — the config watchers, the GitHub
	// connection's change hook — publish with it, and Close cancels it. This is
	// the one type in the core that legitimately holds a context: it is the
	// thing whose lifetime that context represents.
	ctx    context.Context
	cancel context.CancelFunc

	settings       settings.Settings
	settingsStore  *settings.Store
	paths          settings.Paths
	flowsWatcher   *flow.FlowsWatcher
	actionsWatcher *actions.ActionsWatcher
	hiveBusCancel  context.CancelFunc
}

// New builds the core: the store, the domain stores and their watchers, the
// Hive action runtime, and the background subsystems. Nothing is running when
// it returns — call Start.
func New(ctx context.Context, cfg Config) (*App, error) {
	if cfg.Paths.SettingsPath == "" {
		b, _ := settings.LoadBootstrap()
		cfg.Paths = settings.ResolvePaths(b, cfg.MockMode)
	}
	if cfg.SettingsStore == nil {
		cfg.SettingsStore = settings.NewStore(cfg.Paths.SettingsPath)
	}
	runCtx, cancel := context.WithCancel(ctx)

	a := &App{
		Logger:        cfg.Logger,
		Events:        events.New(cfg.Logger),
		mock:          cfg.MockMode,
		settings:      cfg.Settings,
		settingsStore: cfg.SettingsStore,
		paths:         cfg.Paths,
		ctx:           runCtx,
		cancel:        cancel,
	}

	a.PollInterval = cfg.Settings.Polling.Interval.Duration()

	// Mock modes get an in-memory credential store: a keychain read can
	// prompt, and a fixture run that prompts is a fixture run that hangs.
	a.Credentials = buildCredentialStore(cfg.MockMode, cfg.Paths.CredentialsIndexPath)

	if cfg.MockMode == "" {
		a.Fetchers = ghsource.NewFetchers(ghsource.DefaultClient, a.Credentials, cfg.Logger)
		a.Fetchers.SetSearchTTL(a.PollInterval)
	}

	dbOptions := store.DefaultOpenOptions()
	dbOptions.PauseIngest = cfg.Settings.Development.Debug.PauseIngest.Duration()
	dbOptions.PauseCommit = cfg.Settings.Development.Debug.PauseCommit.Duration()
	db, err := store.Open(ctx, cfg.Paths.StateDir, dbOptions)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open desktop store: %w", err)
	}
	a.Store = db

	// The activity recorder is shared by every subsystem that reports to the
	// Activity view (producer, worker, session launcher, config watchers) and
	// by the ActivityService the frontend reads and writes.
	a.ActivityStore = activity.NewStore(db, activity.Options{Emit: func(id int64) {
		a.Events.Publish(a.ctx, events.ActivityAppended{ID: id})
	}})
	a.JobStore = jobs.NewStore(db, jobs.Options{Emit: func(id int64) {
		a.Events.Publish(a.ctx, events.JobsUpdated{JobID: id})
	}})
	if a.Fetchers != nil {
		a.Fetchers.SetRecorder(a.ActivityStore)
	}

	if err := a.openHiveRuntime(runCtx, cfg); err != nil {
		_ = a.Store.Close()
		cancel()
		return nil, err
	}

	a.openActions(cfg.Paths.ActionsPath, cfg.Logger)
	a.openFlows(cfg.Paths.FlowsDir, cfg.Logger)
	a.ActionStore.SetUsageChecker(newActionUsage(a.FlowStore, db))

	a.GitHubConnection = buildGitHubConnection(cfg.MockMode, a.Credentials, func() {
		// Every connection transition drops this provider's fetch caches
		// before anything is notified: a different account must never be
		// served items fetched with the previous token. Fetchers is already
		// GitHub's alone, so invalidating all of them is exactly this
		// provider's scope — and over-invalidating costs a refetch, where
		// under-invalidating serves another account's items.
		if a.Fetchers != nil {
			a.Fetchers.InvalidateAll()
		}
		a.Events.Publish(a.ctx, events.ConnectionUpdated{Provider: ghsource.Provider})
	})

	a.Outputs = a.buildOutputWorker(cfg)
	a.Retention = ingest.NewMaintenance(db, a.FlowStore, store.DefaultRetentionPolicy(), ingest.DefaultRetentionInterval, cfg.Logger)
	a.Engine = a.buildEngine(cfg.Logger)
	a.Sources = a.buildSources(cfg.Logger)
	a.Producer = a.buildProducer(cfg.Logger)
	a.openWebhook(runCtx, cfg)

	a.Inbox = newInboxService(db, a.ActionStore, a.Outputs, a.Launcher)
	a.Flows = newFlowsService(a.FlowStore, db, a.Credentials, func() { a.PublishFlowsUpdated("save") })
	a.Actions = newActionsService(a.ActionStore, func() {
		a.Events.Publish(a.ctx, events.ActionsUpdated{Count: len(a.ActionStore.List())})
	})
	a.Settings = newSettingsService(cfg.SettingsStore, a.Producer, a.Fetchers)
	a.System = newSystemService(cfg.Paths)
	a.Webhooks = newWebhookService(cfg.SettingsStore, db, a.Webhook, a.WebhookHost, a.WebhookPort)
	a.GitHub = newGitHubService(a.GitHubConnection)
	a.Integrations = newIntegrationsService(a.Credentials)
	a.Activity = newActivityService(a.ActivityStore)
	a.Jobs = newJobService(a.JobStore)
	a.Prompts = newPromptsService(cfg.Paths, cfg.SettingsStore, a.Webhooks)

	return a, nil
}

// Start runs the background subsystems: the config watchers, the output
// worker, retention, the poll producer, and the webhook listener. Mock modes
// deliberately skip the worker loop and have no producer, so a fixture run
// stays deterministic.
func (a *App) Start(ctx context.Context) error {
	if a.actionsWatcher != nil {
		a.actionsWatcher.Start()
	}
	if a.flowsWatcher != nil {
		a.flowsWatcher.Start()
	}
	if a.mock == "" {
		a.Outputs.Start(ctx)
	}
	a.Retention.Start(ctx)
	// The engine starts before anything that can append to the log. Its flow
	// installation is synchronous, so by the time a producer tick, a webhook
	// delivery or a test harness can append, there is a runner ready to route
	// it — no window in which a wake-up has nothing to wake.
	if err := a.Engine.Start(ctx); err != nil {
		return fmt.Errorf("start flow engine: %w", err)
	}
	if a.Producer != nil {
		a.Producer.Start(ctx)
	}
	if a.Webhook != nil {
		if err := a.Webhook.Start(ctx); err != nil {
			a.Webhooks.setStartError(err)
			a.Logger.Warn().Err(err).Int("port", a.WebhookPort).Msg("webhook listener unavailable")
		} else if a.WebhookPort == 0 && !a.settings.EnvironmentOverridden(settings.EnvWebhookPort) {
			_, err := a.settingsStore.Update(func(persisted *settings.Settings) error {
				persisted.Webhooks.Port = a.Webhook.Port()
				return nil
			})
			if err != nil {
				a.Webhooks.setStartError(fmt.Errorf("persist allocated webhook port: %w", err))
				a.Logger.Warn().Err(err).Msg("persist allocated webhook port")
			}
		}
	}
	return nil
}

// Close stops the background subsystems and releases resources. It is the
// single teardown path: the adapter's own shutdown hook covers only what it
// owns (a tray, a window).
// RuntimePaths returns the immutable location snapshot used by this process.
func (a *App) RuntimePaths() settings.Paths { return a.paths }

// HiveConn exposes the connection to the vendored Hive action database
// (sessions, messages) as a plain *sql.DB. hivecore types stop at this
// method — adapters that need raw access, such as the e2e harness's table
// resets and read-only snapshots, take the stdlib type rather than the
// vendored *coredb.DB.
func (a *App) HiveConn() *sql.DB {
	if a.hiveDB == nil {
		return nil
	}
	return a.hiveDB.Conn()
}

func (a *App) Close() error {
	a.cancel()

	if a.Webhook != nil {
		a.Webhook.Stop()
	}
	if a.Producer != nil {
		a.Producer.Stop()
	}
	a.Engine.Stop()
	a.Retention.Stop()
	a.Outputs.Stop()
	if a.flowsWatcher != nil {
		a.flowsWatcher.Close()
	}
	if a.actionsWatcher != nil {
		a.actionsWatcher.Close()
	}
	a.Events.Close()

	if a.hiveBusCancel != nil {
		a.hiveBusCancel()
	}

	var err error
	if a.hiveDB != nil {
		if closeErr := a.hiveDB.Close(); closeErr != nil {
			err = fmt.Errorf("close hive action database: %w", closeErr)
		}
	}
	if closeErr := a.Store.Close(); closeErr != nil && err == nil {
		err = fmt.Errorf("close desktop store: %w", closeErr)
	}
	return err
}

func buildGitHubConnection(mock string, creds credentials.Store, onChange func()) ghsource.Connection {
	switch mock {
	case "feed", "pipeline", "action-smoke":
		return ghsource.NewMockConnection(true, creds, onChange)
	case settings.MockOnboarding:
		return ghsource.NewMockConnection(false, creds, onChange)
	default:
		return ghsource.NewLiveConnection(ghsource.DefaultClient, creds, onChange)
	}
}

// buildCredentialStore picks the credential backing. Mock modes never touch
// the OS keychain: reading one can prompt, and the e2e harness has no way to
// answer.
func buildCredentialStore(mock, indexPath string) credentials.Store {
	if mock != "" {
		return credentials.NewMemoryStore()
	}
	return credentials.NewKeychainStore(indexPath)
}

// openActions loads actions.yml eagerly — rather than waiting for the first
// lazy List/Get — so a broken file is logged at startup instead of surfacing
// silently as "no actions found". A watcher that fails to start degrades to
// no hot-reload: the app still works, edits just need a restart.
func (a *App) openActions(path string, logger zerolog.Logger) {
	if _, err := actions.SeedDefaultsIfMissing(path); err != nil {
		logger.Warn().Err(err).Msg("actions seed failed")
	}
	a.ActionStore = actions.NewActionStore(path)
	if err := a.ActionStore.Reload(); err != nil {
		logger.Warn().Err(err).Msg("actions.yml load failed; using last-good (likely empty) action set")
	}

	watcher, err := actions.NewActionsWatcher(path, func() {
		if err := a.ActionStore.Reload(); err != nil {
			logger.Warn().Err(err).Msg("actions.yml reload failed")
		}
		count := len(a.ActionStore.List())
		a.Events.Publish(a.ctx, events.ActionsUpdated{Count: count})
		// A hand edit (or the app's own write) reloaded actions.yml: record
		// the now-effective action count so the change is auditable.
		a.ActivityStore.Record(a.ctx, activity.ConfigReloaded("actions.yml", count))
	}, logger)
	if err != nil {
		logger.Warn().Err(err).Msg("actions.yml hot-reload unavailable")
		return
	}
	a.actionsWatcher = watcher
}

// openFlows constructs the flow store over settings.FlowsDir() and a watcher
// that reloads it on any flows/*.yaml change, including the app's own
// SaveFlow/SaveLayout writes. It must run before the producer and retention:
// both resolve enabled flow ids live from the store.
func (a *App) openFlows(dir string, logger zerolog.Logger) {
	a.FlowStore = flow.NewFlowStore(dir, actions.NewRefs(a.ActionStore))

	watcher, err := flow.NewFlowsWatcher(dir, func() {
		if err := a.FlowStore.Reload(); err != nil {
			logger.Warn().Err(err).Msg("flows reload failed")
		}
		a.PublishFlowsUpdated("reload")
	}, logger)
	if err != nil {
		logger.Warn().Err(err).Msg("flows hot-reload unavailable")
		return
	}
	a.flowsWatcher = watcher
}

// PublishLogAppended announces that the event log grew and wakes the engine to
// route it. The producer and the webhook listener go through the same path;
// this method is exported for the one caller that writes through the store
// directly — the e2e source-to-commit harness, which stands in for a producer
// tick.
func (a *App) PublishLogAppended(nextOffset int64) {
	a.Engine.Wake()
	a.Events.Publish(a.ctx, events.LogAppended{NextOffset: nextOffset})
}

// PublishFlowsUpdated announces a change to the flow set and reinstalls the
// engine's runners against it. The app's own writes go through it too, so a
// save and an external edit are one path.
func (a *App) PublishFlowsUpdated(reason string) {
	a.Engine.Reload()
	a.Events.Publish(a.ctx, events.FlowsUpdated{Reason: reason})
}

// buildEngine wires the flow engine over the store and the live flow set. It
// runs in every mode, including the mock ones: a fixture that seeds inbox
// items also appends the source snapshot they came from, so the engine's
// replay recomputes exactly the membership the fixture declared rather than
// contradicting it.
func (a *App) buildEngine(logger zerolog.Logger) *runtime.Engine {
	scripts := runtime.NewScriptRegistry()
	scripts.Register(js.New(runtime.NewScriptPool(0)))

	return runtime.NewEngine(runtime.EngineOptions{
		Store:   a.Store,
		Flows:   a.FlowStore,
		Scripts: scripts,
		Logger:  logger,
		OnCommitted: func() {
			a.Events.Publish(a.ctx, events.InboxUpdated{})
		},
		OnFlowError: func(flowID string, err error) {
			a.ActivityStore.Record(a.ctx, activity.FlowRuntimeFailed(flowID, err))
		},
	})
}

// buildSources wires the instance half of every source connector's
// declaration. The descriptors are static and live in the registry; the
// factories need dependencies — the GitHub fetcher — and so are built here,
// at the one place that holds them.
//
// TestFactoriesCoverEveryDescriptor fails if a registered connector has no
// factory, which is the failure mode this split trades for: a connector
// declared and not wired is a source node the editor offers and nothing ever
// polls.
func (a *App) buildSources(logger zerolog.Logger) *ingest.Resolver {
	return ingest.NewResolver(a.FlowStore, sourceFactories(a.Fetchers), logger)
}

// sourceFactories is the instance half of the connector registry. It is a
// function of its dependencies rather than a method so the bijection test can
// hold it against the descriptors without standing up an App.
func sourceFactories(fetchers *ghsource.Fetchers) map[string]connector.Factory {
	factories := map[string]connector.Factory{
		webhook.Descriptor.Type: webhook.NewFactory(),
	}
	// Mock modes have no fetchers, so the GitHub connector has nothing to
	// construct instances over and is left out of the map: resolving one logs
	// and skips rather than dereferencing nil.
	if fetchers != nil {
		factories[ghsource.Descriptor.Type] = ghsource.NewFactory(fetchers)
	}
	return factories
}

// buildProducer starts nothing; it wires the event-log producer over every
// enabled pull-mode source node across all flows. Mock modes have no fetcher
// and therefore no producer.
func (a *App) buildProducer(logger zerolog.Logger) *ingest.Producer {
	if a.Fetchers == nil {
		return nil
	}
	producer := ingest.NewProducer(a.Store, a.Sources, a.PollInterval, a.PublishLogAppended, logger)
	producer.SetRecorder(a.ActivityStore)
	producer.SetDebugPause(a.settings.Development.Debug.PauseIngest.Duration())
	return producer
}

// buildOutputWorker wires the output worker. Mock modes keep the worker for
// explicit detail-pane confirmation RPCs but never start its loop: that keeps
// the configured action path real in e2e while stopping a background shell
// action from compromising fixture determinism.
//
// Actions resolve through FlowNotifyActions rather than the catalog directly:
// a notify node's config lives in its flow, not in actions.yml, so the worker
// resolves those ids from the live flow set and everything else from the
// authored catalog.
func (a *App) buildOutputWorker(cfg Config) *dispatch.Worker {
	dispatcher := dispatch.NewDispatcher(map[string]dispatch.Executor{
		dispatch.ActionTypeLaunchSession: dispatch.NewLaunchSessionExecutor(a.Launcher),
		"shell":                          dispatch.NewShellExecutor(cfg.Logger),
		"publish-message":                dispatch.NewPublishMessageExecutor(a.publisher),
		dispatch.ActionTypeNotify:        dispatch.NewNotifyExecutor(cfg.Notifier, cfg.Gate, a.Store, cfg.Logger),
	})
	worker := dispatch.NewWorker(a.Store, dispatch.NewFlowNotifyActions(a.FlowStore, a.ActionStore), dispatcher, dispatch.DefaultOutputWorkerInterval, cfg.Logger)
	worker.SetRecorder(a.ActivityStore)
	worker.SetJobRecorder(a.JobStore)
	return worker
}

// openWebhook constructs the optional loopback listener without binding it.
// Port zero is passed through to net.Listen so the OS allocates without a
// probe/rebind race. Mock instances only claim a listener through an explicit
// port override, keeping parallel e2e lanes isolated.
func (a *App) openWebhook(_ context.Context, cfg Config) {
	a.WebhookHost = cfg.Settings.Webhooks.Host
	a.WebhookPort = cfg.Settings.Webhooks.Port
	if !cfg.Settings.Webhooks.Enabled {
		return
	}
	if cfg.MockMode != "" && !cfg.Settings.EnvironmentOverridden(settings.EnvWebhookPort) {
		return
	}

	a.Webhook = webhook.NewListener(a.Store, a.Sources.PushInstances, a.WebhookHost, a.WebhookPort, a.PublishLogAppended, cfg.Logger)
	a.Webhook.SetRecorder(a.ActivityStore)
}

// openHiveRuntime opens the Hive dependencies desktop actions need. The
// desktop keeps its own database, while sessions and internal events
// intentionally use Hive's shared state and event bus.
func (a *App) openHiveRuntime(ctx context.Context, cfg Config) error {
	dataDir := cfg.Paths.DataDir
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create hive data directory: %w", err)
	}

	configPath := os.Getenv("HIVE_CONFIG")
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}
	hiveCfg, err := config.Load(configPath, dataDir)
	if err != nil {
		return fmt.Errorf("load hive config for actions: %w", err)
	}
	if err := scripts.EnsureExtracted(dataDir, "desktop"); err != nil {
		cfg.Logger.Warn().Err(err).Msg("extract hive action scripts failed")
	}

	// coredb.Open takes no context: it is vendored, and its signature belongs
	// to hive upstream. Landing a change there and re-vendoring is the only
	// way to thread one.
	//nolint:contextcheck // vendored signature, see internal/hivecore
	database, err := coredb.Open(dataDir, coredb.OpenOptions{
		MaxOpenConns: hiveCfg.Database.MaxOpenConns,
		MaxIdleConns: hiveCfg.Database.MaxIdleConns,
		BusyTimeout:  hiveCfg.Database.BusyTimeout,
	})
	if err != nil {
		return fmt.Errorf("open hive action database: %w", err)
	}
	if err := stores.MigrateFromJSON(ctx, database, dataDir); err != nil {
		_ = database.Close()
		return fmt.Errorf("migrate hive action data: %w", err)
	}
	a.hiveDB = database

	bus := eventbus.New(64)
	busCtx, cancel := context.WithCancel(ctx)
	a.hiveBusCancel = cancel
	go bus.Start(busCtx)

	profile := hiveCfg.Agents.DefaultProfile()
	renderer := tmpl.New(tmpl.Config{
		ScriptPaths:  scripts.ScriptPaths(dataDir),
		AgentCommand: profile.CommandOrDefault(hiveCfg.Agents.Default),
		AgentWindow:  hiveCfg.Agents.Default,
		AgentFlags:   profile.ShellFlags(),
	})
	exec := &executil.RealExecutor{}
	sessions := hive.NewSessionService(
		stores.NewSessionStore(database),
		git.NewExecutor(hiveCfg.GitPath, exec),
		hiveCfg,
		bus,
		exec,
		renderer,
		cfg.Logger.With().Str("component", "hive-actions").Logger(),
		io.Discard,
		io.Discard,
	)

	a.Launcher = dispatch.NewHiveSessionLauncher(sessions)
	a.Launcher.SetRecorder(a.ActivityStore)
	a.publisher = dispatch.NewHiveMessagePublisher(hive.NewMessageService(stores.NewMessageStore(database, hiveCfg.Messaging.MaxMessages), hiveCfg, bus))
	return nil
}
