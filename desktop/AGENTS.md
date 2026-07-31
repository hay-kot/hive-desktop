# Agent Instructions — Hive Desktop

Scope: the `desktop/` Wails app, its Wails adapter under
`internal/adapter/wailsui/**`, and the headless core under `internal/app/**`.
The repository-root `AGENTS.md` still applies (git standards, quality gates,
landing-the-plane). `desktop/README.md` is the long-form reference — native
shell, pinned versions, parent-module adaptations, icons, and the flows/actions
data model. **Read `README.md` before changing native-shell, build, or icon
wiring; do not duplicate its detail here.**

**Read [`../docs/architecture.md`](../docs/architecture.md) before adding a
subsystem, an entrypoint, or an extension point.** It names the pattern each
part of the app follows and maps "what you are building" to the section that
specifies it. This file describes how the code is arranged *today*;
`architecture.md` describes the shape it is moving to. Where they disagree,
`architecture.md` wins for new work — the differences are called out under
[Patterns and gotchas](#patterns-and-gotchas).

## What this app is

A Wails v3 desktop shell (Vue 3 + TypeScript frontend, Go backend) that renders
a GitHub-backed feed. A **flow** (`flows/*.yaml`) wires `sources.github` nodes
through filters into `feed`, `action`, and `notify` terminals; a background
producer polls sources, appends to an event log, and commits durable
`feed_item` rows the sidebar reads. `action` and `notify` nodes emit durable
`output_command`s that an output worker dispatches (`launch-session`, `shell`,
`publish-message`, `notify`). GitHub is a **connector, not a login**: its
credential is acquired by an OAuth device flow with a PAT fallback and stored
in the OS keychain, and nothing in the app is gated on holding one.

## Code layout

Go — `desktop/` is `main()` and nothing else; every Wails service lives in the
adapter, and the logic they call lives in the core:

```
desktop/
  main.go                 # bootstrap + wiring: build the core, mount the adapter, run
  buildinfo.go            # -X main.version stamping; must stay in package main
  build/                  # platform Taskfiles, config.yml, icon masters, scripts
  e2e/                    # Docker-only Playwright harness (fixtures, scripts, tests)
  frontend/               # Vue 3 + TS + Vite + Tailwind v4
internal/adapter/wailsui/ # the driving adapter — the only package importing Wails
  *service.go             # Wails service structs exposed to the frontend (RPC surface)
  events.go               # event registration + the emit* wake-up signals
  notify.go tray.go updater.go release.go focusstate.go
  e2e/                    # the server-side half the Playwright suite drives
internal/app/             # the headless core — no transport, no Wails
  settings/               # env-var surface, data/config/flows/actions paths, settings.yaml
  credentials/            # Ref{Provider,Account} -> keychain value, + a ref index
  store/                  # sqlc-backed SQLite: event log, inbox_item, output_command
  flow/                   # flow YAML parse/validate/save, FlowsWatcher, sidebar
    docs/                 # per-node-type markdown — ALSO the frontend's node help
  actions/                # actions.yml store, watcher, seed, editable model, Refs
    docs/                 # per-action-type markdown, rendered into the prompt
  ingest/                 # the producer loop and retention: sources -> event log
    resolver.go           # the flow set -> live connector instances
  runtime/                # the flow engine: index a flow, run a batch, commit
    js/                   # the ScriptRuntime port's goja implementation
    testdata/parity/      # fixture flows + expected commits (see Testing)
  dispatch/               # output worker, dispatcher, executors
  icons/                  # the curated feed glyph set (a leaf: flow + webhook)
  sources/                # the connector registry — registry.go is the whole map
    connector/            # the vocabulary a connector is declared in
    github/               # the GitHub connector; feed/ is its fetch layer,
                          #   ghclient/ its owned HTTP client (ADR 0015 —
                          #   nothing outside internal/hivecore imports the
                          #   vendored github.Client anymore)
    webhook/              # the webhook connector and its local ingress
  activity/ jobs/ prompts/
```

The dependency rule is enforced, not just documented: `depguard` fails any
`internal/app` package that imports Wails or `internal/adapter`.

Frontend (`frontend/src/`): `App.vue` + `components/` (feed UI), `composables/`
(`useFeedState`, `useGitHubConnection`, …), `pipeline/` (the flow editor — canvas, node
palette, node editors), `lib/` (presentation
helpers), `types/`. TS bindings to Go services are **generated** into
`frontend/bindings/` — see Code generation.

**Flow execution is Go's, and nothing about it lives here.**
`internal/app/runtime` owns graph execution and `runtime.Engine` (a field on
`App`) drives it: it installs a runner per enabled flow at startup, reinstalls
on a flows change, and drains the event log on every append — all with this
window closed (ADRs 0010, 0011). **Do not add node execution logic to the
frontend.** A new node
type gets its editor (`nodes/<type>/{config.ts,editor.vue,index.ts}`) here,
and its schema, validation, docs *and execution* in Go. See `architecture.md`
▸ Execution model. `pipeline/__tests__/import-hygiene.spec.ts` fails if a
`nodes/*/runtime.ts` reappears.

**A source connector is declared in Go and adding one barely touches this
directory.** `internal/app/sources` holds a `connector.Descriptor` per
connector — type, title, credentials provider, pull/push mode, stability,
capabilities, config schema — and `flow`'s node registry, `runtime`'s
behaviour registry and **Settings ▸ Integrations** all *derive* their source
entries from it (ADR 0012). Source node types are namespaced:
`sources.github`, `sources.webhook`. A new connector still needs a
`nodes/<type>/` editor entry here until forms are schema-driven, but nothing
else — it gets its Integrations card for free, and `useIntegrations` supplies
its connected accounts to any editor that needs an account picker.

The frontend learns that a run landed from **`inbox:updated`**, not
`log:appended`. The log growing only says a source observed something, which
may route nowhere; `inbox:updated` fires after the engine has committed, which
is the moment membership claims and inbox items are readable.

## Development

Drive everything through the **root** mise tasks (canonical entry points):

```bash
mise run desktop:dev         # Run Wails directly with this worktree's launch.env
mise run desktop:dev:prepare # Create/reuse the isolated instance and launch.env
mise run desktop:dev:fresh   # Safely reseed the instance and regenerate launch.env
mise run desktop:dev:reset   # Safely remove the marked instance and launch.env
mise run desktop:serve     # headless HTTP server build on localhost:8080 (agent UI loop)
mise run desktop:build     # build the app (macOS emits desktop/bin/hive-desktop)
mise run desktop:generate  # regenerate frontend TS bindings after Go service changes
mise run desktop:icons     # regenerate committed icon assets from SVG masters
mise run desktop:test      # frontend vitest + Go tests (unit)
mise run desktop:e2e       # Docker-only Playwright regression gate
mise run devserver         # the shared GitHub proxy desktop:dev routes through
```

`desktop:dev` goes through `cmd/devserver` by default — `launch.env` carries the
API base, and one proxy serves every worktree so concurrent streams share a
rate-limit budget and a response cache (ADR 0017). Leave `mise run devserver`
running; starting a second parks it as a standby that takes over if the first
stops. Nothing preflights the proxy — if it is not answering, GitHub calls fail
as transport errors in the log. To use real GitHub, set
`HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE=""` in the gitignored `overrides.env`.

`solo up` brings the session up from the checked-in `.solo.yml` (devserver + the
app) and `solo down` tears it down.

Go lint/format is the root `mise run lint` (golangci-lint); frontend type
errors surface via `vue-tsc` in the build. Run quality gates after changes.

### Manual / UI verification

Use the **headless server build** for the agent UI loop — never a local GUI
build:

```bash
mise run desktop:serve                       # serves at http://localhost:8080
HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE=onboarding mise run desktop:serve
```

`onboarding` mode reads its flows from a fresh scratch directory rather than
the real config root, so it shows first run even on a machine that already has
workspaces, and the walk cannot touch them. The directory is per-process, so a
`desktop:dev` rebuild — which any Go edit triggers — starts the walk over. Set
`HIVE_DESKTOP_FLOWS_DIR` to opt out and point it at a fixture set instead.

Drive it with Playwright/browser tooling, read screenshots under
`desktop/e2e/screenshots`, edit, repeat. Assets are `//go:embed`ded, so
frontend edits require re-running `desktop:serve`; for a fast frontend loop use
`desktop:dev` (Vite HMR). Native-shell behavior (Dock icon, traffic-light
centering, close-hides-window, tray menu, template-icon tinting) is a **manual**
verification concern — it cannot be checked headlessly.

## Testing

- **Unit** (`mise run desktop:test`): Go logic (`go test ./desktop/...
  ./internal/app/... ./internal/adapter/...`) + frontend `vitest`. `store` and
  `runtime` tests use real SQLite. This is the default gate for
  backend/frontend changes.
- **Engine fixtures** (`internal/app/runtime/testdata/parity/*.json`): a flow, a
  batch of log messages, and the exact `CommitBatch` they are worth. A change
  to routing, sink tagging or node-run accounting belongs in one of these; they
  are cheaper to read than the engine and they were the proof the port off the
  browser engine was faithful (ADR 0011).
- **E2E** (`mise run desktop:e2e`): **Docker-only.** Builds the digest-pinned
  Go/Playwright image in `desktop/e2e/Dockerfile` and runs Playwright inside it
  against private feed / onboarding / pipeline / action-smoke server instances.

**CRITICAL: never run Playwright or the e2e harness on the host.** `run-docker.sh`
mints a fresh 256-bit harness marker that both the image command and the server
launcher require, so host Playwright cannot start the servers. There is no host
fallback — Docker must be available. Each server gets a private data/config root
so parallel projects never mutate checked-in fixtures or share SQLite state.

## Code generation — never edit generated files by hand

- **sqlc** (`internal/app/store/`): queries in `queries/`,
  migrations in `migrations/*.up.sql`. Regenerate with the root `mise run
  generate`; `models.go` and `queries.sql.go` are committed and generated.
  Commit generated output alongside the SQL change.
- **Wails TS bindings** (`frontend/bindings/`): after changing a Wails service
  method or its types, run `mise run desktop:generate`. Bindings **must** be
  generated with the working directory at `desktop/` so the Wails CLI treats it
  as the app package while Go walks up to the parent module. Binding method IDs
  hash the Go package path, so *moving* a service invalidates them too —
  `mise run check:bindings` (also a CI step) is what catches that. The Vite
  plugin and typed events depend on these — a stale binding is a frontend type
  error.

## Patterns and gotchas

These describe the code **as it is today**. For new work follow
`../docs/architecture.md` — extending a superseded pattern makes the migration
more expensive, which is the whole reason it is being done now.

- **Single Go module.** `desktop/` has no `go.mod`; it is the
  `github.com/hay-kot/hive-desktop/desktop` package inside the root module. Because
  the package is `main` and named `desktop`, you **must** give the binary an
  explicit output path — `go build -o ./desktop/bin/hive-desktop ./desktop`
  (add `-tags server` for the headless variant). A bare `go build ./desktop`
  collides with this directory. The mise tasks already do this correctly.
- **No `init()`.** This repo enables `gochecknoinits`. Event registration in
  `wailsui/events.go` uses package-variable initialization
  (`var _ = registerEvents()`), not `init()`. Follow that pattern.
- **The core publishes payloads; the Wails boundary degrades them to wake-up
  signals.** `app/events` carries typed payloads (`LogAppended{NextOffset}`,
  `JobsUpdated{JobID}`, …) and `wailsui.Subscribe` maps each one to the Wails
  event the frontend already knows — `connection:updated`, `log:appended`,
  `flows:updated`, `actions:updated`. On receipt the frontend re-reads the
  relevant service; the signal just says "something changed".

  `inbox:updated` is the one that matters most for the feed: the flow engine
  publishes `InboxUpdated` after it commits, and that — not `log:appended` — is
  when membership claims and inbox items are readable. A log row may route
  nowhere at all.

  Adding an event is three things: a payload type in `app/events/events.go`
  (its `eventName` method is unexported, so an adapter cannot invent one), a
  publish from the core, and a subscriber in `wailsui/events.go` that
  registers the Wails event and emits it. The `emit*` helpers are unexported
  and stay that way — the core must never call one, and `forbidigo` fails the
  build if it does.

  Every `wailsui` subscription uses `events.Coalesce()`: the frontend re-reads
  on receipt, so a dropped intermediate is not observable, and a busy webview
  must never hold up the producer goroutine that published. A consumer that
  needs every event in order uses `events.Buffer(n)` instead — the delta is
  in the payload, which is why an MCP or streaming consumer does not have to
  "re-read the service". See `architecture.md` ▸ Events.
- **Mock modes** (`HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE`): `feed`/`pipeline`/`action-smoke` start
  with `github/octocat` connected; `onboarding` starts with no workspaces and
  nothing connected, and its fake device flow grants after ~1.5s — the two
  together are what make it the first-run mode. Unset → live backends. A mock
  connection must write the credential it pretends to hold, not just flip a
  status flag: everything that resolves an account off the credential store
  works live and fails in mock mode otherwise. In mock modes the live producer
  and output-worker background loop are skipped; `feed`/`action-smoke` seed
  fixed `feed_item` rows (see `mockseed.go`). Use these for deterministic
  offline/e2e runs — do not hit real GitHub in tests.
- **Config vs data split.** User-editable config (flows, `actions.yml`,
  `settings.yaml`) lives in `$XDG_CONFIG_HOME/hive/desktop/` (so it can live in
  a dotfiles repo); app-local state lives under the data root's `desktop/`
  directory. Startup resolves safe defaults → strict YAML validation →
  `HIVE_DESKTOP_*` overrides → effective-value validation, and injects one
  immutable path snapshot. The fixed
  `bootstrap.yaml` stores only `data_dir` and `config_dir`; explicit desktop
  path overrides win. The canonical safe settings shape is:

  ```yaml
  polling: {interval: 5m}
  updates: {enabled: true, channel: ""}
  notifications: {enabled: true, delivery: auto, sound: true}
  appearance: {theme: "", terminal_font_size: "", terminal_font_family: "", terminal_font_weight: 0, terminal_font_weight_bold: 0, terminal_show_windows: true, terminal_pool_size: 3}   # terminal_font_size: small/medium/large/xl/xxl, "" = medium; terminal_font_family: an installed monospace family, "" = the bundled face; terminal_font_weight/_bold: 300/350/400/600/700, 0 = the defaults 350/700 (ADR 0050); terminal_show_windows lists every session's windows in the terminal sidebar; terminal_pool_size is how many sessions stay attached for instant switching (1-6, ADR 0042)
  http: {enabled: true, host: 127.0.0.1, port: 0}   # loopback server: webhook listener + agent API (ADR 0021)
  keybindings: {}
  paths: {tmux: ""}                 # absolute path to tmux; "" discovers it (ADR 0039)
  experimental: {terminal: false}   # ships-dark opt-ins, read at startup; terminal mode (ADR 0037)
  development:
    mocks: {mode: live}
    instance: {id: ""}
    github: {api_base: ""}   # loopback-only devserver override (ADR 0017)
    vite: {host: 127.0.0.1, port: 0}
    wails: {host: 127.0.0.1, port: 0}
    pprof: {enabled: false}   # mounts on the loopback HTTP server when on (ADR 0023)
    debug: {pause_ingest: 0s, pause_commit: 0s}
  ```

  The loopback HTTP server (webhook listener + agent API) is on by default and
  allocates directly through port `0`. Pprof is off by default; when enabled it
  mounts `/debug/pprof/` on that same server (`httpapi.PprofHandler`, ADR 0023),
  so it has no address of its own and needs `http.enabled`. Dev uses
  `cmd/devtools` plus the gitignored worktree-local `.hive-desktop/`; normal
  runs reuse it, while `desktop:dev:fresh` and `desktop:dev:reset` are
  marker-guarded destructive operations that refuse while a configured dev
  server is active. `prepare` writes non-secret `launch.env`; the `desktop:dev`
  mise task loads it followed by optional gitignored `overrides.env`, then
  starts Wails through `devtools run`, which owns the session's teardown so a
  closed terminal cannot leave the app running (ADR 0046).
  Data/config/ports are isolated, but the OS keychain,
  the fixed bootstrap pointer, and `hive.db` are shared: dev sets
  `HIVE_DESKTOP_HIVE_DATA_DIR` to the installed hive data dir so sessions created
  in dev land in the real hive database (desktop-pipeline.db and feed state stay
  worktree-isolated). Set it to the worktree data dir in `overrides.env` to
  re-isolate. e2e leaves it unset, so its hive.db stays isolated.
- **Flows/actions are code, hot-reloaded and last-good.** Flow parsing is strict
  and validated by Go on save/deploy (unique node ids, known types, source
  limits within GitHub caps, action refs that exist, valid wires). `FlowsWatcher`
  and `ActionsWatcher` watch the *directory* (so atomic editor saves work) and
  reload live; a broken file keeps the **last-good** set rather than blanking
  the running app. The app's own SaveFlow/SaveLayout writes intentionally
  trigger the same reload + wake-up.
- **Keychain / secrets.** Credentials go through `app/credentials`, keyed by
  `Ref{Provider, Account}` — values in the OS keychain, refs in a JSON index
  beside the state dir because keychains cannot enumerate. The keychain is the
  truth and the index is a cache: a ref present in the index but absent from
  the keychain reads as `ErrNotFound` and is pruned. `HIVE_GITHUB_TOKEN`
  overrides every `github/*` credential for headless runs
  (`credentials.EnvOverrideName` derives it from the provider name);
  `HIVE_GITHUB_CLIENT_ID` overrides the device-flow client id. Never log or
  persist credential values elsewhere.

  Two rules govern new work: **config holds refs, never tokens** (`flows/` is
  dotfiles-managed, so an embedded token is a token in a git repo), and
  **GitHub is a connector, not a login** — do not add code that gates the app
  on being connected to GitHub. Lookup is generic and lives in
  `app/credentials`; only *acquisition* is provider-specific and lives with
  the connector (`sources/github/connect.go`). See `architecture.md` ▸
  Credentials and docs/decisions/0013.

  The vendored `internal/hivecore/github/token.go` single-slot store is
  untouched and unused by the app. Do not reach for it.
- **First run creates a workspace before it offers an account.** The order is
  create workspace → connect GitHub → feed, and the connect step is skippable
  past a warning. A workspace created with no account connected has an
  *empty* graph, which is a valid flow — a source node names the credential
  it fetches as, so there is no unconfigured source node to stand in for one.
  `FlowsService.SeedStarter` is what fills it in once an account exists, and
  `flow.FlowStore.Create` takes its starter graph from its caller so `flow`
  names no connector.
- **LLM prompts are Go-owned** (docs/decisions/0009). All prompt text lives in
  `internal/app/prompts/templates/`; nothing in the frontend builds a
  prompt string. Adding one is a template plus a `definitions` entry — Settings
  ▸ LLM prompts lists whatever the registry reports. Per-type prose belongs in
  `flow/docs/<type>.md` / `actions/docs/<type>.md`, never in a prompt template,
  and a registry↔docs bijection test enforces that a new type documents itself.
- **Node docs are shared across the language boundary.** The frontend imports
  `internal/app/flow/docs/*.md` through the `@nodedocs` Vite alias
  rather than keeping a copy — it is declared in **both** `vite.config.ts` and
  `vitest.config.ts`, each with a matching `server.fs.allow` entry (the files
  sit outside the Vite root). These docs are read by the node drawer *and* by
  an LLM, so keep them free of UI-only references like "the row below".

## Environment variables

Desktop-owned configuration uses `HIVE_DESKTOP_<NAMESPACE>_<FIELD>`. Every
value is parsed and validated; settings overrides are process-local and are not
persisted by UI writes.

| Var | Purpose |
| --- | --- |
| `HIVE_DESKTOP_DATA_DIR` | Desktop data root |
| `HIVE_DESKTOP_HIVE_DATA_DIR` | Override only the hive.db data dir (defaults to the data root). `desktop:dev` points it at the installed hive data dir so dev sessions land in the real hive database; desktop state stays worktree-isolated |
| `HIVE_DESKTOP_CONFIG_DIR` | Desktop config root |
| `HIVE_DESKTOP_FLOWS_DIR` | Override only `flows/` |
| `HIVE_DESKTOP_ACTIONS_PATH` | Override only `actions.yml` |
| `HIVE_DESKTOP_LOG_LEVEL` | Root logger level |
| `HIVE_DESKTOP_POLLING_INTERVAL` | Pull-source interval (minimum `60s`) |
| `HIVE_DESKTOP_UPDATES_ENABLED` | Enable update checks |
| `HIVE_DESKTOP_UPDATES_CHANNEL` | `stable`, `beta`, or `dev` |
| `HIVE_DESKTOP_NOTIFICATIONS_ENABLED` | Enable notifications |
| `HIVE_DESKTOP_NOTIFICATIONS_DELIVERY` | `auto`, `system`, or `app` |
| `HIVE_DESKTOP_NOTIFICATIONS_SOUND` | Enable notification sound |
| `HIVE_DESKTOP_APPEARANCE_THEME` | Frontend theme id |
| `HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_SIZE` | Terminal font size preset (`small`/`medium`/`large`/`xl`/`xxl`); empty means medium |
| `HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_FAMILY` | Terminal font family — any installed monospace family; empty is the bundled CaskaydiaMono Nerd Font (ADR 0050) |
| `HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_WEIGHT` | Weight normal terminal text draws at (`300`/`350`/`400`/`600`/`700`); `0` means the default, 350 |
| `HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_WEIGHT_BOLD` | Weight bold terminal text draws at; `0` means the default, 700 |
| `HIVE_DESKTOP_APPEARANCE_TERMINAL_SHOW_WINDOWS` | List every active session's windows in the terminal sidebar, not just the attached one's; on by default |
| `HIVE_DESKTOP_APPEARANCE_TERMINAL_POOL_SIZE` | Sessions the terminal view keeps attached for instant switching (1-6, ADR 0042); values outside the range read as the default, 3 |
| `HIVE_DESKTOP_HTTP_ENABLED` | Enable the loopback HTTP server (webhook listener + agent API); on by default |
| `HIVE_DESKTOP_HTTP_HOST` | HTTP loopback host |
| `HIVE_DESKTOP_HTTP_PORT` | HTTP port; `0` asks the OS to allocate |
| `HIVE_DESKTOP_PATHS_TMUX` | Absolute path to tmux, skipping discovery (ADR 0039); empty searches `$PATH` then the usual package-manager prefixes |
| `HIVE_DESKTOP_EXPERIMENTAL_TERMINAL` | Opt into terminal mode (ships dark, ADR 0037); off by default, read at startup |
| `HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE` | `live`, `feed`, `pipeline`, `action-smoke`, or `onboarding` |
| `HIVE_DESKTOP_DEVELOPMENT_INSTANCE_ID` | Optional development instance label |
| `HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE` | Point the GitHub REST/GraphQL base at `cmd/devserver` (dev caching proxy + event simulator, ADR 0017). **Set by `launch.env` — `desktop:dev` is proxied by default**; set it empty in `overrides.env` to use real GitHub. Loopback-only, validated. Applies to both the fetch layer and the connect flow; the OAuth device flow still goes to github.com |
| `HIVE_DESKTOP_DEVELOPMENT_VITE_HOST` | Dev Vite host; currently must be `127.0.0.1` because Wails constructs a localhost frontend URL |
| `HIVE_DESKTOP_DEVELOPMENT_VITE_PORT` | Dev Vite port; `0` preselects a free port |
| `HIVE_DESKTOP_DEVELOPMENT_WAILS_HOST` | Dev Wails loopback host |
| `HIVE_DESKTOP_DEVELOPMENT_WAILS_PORT` | Dev Wails port; `0` preselects a free port |
| `HIVE_DESKTOP_DEVELOPMENT_PPROF_ENABLED` | Mount `/debug/pprof/` on the loopback HTTP server (ADR 0023); off by default, needs `http.enabled` |
| `HIVE_DESKTOP_DEVELOPMENT_DEBUG_PAUSE_INGEST` | Ingestion crash-window delay |
| `HIVE_DESKTOP_DEVELOPMENT_DEBUG_PAUSE_COMMIT` | Commit crash-window delay |
| `HIVE_DESKTOP_DEVTOOLS_LOG_LEVEL` | `cmd/devtools` console verbosity (default `info`) |
| `HIVE_DESKTOP_E2E_HARNESS` | Marker-gates Docker-only e2e routes |

External boundaries keep their own names: `XDG_*` locates defaults;
`WAILS_SERVER_HOST`/`PORT` and `WAILS_VITE_HOST`/`PORT` belong to the
framework and receive the dev-launcher bridge; `HIVE_GITHUB_TOKEN` and
`HIVE_GITHUB_CLIENT_ID` are credential/provider inputs; build, release-secret,
and vendored Hive variables are not desktop settings.
