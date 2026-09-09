# Hive desktop shell

This directory is the Wails v3 Vue + TypeScript shell for Hive.

## Native shell

On macOS, the shell uses `application.MacOptions.ActivationPolicy` set to
`application.ActivationPolicyRegular`, so Hive is a regular app with a Dock
icon. It also sets `ApplicationShouldTerminateAfterLastWindowClosed: false`,
keeping the application alive after its window closes.

The visible 1360×864 Hive window uses native hidden-inset titlebar chrome: a
`MacTitleBar` with `AppearsTransparent`, `HideTitle`, `FullSizeContent`,
`UseToolbar`, and `HideToolbarSeparator`, pinned to
`MacToolbarStyleUnifiedCompact` so AppKit cannot drift the toolbar height
across macOS versions, and `InvisibleTitleBarHeight: 42` so the traffic
lights center on the 42px HTML titlebar.

Closing the window hides it rather than destroying it, via a `WindowClosing`
hook registered with `RegisterHook`. It must be a hook, not `OnWindowEvent`:
hooks run synchronously before listeners, so `Cancel()` reliably aborts
Wails' internal window-destroy listener, which otherwise races the callback
in a separate goroutine. The app keeps running; reopen the window from the
Dock (`ApplicationShouldHandleReopen` calls `Show`) or from the tray menu.

The tray is a template icon with a dynamic menu: **Show Hive** calls
`window.Show()` and `window.Focus()`, profile checkboxes toggle whether each
flow polls and runs, and **Quit** calls `app.Quit()`. Invalid flow files remain
visible as disabled menu items. The menu is rebuilt after flow changes so
external YAML edits and in-app toggles stay synchronized. In the pinned Wails
release, `SystemTray.SetTemplateIcon` accepts exactly one `[]byte` PNG, so the
shell embeds only the retina `tray-templateTemplate@2x.png`; the 1x PNG is
still generated and committed as an asset but not embedded.

Manual native-shell verification remains required: check the Dock icon,
native traffic lights centered on the 42px titlebar, close-hides-window,
reopening from the Dock and from the tray menu, profile checkbox toggles and
live menu refresh, template-icon tinting in light and dark menu bars, and Quit
from the tray menu.

## Pinned versions

- Wails CLI and Go module: `github.com/wailsapp/wails/v3 v3.0.0-beta.4`
- npm runtime: `@wailsio/runtime 3.0.0-alpha.97`

`3.0.0-alpha.97` is the runtime version bundled by the pinned Wails Go module.
The project was scaffolded from `wails3 init -t vue -n hive-desktop`, the Vue +
TypeScript template of the alpha it started on.

From the repository root, `mise install` provisions the matching Wails CLI.
The manual equivalent is:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.4
```

## Parent-module adaptations

The generated template normally has its own `go.mod`. Hive is a single Go
module, so `desktop/go.mod` and `desktop/go.sum` were removed. The desktop
entry point is the `github.com/hay-kot/hive-desktop/desktop` package within the root
`github.com/hay-kot/hive-desktop` module, and the Wails dependency is required by the
root `go.mod`.

The Wails Taskfiles still run from `desktop/`, which lets Go discover the
parent module automatically. Their `go:mod:tidy` task explicitly runs from the
module root, and their module-file task inputs point at `../go.mod` and
`../go.sum`. The Android and iOS binding-generation task inputs use those same
parent paths. The template's unused iOS option-overlay stubs were removed;
they were subdirectory files rather than code compiled with the desktop package.

A `main` package named `desktop` cannot use an unqualified `go build
./desktop` output at the repository root: Go would try to write a `desktop`
binary where this directory already exists. Always give the desktop binary an
explicit output path:

```sh
go build -o ./desktop/bin/hive-desktop ./desktop
go build -tags server -o ./desktop/bin/hive-desktop-server ./desktop
```

`frontend/dist/.gitkeep` is tracked and embedded by `//go:embed
all:frontend/dist`, so the first command also compiles before a frontend build.
`frontend/public/.gitkeep` is copied by Vite so the tracked dist placeholder is
restored after every frontend build. The rest of `frontend/dist` remains ignored.

Bindings must be generated while the current directory is `desktop/`, so the
Wails CLI identifies that directory as the application package while Go walks
up to the parent module:

```sh
cd desktop
wails3 generate bindings -clean=true -ts -i
```

The frontend Vite plugin requires generated typed-event bindings. The shell
registers the `auth:updated`, `log:appended`, `flows:updated`, and
`actions:updated` events using package-variable initialization rather than an
`init()` function because this repository enables `gochecknoinits`
(`internal/adapter/wailsui/events.go` carries a comment saying the same).
Each one is a subscription to a typed payload the core publishes on
`app/events`; the adapter degrades it to the wake-up signal below. All are wake-up signals: `auth:updated`
makes the frontend re-read auth Status (device-flow grants land in a Go
goroutine), `log:appended` carries the pipeline event log's new tail offset,
`flows:updated` fires after a flows/*.yaml reload, and `actions:updated` fires
after an actions.yml reload so the detail pane can re-read configured actions.

The GitHub fetch layer lives in `internal/app/sources/github/feed`: mock fixtures in
`HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE` modes, or the GitHub-backed `LiveProvider`. Live data is
acquired per embedded flow **source** (a search query or the notifications
inbox) and cached by what is requested — kind + query + limit — so any number
of source nodes reading the same data share one request. The pipeline producer
polls every enabled flow's `sources.github` nodes, appends changed items to the
event log, and commits terminal `feed` nodes into durable `feed_item` rows that
the sidebar reads.

## Flows, feeds, and actions as code

A profile is a flow. Flow definitions live as user-editable YAML under
`$XDG_CONFIG_HOME/hive/desktop/flows/` (`~/.config` fallback;
`HIVE_DESKTOP_FLOWS_DIR` overrides the directory), deliberately in the config dir
so they can live in a dotfiles repo. App-local state (`feed_item`, read state,
event-log offsets, queued output commands) stays in the data dir's `desktop/`
subdirectory.

The **System** settings screen (gear → System) surfaces these locations — the
data directory, config directory, log file (`<data-dir>/desktop/desktop.log`),
and pipeline database (`<data-dir>/desktop/desktop-pipeline.db`) — each with
copy-path, open-in-default-app, and reveal-in-file-manager actions. It can also
point the data and config directories at a different folder (e.g. an
iCloud-synced directory): the choice is written to
`$XDG_CONFIG_HOME/hive/desktop/bootstrap.yaml`. Startup resolves explicit
`HIVE_DESKTOP_DATA_DIR` / `HIVE_DESKTOP_CONFIG_DIR` overrides first, then the
pointer, then XDG defaults, and injects one immutable path snapshot. Overrides
are point-only — existing data is not moved — and take effect after a restart.
The pointer remains at its fixed XDG location so the app can find the config
root after it moves.

## Desktop settings

`settings.yaml` is strictly decoded into a nested typed schema. Resolution is
safe defaults → strict YAML validation → typed `HIVE_DESKTOP_*` environment
overrides → effective-value validation; unknown fields and invalid explicit
values fail startup even when an environment value shadows them. Process
overrides are reapplied after writes and are never persisted accidentally.

**Resolution happens once, at startup: assume every change needs a relaunch.**
Editing `settings.yaml` while the app runs, or flipping a flag the running
process already mounted, does not reach it — the exceptions are
`notifications.*` (re-read on every delivery) and the poll interval when changed
through Settings ▸ Integrations rather than by hand. Making reload dynamic or
triggerable is [#151](https://github.com/hay-kot/hive-desktop/issues/151).

A fully expanded safe configuration is:

```yaml
polling:
  interval: 5m
updates:
  enabled: true
  channel: "" # empty follows the running build: stable, beta, or dev
notifications:
  enabled: true
  delivery: auto # auto, system, or app
  sound: true
appearance:
  theme: ""
  terminal_font_size: "" # small, medium, large, xl, or xxl; empty means medium
  terminal_font_family: "" # any installed monospace family; empty is the bundled JetBrains Mono (ADR bundled-faces-are-jetbrains-mono-inter-and-a-symbol-font)
  terminal_font_weight: 0 # 300, 350, 400, 600, or 700; 0 means the default, 350
  terminal_font_weight_bold: 0 # the weight bold cells draw at; 0 means the default, 700
  terminal_line_height: 0 # 1 to 1.6 in tenths; 0 means the default, 1.2 (ADR terminal-line-height-and-letter-spacing)
  terminal_letter_spacing: 0 # extra tracking in device pixels, 0 to 3 (ADR terminal-line-height-and-letter-spacing)
  terminal_show_windows: true # list every active session's windows in the terminal sidebar
  terminal_show_status_bar: false # give the attached session a bar carrying its checkout's git and pull-request state (ADR session-git-and-pull-request-status-is-computed-in-app-not-shelled-out-to-hive-or-gh)
  terminal_pool_size: 3 # sessions kept attached for instant switching (1-6, ADR terminal-attach-pool)
profiles:
  order: [] # flow ids, top of the rail first; ids left out sort alphabetically
            # behind them. Written by dragging a tile in the rail.
http:
  enabled: true # loopback server: webhook listener + agent API (ADR agent-http-api)
  host: 127.0.0.1
  port: 0 # the OS chooses
telemetry:
  enabled: false # export this app's own metrics, logs and traces over OTLP
  endpoint: "" # the signal-less OTLP base, https only; may be a secret reference
  instance_id: "" # the endpoint's basic-auth username; may be a secret reference
  token: "" # a reference, never a token: env:NAME, file:/path, or op://vault/item/field
keybindings: {} # sparse overrides; omitted commands keep catalog defaults.
                 # A binding is a single combo ("j") or a space-separated
                 # sequence of combos pressed in order ("g i").
agent_workspaces:
  dir: "" # workspace root; empty is the config dir's workspaces/ (a leading ~ is expanded)
  session_end_delay: 10s # grace between a chat asking to end its own session and the session being ended
paths:
  tmux: "" # absolute path to tmux; empty discovers it (ADR tmux-discovery)
development:
  mocks:
    mode: live # live, feed, pipeline, onboarding, or action-smoke
  instance:
    id: ""
  github:
    api_base: "" # loopback-only devserver override (ADR devserver-github-proxy)
  vite:
    host: 127.0.0.1
    port: 0
  wails:
    host: 127.0.0.1
    port: 0
  pprof:
    enabled: false # mounts on the loopback HTTP server when on (ADR pprof-debug-endpoint)
  metrics:
    enabled: false # serves /metrics on the loopback HTTP server when on
  debug:
    pause_ingest: 0s
    pause_commit: 0s
```

`paths.tmux` is the escape hatch for tmux discovery, not the normal way
to configure it: left empty, the app searches `$PATH` and then the prefixes
package managers install into (Homebrew, MacPorts, Nix), because a desktop
launch does not inherit the shell's `$PATH` — macOS gives an `.app` bundle
`/usr/bin:/bin:/usr/sbin:/sbin` (ADR tmux-discovery). Set it only for an install those
misses; it must be absolute, a configured path that does not work is an error
rather than a fallback to a different tmux, and changing it takes a relaunch.
Installing tmux does not: a failed lookup is retried, so only a successful one
is remembered.

`telemetry` sends the app's own signals to an OpenTelemetry endpoint with no
collector in between; `development.metrics` serves the same instruments at
`/metrics` on the loopback server for a local scrape. The two are independent —
either, both, or neither — because one MeterProvider feeds both readers.

All three of `endpoint`, `instance_id` and `token` accept a **secret
reference** (ADR config-holds-secret-references-not-secrets-and-1password-is-one-of-the-sources), so one 1Password item can hold a whole
destination:

```yaml
telemetry:
  enabled: true
  endpoint: op://Private/Grafana Cloud/endpoint
  instance_id: op://Private/Grafana Cloud/username
  token: op://Private/Grafana Cloud/credential
```

Three sources are recognized — `env:NAME`, `file:/path`, and
`op://vault/item/field` — and they differ in whether a literal is allowed.
`token` **requires** a reference, because a literal there is a credential in a
dotfiles-managed file; `endpoint` and `instance_id` name a destination and are
ordinarily written out, so both forms work:

```yaml
endpoint: https://otlp-gateway-prod-us-central-0.grafana.net/otlp  # fine
token: glc_eyJvIjoi...                                            # rejected
```

A written-out endpoint is checked for https at load. A reference is not,
because its target is unknown until launch; the resolved value is checked
either way. References resolve only when `telemetry.enabled` is true, so a
disabled section never raises a 1Password prompt.

Prefer `file:` or `op://` for an installed app. A launched `.app` inherits
almost no environment and the app reads no env files, so `env:` resolves only
when a shell or `mise run dev` put the variable there. The `op://` value is
what 1Password's own **Copy Secret Reference** puts on the clipboard; `op` is
found through the same package-manager prefixes tmux discovery searches, and a
locked vault can raise an approval prompt on the first read.

For Grafana Cloud the endpoint is `https://otlp-gateway-<zone>.grafana.net/otlp`
and `instance_id` is the OTLP instance id printed on the stack's OpenTelemetry
tile, which is **not** the stack id.

Every scalar override mirrors its YAML path, for example
`updates.channel` → `HIVE_DESKTOP_UPDATES_CHANNEL` and `http.port` →
`HIVE_DESKTOP_HTTP_PORT`. Paths and logging use
`HIVE_DESKTOP_DATA_DIR`, `HIVE_DESKTOP_CONFIG_DIR`,
`HIVE_DESKTOP_FLOWS_DIR`, `HIVE_DESKTOP_ACTIONS_PATH`, and
`HIVE_DESKTOP_LOG_LEVEL`. Wails, credentials, build stamping, and release
secrets are separate environment boundaries rather than settings fields.
`development.pprof` is parsed and validated but does not start an endpoint yet;
that waits for the common plugs lifecycle.

```yaml
name: Triage
enabled: true
nodes:
  - id: my-work
    type: sources.github
    credential: github/octocat
    kind: search
    query: "is:open involves:@me archived:false"
    limit: 50
  - id: team-feed
    type: feed
wires:
  - { from: my-work, to: team-feed }
```

Flow parsing is strict and validated by Go on Deploy: node ids are unique,
known node types decode their own config, source limits match the GitHub API
caps, action nodes reference actions that exist in `actions.yml`, and wires
connect valid ports. A `flow.FlowsWatcher` watches the directory (not
individual files, so atomic editor saves work) and hot-reloads external edits;
the app's own SaveFlow/SaveLayout writes intentionally trigger the same reload
and `flows:updated` wake-up.

`actions.yml` lives at `$XDG_CONFIG_HOME/hive/desktop/actions.yml`
(`HIVE_DESKTOP_ACTIONS_PATH` overrides the file) and defines detail-pane/output
worker actions such as `launch-session`, `shell`, and `publish-message`:

```yaml
version: 1
actions:
  - id: review-pr
    label: Spawn review agent
    type: launch-session
    applies_to: [pr]
    prompt_template: "Review {{ .Payload.title }}"
```

The file's sequence order is the catalog's presentation order: `ActionStore`
never re-sorts, so the detail pane and item action menu offer applicable
actions in the order they appear on disk. Dragging a row in Settings ▸ Actions
rewrites that sequence (`ActionStore.Reorder`), and hand-editing the file has
the same effect.

An `actions.ActionsWatcher` watches the `actions.yml` parent directory,
debounces write/rename bursts, reloads `ActionStore`, and emits
`actions:updated`. `ActionStore` keeps the last-good action set when a broken
file is saved, so a half-edited config does not blank actions out from under a
running flow or the detail pane.

The headless core lives under `internal/app/**` and every Wails service under
`internal/adapter/wailsui/**`; `desktop/` is `main()` plus build-info stamping.
`internal/app/auth` implements GitHub authentication behind
`wailsui.AuthService`: an OAuth device flow plus a personal-access-token
fallback, with tokens stored in the OS keychain (`HIVE_GITHUB_TOKEN` is a
read-only headless override). The device flow uses the registered Hive Desktop
OAuth app's public client ID by default; `HIVE_GITHUB_CLIENT_ID` overrides it,
e.g. to test another registration. `internal/hivecore/github` is the shared
GitHub REST client, vendored rather than desktop-owned.

`HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE` selects deterministic offline backends:
`feed` starts authenticated, while `onboarding` starts signed out with a fake
device flow that grants after ~1.5s. `live` is the safe default.

`build/config.yml` keeps `dev_mode.root_path: .`; when `wails3 dev` is started
from `desktop/`, Wails watches `desktop/` rather than the whole repository.

## Development and builds

Use the root mise tasks as the canonical entry points:

```sh
mise run bindings    # Regenerate frontend TS bindings.
mise run icons       # Regenerate committed icon assets.
mise run build       # Build the desktop app; on macOS emits desktop/bin/hive-desktop.
mise run serve       # Build and run the headless server build.
mise run dev         # Run Wails directly with the generated launch.env.
mise run dev:prepare # Create/reuse the isolated instance and launch.env.
mise run dev:fresh   # Safely reseed the instance and regenerate launch.env.
mise run dev:reset   # Safely remove the marked instance and launch.env.
```

`dev` runs `wails3 dev -config ./build/config.yml` from the desktop application
directory. There is no Taskfile equivalent: the Taskfile is wails3's dispatch
target and carries only `build`, `package`, and `run` (ADR
mise-is-the-task-interface-and-the-taskfile-is-wails3-build-dispatch).

Dev mode uses the gitignored `.hive-desktop/` directory in the current
worktree. First use snapshots the installed databases and desktop config;
`dev:prepare` and setup create that state once; normal `dev` runs Wails
directly with it. `dev:fresh` deletes and reseeds it through
marker/path/symlink checks, and `dev:reset` safely removes it. A
short-lived worktree lock serializes preparation and destructive operations;
fresh/reset also refuse while either configured development server is active.
Config symlink targets are materialized into the snapshot rather than retained
as links to installed state. The configured agent-workspace root is copied to
`.hive-desktop/config/workspaces` and pinned there by
`HIVE_DESKTOP_AGENT_WORKSPACES_DIR` in `launch.env`, including when the installed
root is in iCloud Drive or another File Provider. Separate worktrees therefore
have separate config, workspaces, data, databases and logs. Explicit
`HIVE_DESKTOP_DATA_DIR` / `HIVE_DESKTOP_CONFIG_DIR` values still win.

`development.vite` uses the required `127.0.0.1` host because Wails constructs
its frontend URL with localhost; its port defaults to `0`. `development.wails`
uses a loopback host and port `0` by default. `cmd/devtools prepare` chooses
distinct free ports and atomically writes the gitignored, non-secret
`launch.env`; the `dev` mise task loads it, then loads the optional,
gitignored developer-authored `overrides.env` so explicit overrides win without
special handling in devtools. The generated values bridge to framework-owned
`WAILS_VITE_*` and `WAILS_SERVER_*` variables. Override those framework names
for framework addresses; use `HIVE_DESKTOP_*` names for application settings.
Setup and a missing-file-only enter hook prepare `launch.env`; mise derives
`VITE_HIVE_DEV_BRANCH` from Git for each launch rather than persisting it, then
`dev` invokes Wails directly. Wails/Vite do not read application YAML
themselves. Their
preselected ports have an unavoidable preflight-to-bind race because neither
framework accepts an open listener. `cmd/devtools` uses urfave/cli subcommands
and zerolog console output; pass `--log-level` before the subcommand or set
`HIVE_DESKTOP_DEVTOOLS_LOG_LEVEL` to adjust its default `info` verbosity.

The OS keychain and fixed `bootstrap.yaml` remain shared. Use a mock mode when
credential isolation matters: signing out of a live dev instance can affect the
installed app's keychain credential.

Wails supports server builds. `serve` builds the frontend, then compiles
the pure HTTP-server variant without GUI dependencies to
`desktop/bin/hive-desktop-server` and runs it. The assets are `//go:embed`ded,
so frontend edits require re-running the task; the fast frontend loop is `dev`
with Vite HMR. The server defaults to `localhost:8080`; if that port is taken,
override it with the Wails-native `WAILS_SERVER_PORT` env var
(e.g. `WAILS_SERVER_PORT=9000 mise run serve`).

## Icons

The desktop icon masters live in `build/icons/`: `hive-mark.svg` is the
1024px amber Hive mark on its dark rounded-square field, and
`tray-template.svg` is the separate 18px macOS template mark.
Regenerate every committed desktop icon with `mise run icons`.

The mark's amber is `#f5b23f` — the same value as the app's `--hv-accent` in
`frontend/src/styles/main.css`. Keep the two in step.

The tray master is **not** a scaled `hive-mark.svg`. At 18-44px the app mark's
connector strokes fall below a pixel and its hexagons close up, so the tray
redraws the same four-node figure with wider spacing, much thicker strokes, and
the amber/gray split carried as opacity (macOS tints a template through its
alpha). `web/public/favicon.svg` is a third copy on the tray's proportions, so
a change to the mark has to land in all three.

The script requires librsvg (`rsvg-convert`), ImageMagick (`magick`), and
macOS `iconutil`; install the first two with `brew install librsvg imagemagick`.
The authoring toolchain was `rsvg-convert 2.62.3` and ImageMagick
`7.1.2-27`; inspect local versions with `rsvg-convert --version` and
`magick --version`. It strips volatile PNG metadata. Output is byte-stable when
regenerated on this authoring toolchain, but that guarantee does not extend to
other renderer or ImageMagick versions.

`build/darwin/icons.icns` is copied directly into macOS bundles; the Wails
scaffold's `appicon.icon` and `Assets.car` inputs were removed, so the template
icon generator is not part of any desktop build. `CFBundleIconFile` continues
to point at `icons.icns`. The Linux AppImage consumes the 512px
`build/appicon.png`; nFPM consumes the generated
`build/linux/icon-128.png` for its 128px hicolor installation path. The build
targets are macOS and Linux only, so no Windows assets are generated. The
generated `tray-templateTemplate.png` and `tray-templateTemplate@2x.png`
retain the macOS `Template` suffix for automatic tinting.

## Agent-driven UI verification

Use the headless server build for the UI verification loop:

```sh
mise run serve
```

Drive and inspect the app at `http://localhost:8080` with Playwright or browser
tooling, read the screenshots in `desktop/e2e/screenshots`, edit, and repeat.
Set `HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE=onboarding` to drive the first-run screen offline.
Run `mise run e2e` as the Docker-only regression gate. Its harness
builds the server and starts private feed, onboarding, pipeline, and action
smoke instances inside the pinned Playwright image; no local browser install
or host Playwright invocation is supported.

Native shell behavior — the tray, the Dock, and close-hides-window — remains
a manual verification concern.

## Actions catalog and delivery

Desktop actions are global configuration, stored in `actions.yml` beside the
flow directory (`$XDG_CONFIG_HOME/hive/desktop/actions.yml`; override with
`HIVE_DESKTOP_ACTIONS_PATH`). The settings screen creates, edits, and deletes the
catalog entries. External YAML edits reload live; a parse failure keeps the
last-good catalog until the file is fixed. `show_in_detail` controls manual
feed-item visibility only, while a flow `action` node may target any catalog
id regardless of its detail kind scope.

A flow action node is automation control: it emits a durable, deduplicated
`output_command`, not an editor-side script. `launch-session` can launch
headlessly when its repository template is configured, or interactively ask
for repository/name/agent when it is not. Prefer local HTTPS or SSH remotes
for repository templates. `shell` captures bounded stdout/stderr diagnostics.
`publish-message` accepts only a constant topic and durably publishes with
sender `hive-desktop` and an empty session identity. Completed outcomes are
typed (session or message); failed outcomes retain their persisted diagnostics.

## Docker E2E gate

`mise run e2e` is Docker-only. It builds the digest-pinned
Go/Playwright image in `desktop/e2e/Dockerfile` and runs Playwright there; it
never attaches to a host browser or server. `run-docker.sh` supplies a fresh
256-bit harness marker, which both the image command and server launcher
require, so direct host Playwright cannot start the servers. Each server has a
private data/config root. Fixture-driven servers receive private flow/action
copies and a run id; onboarding deliberately receives no injected fixture env,
and action-seed deliberately starts without an action fixture to verify exact
first-run seeding. Action smoke also gets a local bare Git remote. This keeps
parallel browser projects from mutating checked-in fixtures or sharing
SQLite/action state. Docker must be available; there is no host fallback.

## Reading what the app costs

The developer-tools pane (`/dev`, "Open developer tools" in the palette) polls
`internal/app/procstats`: resident memory and CPU for the app **and the process
tree below it** (a terminal's shell, an agent), plus goroutines, heap, GC, and a
measured Wails round trip. RSS is what the OS charges for and `runtime.MemStats`
cannot report it at all, which is what gopsutil is there for. Spans answer "why
was that click slow"; this answers "what is this build costing, and is it
growing".

Frame rate, dropped frames and event-loop lag come from `useFrameStats`, which
**starts at boot, not when the pane opens** — the jank worth catching happens in
the terminal or a long feed, so a sampler scoped to the pane would only measure
the pane. Go make something stutter, then open `/dev` and read the last ten
seconds. Frames past twice the display period and lag past 50ms are also
recorded as `ui` spans, so `perf.jsonl` keeps history beyond that window. The
sampler pauses while the window is occluded, since `requestAnimationFrame`
stops there and the gap is the OS declining to draw, not a stall.

Two things WebKit does not give us, so do not go looking: `longtask` /
`long-animation-frame` observers (Chromium-only, so no attribution of _which_
task blocked) and `performance.memory` (no JS heap size to sit beside the Go
heap). `performance.now()` is also clamped to ~1ms, which is why the round-trip
figures are timed in batches rather than per call.

The webview is **not** in that total: on macOS the WebKit processes are XPC
services parented to launchd, not children of the app, so they cannot be
attributed without a private API. The pane states this rather than
under-reporting silently.

Set `HIVE_DESKTOP_DEVELOPMENT_DEVTOOLS_ENABLED=1` to open it on a signed build,
which is the one worth measuring (ADR
developer-tools-are-reachable-in-a-shipped-build-behind-a-setting); a Vite dev
build always has it.

Recording UI spans to `perf.jsonl` is the `usePerf` hook, on in `dev` via
`launch.env` and off in a shipped build (ADR
ui-performance-spans-are-recorded-to-jsonl). The **ui-perf** agent skill carries
the full loop: the naming rules and the jq recipes for percentiles, outliers,
and grouping by attribute.
