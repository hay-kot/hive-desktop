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
a GitHub-backed feed. A **flow** (`flows/*.yaml`) wires `github-source` nodes
through filters into `feed`, `action`, and `notify` terminals; a background
producer polls sources, appends to an event log, and commits durable
`feed_item` rows the sidebar reads. `action` and `notify` nodes emit durable
`output_command`s that an output worker dispatches (`launch-session`, `shell`,
`publish-message`, `notify`). Auth is a GitHub OAuth device flow with a PAT
fallback, tokens in the OS keychain.

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
  auth/                   # device-flow + PAT auth backends
  store/                  # sqlc-backed SQLite: event log, inbox_item, output_command
  flow/                   # flow YAML parse/validate/save, FlowsWatcher, sidebar
    docs/                 # per-node-type markdown — ALSO the frontend's node help
  actions/                # actions.yml store, watcher, seed, editable model, Refs
    docs/                 # per-action-type markdown, rendered into the prompt
  ingest/                 # the producer loop and retention: sources -> event log
  dispatch/               # output worker, dispatcher, executors
  sources/github/         # the GitHub connector; feed/ is its fetch layer
  sources/webhook/        # the local webhook ingress
  activity/ jobs/ prompts/
```

The dependency rule is enforced, not just documented: `depguard` fails any
`internal/app` package that imports Wails or `internal/adapter`.

Frontend (`frontend/src/`): `App.vue` + `components/` (feed UI), `composables/`
(`useFeedState`, `useAuth`, …), `pipeline/` (the flow editor — canvas, node
palette, in-browser graph engine in `pipeline/engine/`), `lib/` (presentation
helpers), `types/`. TS bindings to Go services are **generated** into
`frontend/bindings/` — see Code generation.

**The in-browser graph engine is being removed.** `pipeline/engine/`,
`driver.ts`, `processors.ts` and `nodes/*/runtime.ts` currently own topological
execution, filter matching and function-node evaluation — Go ingests and
executes, but the browser routes. That split makes the desktop window a hard
dependency of flow execution and puts the correctness-critical parts out of
reach of any headless surface, so the engine moves to Go. **Do not add node
execution logic to the frontend.** A new node type gets its editor
(`nodes/<type>/{config.ts,editor.vue,index.ts}`) in the frontend and its
schema, validation, docs and — once the Go engine lands — its execution in Go.
See `architecture.md` ▸ Execution model.

## Development

Drive everything through the **root** mise tasks (canonical entry points):

```bash
mise run desktop:dev       # wails3 dev — live frontend (Vite HMR) + Go; picks a free port;
                           # runs on an ephemeral copy of the local data dir (HIVE_DATA_DIR overrides)
mise run desktop:serve     # headless HTTP server build on localhost:8080 (agent UI loop)
mise run desktop:build     # build the app (macOS emits desktop/bin/hive-desktop)
mise run desktop:generate  # regenerate frontend TS bindings after Go service changes
mise run desktop:icons     # regenerate committed icon assets from SVG masters
mise run desktop:test      # frontend vitest + Go tests (unit)
mise run desktop:e2e       # Docker-only Playwright regression gate
```

Go lint/format is the root `mise run lint` (golangci-lint); frontend type
errors surface via `vue-tsc` in the build. Run quality gates after changes.

### Manual / UI verification

Use the **headless server build** for the agent UI loop — never a local GUI
build:

```bash
mise run desktop:serve                       # serves at http://localhost:8080
HIVE_DESKTOP_MOCK=onboarding mise run desktop:serve   # drive the signed-out first-run screen offline
```

Drive it with Playwright/browser tooling, read screenshots under
`desktop/e2e/screenshots`, edit, repeat. Assets are `//go:embed`ded, so
frontend edits require re-running `desktop:serve`; for a fast frontend loop use
`desktop:dev` (Vite HMR). Native-shell behavior (Dock icon, traffic-light
centering, close-hides-window, tray menu, template-icon tinting) is a **manual**
verification concern — it cannot be checked headlessly.

## Testing

- **Unit** (`mise run desktop:test`): Go logic (`go test ./desktop/...
  ./internal/app/... ./internal/adapter/...`) + frontend `vitest`. `store`
  tests use real SQLite. This is the default gate for backend/frontend
  changes.
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

These describe the code **as it is today**. Two are being replaced and are
marked as such. For new work follow `../docs/architecture.md` — extending a
superseded pattern makes the migration more expensive, which is the whole
reason it is being done now.

- **Single Go module.** `desktop/` has no `go.mod`; it is the
  `github.com/hay-kot/hive-desktop/desktop` package inside the root module. Because
  the package is `main` and named `desktop`, you **must** give the binary an
  explicit output path — `go build -o ./desktop/bin/hive-desktop ./desktop`
  (add `-tags server` for the headless variant). A bare `go build ./desktop`
  collides with this directory. The mise tasks already do this correctly.
- **No `init()`.** This repo enables `gochecknoinits`. Event registration in
  `wailsui/events.go` uses package-variable initialization
  (`var _ = registerEvents()`), not `init()`. Follow that pattern.
- **Frontend events are wake-up signals, not payloads.** `wailsui/events.go`
  registers `auth:updated`, `log:appended`, `flows:updated`, `actions:updated`. On
  receipt the frontend re-reads the relevant service; the event just says
  "something changed" (only `log:appended` carries meaningful data — the new
  tail offset). Adding a new signal means registering it in `registerEvents()`
  and adding an `emit*` helper.

  **Being replaced.** The wake-up contract is right for a GUI and stays *at the
  Wails boundary*, but it is moving out of the core: `app/events` will carry
  typed payloads and the `wailsui` adapter will degrade them to these same
  signals. An MCP client cannot cheaply "re-read the service", and a streaming
  consumer needs the delta. New core events carry a payload; no new
  package-level `emit*` functions. See `architecture.md` ▸ Events.
- **Mock modes** (`HIVE_DESKTOP_MOCK`): `feed`/`pipeline`/`action-smoke` start
  authenticated; `onboarding` starts signed out with a fake device flow that
  grants after ~1.5s. Unset → live backends. In mock modes the live producer
  and output-worker background loop are skipped; `feed`/`action-smoke` seed
  fixed `feed_item` rows (see `mockseed.go`). Use these for deterministic
  offline/e2e runs — do not hit real GitHub in tests.
- **Config vs data split.** User-editable config (flows, `actions.yml`) lives in
  `$XDG_CONFIG_HOME/hive/desktop/` (so it can live in a dotfiles repo);
  app-local state (`feed_item`, read state, event-log offsets, queued output
  commands) lives in the data dir's `desktop/` subdirectory. Respect that
  boundary when adding persistence.
- **Flows/actions are code, hot-reloaded and last-good.** Flow parsing is strict
  and validated by Go on save/deploy (unique node ids, known types, source
  limits within GitHub caps, action refs that exist, valid wires). `FlowsWatcher`
  and `ActionsWatcher` watch the *directory* (so atomic editor saves work) and
  reload live; a broken file keeps the **last-good** set rather than blanking
  the running app. The app's own SaveFlow/SaveLayout writes intentionally
  trigger the same reload + wake-up.
- **Keychain / secrets.** Tokens go through the OS keychain via
  `github.NewKeychainStore()`. `HIVE_GITHUB_TOKEN` is a read-only headless
  override; `HIVE_GITHUB_CLIENT_ID` overrides the device-flow client id. Never
  log or persist tokens elsewhere.

  **Being replaced.** That store is single-slot — the vendored
  `internal/hivecore/github/token.go` pins its keychain account to a fixed
  constant, so it has no provider or account dimension and cannot hold a second
  provider's credentials. It becomes `app/credentials` keyed by
  `Ref{Provider, Account}`, with a separate index of refs because keychains
  cannot enumerate. Two rules already apply to new work: **config holds refs,
  never tokens** (`flows/` is dotfiles-managed, so an embedded token is a token
  in a git repo), and **GitHub is a connector, not a login** — do not add code
  that gates the app on being signed in to GitHub. See `architecture.md` ▸
  Credentials.
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

Defined in `internal/app/settings/paths.go` unless noted:

| Var | Purpose |
| --- | --- |
| `HIVE_DESKTOP_MOCK` | Select a deterministic offline backend (`feed`/`pipeline`/`action-smoke`/`onboarding`) |
| `HIVE_DESKTOP_CONFIG` | Override the config root (holds `flows/` + `actions.yml`) |
| `HIVE_DESKTOP_FLOWS` | Override just the flows directory |
| `HIVE_DESKTOP_ACTIONS` | Override the `actions.yml` path |
| `HIVE_DATA_DIR` | App-local state root (shared with the CLI convention) |
| `HIVE_GITHUB_TOKEN` | Read-only headless auth override |
| `HIVE_GITHUB_CLIENT_ID` | Override the device-flow OAuth client id |
| `HIVE_DESKTOP_WEBHOOK_PORT` | Override the local webhook listener port (settings.yaml `webhook_port`, randomly allocated from 20000–32767 on first run); in mock modes the listener starts only when this is set |
| `WAILS_SERVER_PORT` | Server-build port (default 8080) |
| `WAILS_VITE_PORT` | Dev Vite port (auto-picked free port otherwise) |
