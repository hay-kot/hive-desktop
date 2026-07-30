package app

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	"github.com/hay-kot/hive-desktop/internal/app/profileimg"
	"github.com/hay-kot/hive-desktop/internal/app/report"
	"github.com/hay-kot/hive-desktop/internal/app/runtime"
	"github.com/hay-kot/hive-desktop/internal/app/runtime/js"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
	"github.com/hay-kot/hive-desktop/internal/app/skills"
	"github.com/hay-kot/hive-desktop/internal/app/sourcemark"
	"github.com/hay-kot/hive-desktop/internal/app/sources/connector"
	ghsource "github.com/hay-kot/hive-desktop/internal/app/sources/github"
	"github.com/hay-kot/hive-desktop/internal/app/sources/github/ghclient"
	"github.com/hay-kot/hive-desktop/internal/app/sources/grafana"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/hay-kot/hive-desktop/internal/app/tmuxbin"
	"github.com/hay-kot/hive-desktop/internal/app/tmuxcc"
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

	// Build stamps the running binary into report bundles. ReportUploader is a
	// driven port; nil disables problem reporting (no report token in the build).
	Build          report.Build
	ReportUploader report.Uploader
}

// App is the headless core. Driving adapters hold *App and the concrete
// types on it; there are no driving-port interfaces.
type App struct {
	// The per-domain services. Driving adapters call these — never the
	// unexported domain stores further down, which is what they are built
	// over.
	Inbox    *InboxService
	Sessions *SessionsService
	Flows    *FlowsService
	Actions  *ActionsService
	Settings *SettingsService
	System   *SystemService
	Webhooks *WebhookService
	GitHub   *GitHubService
	Grafana  *GrafanaService
	// Integrations lists the connector registry with each entry's connection
	// state. Generic; GitHub and Grafana above are the provider-specific
	// acquisition halves.
	Integrations *IntegrationsService
	Activity     *ActivityService
	Jobs         *JobService
	Prompts      *PromptsService
	Skills       *SkillsService
	Report       *ReportService
	Terminals    *TerminalsService

	// Events is the typed pub/sub bus wailsui.Subscribe degrades into
	// wake-up events for the frontend. Store is the one raw handle every
	// driving adapter may still hold directly: an app-owned type (not
	// vendored), needed by the e2e harness for table resets and fixture
	// seeding that no per-domain service has a reason to expose otherwise.
	Events *events.Bus
	Store  *store.DB

	// Domain stores. Nothing outside this package holds these — a bypass
	// here is exactly the bug this rule exists to prevent: ProfileTray once
	// wrote through flowStore directly (store.SetEnabled), duplicating
	// FlowsService.SetEnabled minus its typed-error wrapping and its
	// notifyUpdated event. A domain's need is a method on its service, not
	// the store underneath it. activityStore and jobStore are unexported for
	// the same reason despite looking store-shaped: ActivityService and
	// JobService above already front them, so nothing else may reach past
	// those either.
	actionStore   *actions.ActionStore
	flowStore     *flow.FlowStore
	activityStore *activity.Store
	jobStore      *jobs.Store
	fetchers      *ghsource.Fetchers
	credentials   credentials.Store

	// gitHubConnection acquires and releases GitHub credentials. It is one
	// connector's, not the app's: nothing here is gated on it holding one.
	gitHubConnection ghsource.Connection

	// grafanaFetchers hands out one per-stack fetcher; grafanaAuth connects and
	// disconnects stacks. Like GitHub, nothing is gated on them.
	grafanaFetchers *grafana.Fetchers
	grafanaAuth     *grafana.Authenticator

	// sources resolves the current flow set into live connector instances.
	// Both ingress paths go through it — the poll producer takes its
	// pull-mode instances, the webhook listener its push-mode ones — so a
	// connector is constructed one way regardless of how it delivers.
	sources *ingest.Resolver

	// Background subsystems, owned here so main.go stops holding them.
	// Uniform lifecycle through a plugs manager was evaluated and declined
	// for now — see the note on Close.
	producer    *ingest.Producer
	engine      *runtime.Engine
	outputs     *dispatch.Worker
	retention   *ingest.Maintenance
	webhook     *webhook.Listener
	webhookHost string
	webhookPort int

	// Hive integration: sessions and internal events use Hive's own shared
	// state and event bus, while this app keeps its own database.
	launcher *dispatch.HiveSessionLauncher
	sessions *dispatch.HiveSessionManager
	hiveDB   *coredb.DB

	// terminals owns one tmux control-mode client per attached session slug.
	// Its context is the app's lifetime, not a request's (ADR 0036).
	terminals *tmuxcc.Manager

	// tmux is the one place the tmux binary is discovered, shared by the
	// terminal's control clients and Hive's session spawning (ADR 0039).
	tmux *tmuxbin.Resolver

	// pollInterval is the validated, clamped interval the producer polls on.
	pollInterval time.Duration

	logger    zerolog.Logger
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
		logger:        cfg.Logger,
		Events:        events.New(cfg.Logger),
		mock:          cfg.MockMode,
		settings:      cfg.Settings,
		settingsStore: cfg.SettingsStore,
		paths:         cfg.Paths,
		ctx:           runCtx,
		cancel:        cancel,
	}

	a.pollInterval = cfg.Settings.Polling.Interval.Duration()
	a.tmux = tmuxbin.NewResolver(cfg.Settings.Paths.Tmux)

	// Mock modes get an in-memory credential store: a keychain read can
	// prompt, and a fixture run that prompts is a fixture run that hangs.
	a.credentials = buildCredentialStore(cfg.MockMode, cfg.Paths.CredentialsIndexPath)

	// One client template backs both the fetch layer and the connect flow, so a
	// development instance pointed at cmd/devserver never splits its traffic
	// between the proxy and real GitHub. The API base is the ADR 0017 dev
	// override and is empty in shipped builds; the OAuth base is never
	// redirected, so the device flow still reaches github.com.
	gitHubOpts := []ghclient.Option{ghclient.WithLogger(cfg.Logger)}
	if apiBase := cfg.Settings.GitHubAPIBase(); apiBase != "" {
		gitHubOpts = append(gitHubOpts, ghclient.WithAPIBase(apiBase))
	}
	gitHubClient := ghclient.NewClient(gitHubOpts...)

	if cfg.MockMode == "" {
		a.fetchers = ghsource.NewFetchers(gitHubClient, a.credentials, cfg.Logger)
		a.fetchers.SetSearchTTL(a.pollInterval)
	}

	dbOptions := store.DefaultOpenOptions()
	dbOptions.PauseIngest = cfg.Settings.Development.Debug.PauseIngest.Duration()
	dbOptions.PauseCommit = cfg.Settings.Development.Debug.PauseCommit.Duration()
	dbOptions.Logger = cfg.Logger
	db, err := store.Open(ctx, cfg.Paths.StateDir, dbOptions)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open desktop store: %w", err)
	}
	a.Store = db

	// The activity recorder is shared by every subsystem that reports to the
	// Activity view (producer, worker, session launcher, config watchers) and
	// by the ActivityService the frontend reads and writes.
	a.activityStore = activity.NewStore(db, activity.Options{Emit: func(id int64) {
		a.Events.Publish(a.ctx, events.ActivityAppended{ID: id})
	}})
	a.jobStore = jobs.NewStore(db, jobs.Options{Emit: func(id int64) {
		a.Events.Publish(a.ctx, events.JobsUpdated{JobID: id})
	}})
	if a.fetchers != nil {
		a.fetchers.SetRecorder(a.activityStore)
	}

	if err := a.openHiveRuntime(runCtx, cfg); err != nil {
		_ = a.Store.Close()
		cancel()
		return nil, err
	}
	a.terminals = tmuxcc.NewManager(runCtx, tmuxcc.ManagerOptions{Logger: cfg.Logger, Binary: a.tmux.Path})

	a.openActions(cfg.Paths.ActionsPath, cfg.Logger)
	a.openFlows(cfg.Paths.FlowsDir, cfg.Logger)
	a.actionStore.SetUsageChecker(newActionUsage(a.flowStore, db))

	a.gitHubConnection = buildGitHubConnection(cfg.MockMode, gitHubClient, a.credentials, func() {
		// Every connection transition drops this provider's fetch caches
		// before anything is notified: a different account must never be
		// served items fetched with the previous token. Fetchers is already
		// GitHub's alone, so invalidating all of them is exactly this
		// provider's scope — and over-invalidating costs a refetch, where
		// under-invalidating serves another account's items.
		if a.fetchers != nil {
			a.fetchers.InvalidateAll()
		}
		a.Events.Publish(a.ctx, events.ConnectionUpdated{Provider: ghsource.Provider})
	})

	// Grafana's client is built per tick from a stack URL and token, so unlike
	// GitHub there is no mock-mode fetch template to gate on — the fetcher
	// registry and its stack store are always wired.
	grafanaStacks := grafana.NewStackStore(filepath.Join(cfg.Paths.StateDir, "grafana-stacks.json"))
	a.grafanaFetchers = grafana.NewFetchers(grafanaStacks, a.credentials, cfg.Logger)
	a.grafanaAuth = grafana.NewAuthenticator(a.credentials, grafanaStacks, cfg.Logger, func(credentials.Ref) {
		// Drop every stack's cooldown so a freshly connected account isn't held
		// back by its predecessor's rate limit, then announce so Integrations re-reads.
		a.grafanaFetchers.InvalidateAll()
		a.Events.Publish(a.ctx, events.ConnectionUpdated{Provider: grafana.Provider})
	})

	a.outputs = a.buildOutputWorker(cfg)
	a.retention = ingest.NewMaintenance(db, a.flowStore, store.DefaultRetentionPolicy(), ingest.DefaultRetentionInterval, cfg.Logger)
	a.engine = a.buildEngine(cfg.Logger)
	a.sources = a.buildSources(cfg.Logger)
	a.producer = a.buildProducer(cfg.Logger)
	a.openWebhook(runCtx, cfg)

	a.Inbox = newInboxService(db, a.actionStore, a.outputs)
	a.Sessions = newSessionsService(a.launcher, a.sessions, a.terminals, a.jobStore)
	profileImages := profileimg.NewStore(filepath.Join(cfg.Paths.StateDir, "assets", "profiles"))
	sourceMarks := sourcemark.NewStore(filepath.Join(cfg.Paths.StateDir, "assets", "webhookmarks"))
	a.Flows = newFlowsService(a.flowStore, db, a.credentials, profileImages, sourceMarks, func() { a.PublishFlowsUpdated("save") })
	a.Actions = newActionsService(a.actionStore, func() {
		a.Events.Publish(a.ctx, events.ActionsUpdated{Count: len(a.actionStore.List())})
	})
	a.Settings = newSettingsService(cfg.SettingsStore, a.producer, a.fetchers)
	a.System = newSystemService(cfg.Paths)
	a.Webhooks = newWebhookService(cfg.SettingsStore, db, a.webhook, sourceMarks, a.webhookHost, a.webhookPort)
	a.GitHub = newGitHubService(a.gitHubConnection)
	a.Grafana = newGrafanaService(a.grafanaAuth)
	a.Integrations = newIntegrationsService(a.credentials)
	a.Activity = newActivityService(a.activityStore)
	a.Jobs = newJobService(a.jobStore)
	a.Prompts = newPromptsService(cfg.Paths, cfg.SettingsStore, a.Webhooks)
	installer, err := skills.NewInstaller(filepath.Join(cfg.Paths.StateDir, "skills.json"))
	if err != nil {
		return nil, fmt.Errorf("load skills index: %w", err)
	}
	a.Skills = newSkillsService(a.Prompts, installer, cfg.SettingsStore, cfg.MockMode, cfg.Logger)
	a.Report = newReportService(cfg.Paths, cfg.SettingsStore, cfg.Build, cfg.ReportUploader, cfg.Logger)
	a.Terminals = newTerminalsService(a.terminals, tmuxcc.NopMetrics)

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
		a.outputs.Start(ctx)
	}
	a.retention.Start(ctx)
	// The engine starts before anything that can append to the log. Its flow
	// installation is synchronous, so by the time a producer tick, a webhook
	// delivery or a test harness can append, there is a runner ready to route
	// it — no window in which a wake-up has nothing to wake.
	if err := a.engine.Start(ctx); err != nil {
		return fmt.Errorf("start flow engine: %w", err)
	}
	if a.producer != nil {
		a.producer.Start(ctx)
	}
	if a.webhook != nil {
		if err := a.webhook.Start(ctx); err != nil {
			a.Webhooks.setStartError(err)
			a.logger.Warn().Err(err).Int("port", a.webhookPort).Msg("webhook listener unavailable")
		} else if a.webhookPort == 0 && !a.settings.EnvironmentOverridden(settings.EnvHTTPPort) {
			_, err := a.settingsStore.Update(func(persisted *settings.Settings) error {
				persisted.HTTP.Port = a.webhook.Port()
				return nil
			})
			if err != nil {
				a.Webhooks.setStartError(fmt.Errorf("persist allocated webhook port: %w", err))
				a.logger.Warn().Err(err).Msg("persist allocated webhook port")
			}
		}
	}
	// Re-sync already-installed agent skills so a moved config path or a new node
	// type re-renders itself without the user re-installing. It only touches files
	// already tracked in the index, so an empty index is a no-op; it is skipped in
	// mock/e2e runs so a fixture launch never writes into the real ~ skill dirs.
	if a.mock == "" && a.settings.Skills.AutoUpdate {
		go a.syncInstalledSkills()
	}
	return nil
}

func (a *App) syncInstalledSkills() {
	res, err := a.Skills.SyncInstalled(a.ctx)
	if err != nil {
		a.logger.Warn().Err(err).Msg("sync installed skills")
		return
	}
	if res.Updated+res.Restored > 0 {
		a.logger.Info().
			Int("updated", res.Updated).
			Int("restored", res.Restored).
			Int("modified", res.Modified).
			Msg("synced installed agent skills")
	}
}

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

// Close stops the background subsystems and releases resources. It is the
// single teardown path: the adapter's own shutdown hook covers only what it
// owns (a tray, a window).
//
// Each subsystem's own Stop/Close is idempotent (its own stopOnce; webhook's
// Stop below is the one that gained one, see docs/decisions/0016), so the
// hand-written sequence below — unchanged from before this phase — stays
// safe to call in this reverse-startup order even if something upstream
// already tore part of it down.
//
// A plugs.Manager (docs/architecture.md's "Background lifecycle") was
// evaluated to replace this sequence and declined for now: appkit/plugs
// v0.0.0-20260423210245's Manager.Start unconditionally calls
// signal.NotifyContext, which permanently adds Go's one-time os/signal
// watcher goroutine to the process (confirmed against the stdlib source —
// there is no way to opt out; passing no signals means "watch all signals,"
// not "watch none," per signal.Notify's own documented behavior) and that
// goroutine has nowhere to go before TestAppLifecycle's strict
// before-vs-after goroutine count runs. Revisit once either that test
// tolerates it or a plugs release makes signal registration optional.
func (a *App) Close() error {
	a.cancel()

	// Before the webhook listener: a terminal WebSocket has hijacked its
	// connection, which http.Server.Shutdown neither tracks nor closes, so the
	// socket has to be brought down by closing the streams behind it first
	// (ADR 0036). The context is a fresh one for the same reason Shutdown's is.
	if a.terminals != nil {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
		_ = a.terminals.Stop(stopCtx)
		cancel()
	}

	if a.webhook != nil {
		// A fresh, un-cancelled context for the graceful drain: a.ctx may
		// already be cancelled by the line above, and handing a Done context
		// to Shutdown would mean "stop now" instead of "you have this long
		// to drain." WithoutCancel keeps this a context derived from a.ctx
		// rather than a bare root, without inheriting a deadline that may
		// have already passed.
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
		// A shutdown failure only warns, matching Start's own bind-failure
		// policy: a slow or stuck drain must never fail Close outright.
		if err := a.webhook.Stop(stopCtx); err != nil {
			a.logger.Warn().Err(err).Msg("webhook listener shutdown")
		}
		cancel()
	}
	if a.producer != nil {
		a.producer.Stop()
	}
	a.engine.Stop()
	a.retention.Stop()
	a.outputs.Stop()
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

func buildGitHubConnection(mock string, client *ghclient.Client, creds credentials.Store, onChange func()) ghsource.Connection {
	switch mock {
	case "feed", "pipeline", "action-smoke":
		return ghsource.NewMockConnection(true, creds, onChange)
	case settings.MockOnboarding:
		return ghsource.NewMockConnection(false, creds, onChange)
	default:
		return ghsource.NewLiveConnection(client, creds, onChange)
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
	a.actionStore = actions.NewActionStore(path)
	if err := a.actionStore.Reload(); err != nil {
		logger.Warn().Err(err).Msg("actions.yml load failed; using last-good (likely empty) action set")
	}

	watcher, err := actions.NewActionsWatcher(path, func() {
		if err := a.actionStore.Reload(); err != nil {
			logger.Warn().Err(err).Msg("actions.yml reload failed")
		}
		count := len(a.actionStore.List())
		a.Events.Publish(a.ctx, events.ActionsUpdated{Count: count})
		// A hand edit (or the app's own write) reloaded actions.yml: record
		// the now-effective action count so the change is auditable.
		a.activityStore.Record(a.ctx, activity.ConfigReloaded("actions.yml", count))
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
	a.flowStore = flow.NewFlowStore(dir, actions.NewRefs(a.actionStore))

	watcher, err := flow.NewFlowsWatcher(dir, func() {
		if err := a.flowStore.Reload(); err != nil {
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
	a.engine.Wake()
	a.Events.Publish(a.ctx, events.LogAppended{NextOffset: nextOffset})
}

// PublishFlowsUpdated announces a change to the flow set and reinstalls the
// engine's runners against it. The app's own writes go through it too, so a
// save and an external edit are one path.
func (a *App) PublishFlowsUpdated(reason string) {
	a.engine.Reload()
	a.Events.Publish(a.ctx, events.FlowsUpdated{Reason: reason})
}

// RefreshSources drops the fetch caches and drives one producer tick, returning
// its summary. The engine commits on its own goroutine, so a caller reads back
// with a short retry. Mock modes have no producer and report KindUnavailable.
func (a *App) RefreshSources(ctx context.Context) (ingest.TickSummary, error) {
	if a.producer == nil {
		return ingest.TickSummary{}, Errorf(KindUnavailable, "source refresh is unavailable in this mode")
	}
	if a.fetchers != nil {
		a.fetchers.InvalidateAll()
	}
	return a.producer.Tick(ctx), nil
}

// MountAPI mounts h onto the loopback webhook listener at prefix so the HTTP API
// shares its port. It reports false when no listener exists. Call before Start.
func (a *App) MountAPI(prefix string, h http.Handler) bool {
	if a.webhook == nil {
		return false
	}
	a.webhook.MountAPI(prefix, h)
	return true
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
		Flows:   a.flowStore,
		Scripts: scripts,
		Logger:  logger,
		OnCommitted: func() {
			a.Events.Publish(a.ctx, events.InboxUpdated{})
		},
		OnFlowError: func(flowID string, err error) {
			a.activityStore.Record(a.ctx, activity.FlowRuntimeFailed(flowID, err))
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
	return ingest.NewResolver(a.flowStore, sourceFactories(a.fetchers, a.grafanaFetchers), logger)
}

// sourceFactories is the instance half of the connector registry. It is a
// function of its dependencies rather than a method so the bijection test can
// hold it against the descriptors without standing up an App.
func sourceFactories(fetchers *ghsource.Fetchers, grafanaFetchers *grafana.Fetchers) map[string]connector.Factory {
	factories := map[string]connector.Factory{
		webhook.Descriptor.Type: webhook.NewFactory(),
	}
	// Mock modes have no fetchers, so the GitHub connector has nothing to
	// construct instances over and is left out of the map: resolving one logs
	// and skips rather than dereferencing nil.
	if fetchers != nil {
		factories[ghsource.Descriptor.Type] = ghsource.NewFactory(fetchers)
	}
	// Always wired in a real build; the nil guard is only for the bijection test.
	if grafanaFetchers != nil {
		factories[grafana.MetricsDescriptor.Type] = grafana.NewMetricsFactory(grafanaFetchers)
		factories[grafana.AlertsDescriptor.Type] = grafana.NewAlertsFactory(grafanaFetchers)
	}
	return factories
}

// buildProducer starts nothing; it wires the event-log producer over every
// enabled pull-mode source node across all flows. Mock modes have no fetcher
// and therefore no producer.
func (a *App) buildProducer(logger zerolog.Logger) *ingest.Producer {
	if a.fetchers == nil {
		return nil
	}
	producer := ingest.NewProducer(a.Store, a.sources, a.pollInterval, a.PublishLogAppended, logger)
	producer.SetRecorder(a.activityStore)
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
	dispatcher := dispatch.NewDispatcher(outputExecutors(a.launcher, a.publisher, a.observedNotifier(cfg.Notifier), cfg.Gate, a.Store, cfg.Logger))
	worker := dispatch.NewWorker(a.Store, dispatch.NewFlowNotifyActions(a.flowStore, a.actionStore), dispatcher, dispatch.DefaultOutputWorkerInterval, cfg.Logger)
	worker.SetRecorder(a.activityStore)
	worker.SetJobRecorder(a.jobStore)
	return worker
}

// observedNotifier decorates the adapter's delivery port so the core learns
// when a notify terminal's delivery actually reaches the user. dispatch's
// NotifyExecutor calls this port directly and never touches the event bus —
// events carry payloads in the core, not in dispatch, which is why the wrap
// happens here instead: app.go is where Config's driven port enters the
// core. Only a successful delivery publishes: dispatch itself retries a
// failed one rather than recording it, so announcing on error would report a
// notification the user never saw. A nil port stays nil — NotifyExecutor's
// own nil check is what turns "no notifier configured" into a typed failure,
// and wrapping nil here would silently paper over that.
func (a *App) observedNotifier(next dispatch.SystemNotifier) dispatch.SystemNotifier {
	if next == nil {
		return nil
	}
	return systemNotifierFunc(func(ctx context.Context, in dispatch.SystemNotification) error {
		if err := next.Notify(ctx, in); err != nil {
			return err
		}
		profileID, _ := in.Data["profileId"].(string)
		itemID, _ := in.Data["itemId"].(int64)
		// a.ctx, not the incoming ctx, on purpose: every other publish in this
		// file uses the app's own lifetime rather than whatever call
		// triggered it (see the field doc on App.ctx), and a Buffer
		// subscriber's blocking wait should be bounded by "is the app still
		// running", not by a dispatch command's own deadline.
		//nolint:contextcheck // deliberate -- see comment above
		a.Events.Publish(a.ctx, events.NotificationRaised{
			ProfileID: profileID,
			ItemID:    itemID,
			Title:     in.Title,
			Body:      in.Body,
			Severity:  in.Severity,
			InApp:     in.InApp,
		})
		return nil
	})
}

// systemNotifierFunc adapts a plain function to dispatch.SystemNotifier, the
// same shape http.HandlerFunc gives http.Handler.
type systemNotifierFunc func(ctx context.Context, n dispatch.SystemNotification) error

func (f systemNotifierFunc) Notify(ctx context.Context, n dispatch.SystemNotification) error {
	return f(ctx, n)
}

// openWebhook constructs the optional loopback listener without binding it.
// Port zero is passed through to net.Listen so the OS allocates without a
// probe/rebind race. Mock instances only claim a listener through an explicit
// port override, keeping parallel e2e lanes isolated.
func (a *App) openWebhook(_ context.Context, cfg Config) {
	a.webhookHost = cfg.Settings.HTTP.Host
	a.webhookPort = cfg.Settings.HTTP.Port
	if !cfg.Settings.HTTP.Enabled {
		return
	}
	if cfg.MockMode != "" && !cfg.Settings.EnvironmentOverridden(settings.EnvHTTPPort) {
		return
	}

	a.webhook = webhook.NewListener(a.Store, a.sources.PushInstances, a.webhookHost, a.webhookPort, a.PublishLogAppended, cfg.Logger)
	a.webhook.SetRecorder(a.activityStore)
}

// openHiveRuntime opens the Hive dependencies desktop actions need. The
// desktop keeps its own database, while sessions and internal events
// intentionally use Hive's shared state and event bus.
func (a *App) openHiveRuntime(ctx context.Context, cfg Config) error {
	dataDir := cfg.Paths.HiveDataDir
	if dataDir == "" {
		dataDir = cfg.Paths.DataDir
	}
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
	exec := newTmuxExecutor(&executil.RealExecutor{}, a.tmux)
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

	a.launcher = dispatch.NewHiveSessionLauncher(sessions)
	a.launcher.SetRecorder(a.activityStore)
	a.sessions = dispatch.NewHiveSessionManager(sessions)
	a.publisher = dispatch.NewHiveMessagePublisher(hive.NewMessageService(stores.NewMessageStore(database, hiveCfg.Messaging.MaxMessages), hiveCfg, bus))
	return nil
}
