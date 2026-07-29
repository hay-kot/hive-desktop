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
alpha, `SystemTray.SetTemplateIcon` accepts exactly one `[]byte` PNG, so the
shell embeds only the retina `tray-templateTemplate@2x.png`; the 1x PNG is
still generated and committed as an asset but not embedded.

Manual native-shell verification remains required: check the Dock icon,
native traffic lights centered on the 42px titlebar, close-hides-window,
reopening from the Dock and from the tray menu, profile checkbox toggles and
live menu refresh, template-icon tinting in light and dark menu bars, and Quit
from the tray menu.

## Pinned versions

- Wails CLI and Go module: `github.com/wailsapp/wails/v3 v3.0.0-alpha2.117`
- npm runtime: `@wailsio/runtime 3.0.0-alpha.97`

`3.0.0-alpha.97` is the runtime version bundled by the pinned Wails Go module.
The `vue-ts` template alias was not available in alpha2.116; `wails3 init -t
vue -n hive-desktop` is that release's Vue + TypeScript template.

From the repository root, `mise install` provisions the matching Wails CLI.
The manual equivalent is:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
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
overrides are reapplied after writes and are never persisted accidentally. A
fully expanded safe configuration is:

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
http:
  enabled: true # loopback server: webhook listener + agent API (ADR 0021)
  host: 127.0.0.1
  port: 0 # the OS chooses
keybindings: {} # sparse overrides; omitted commands keep catalog defaults
experimental:
  terminal: false # terminal mode ships dark (ADR 0033); read at startup
development:
  mocks:
    mode: live # live, feed, pipeline, onboarding, or action-smoke
  instance:
    id: ""
  github:
    api_base: "" # loopback-only devserver override (ADR 0017)
  vite:
    host: 127.0.0.1
    port: 0
  wails:
    host: 127.0.0.1
    port: 0
  pprof:
    enabled: false # mounts on the loopback HTTP server when on (ADR 0023)
  debug:
    pause_ingest: 0s
    pause_commit: 0s
```

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
mise run desktop:generate # Regenerate frontend TS bindings.
mise run desktop:icons    # Regenerate committed icon assets.
mise run desktop:build    # Build the desktop app; on macOS emits desktop/bin/hive-desktop.
mise run desktop:serve     # Build and run the headless server build.
mise run desktop:dev         # Run Wails directly with the generated launch.env.
mise run desktop:dev:prepare # Create/reuse the isolated instance and launch.env.
mise run desktop:dev:fresh   # Safely reseed the instance and regenerate launch.env.
mise run desktop:dev:reset   # Safely remove the marked instance and launch.env.
```

`desktop:dev` runs `wails3 dev -config ./build/config.yml` from the desktop
application directory. The equivalent Taskfile command is `wails3 task dev`.

Dev mode uses the gitignored `.hive-desktop/` directory in the current
worktree. First use snapshots the installed databases and desktop config;
`desktop:dev:prepare` and setup create that state once; normal `desktop:dev`
runs Wails directly with it. `desktop:dev:fresh` deletes and reseeds it through
marker/path/symlink checks, and `desktop:dev:reset` safely removes it. A
short-lived worktree lock serializes preparation and destructive operations;
fresh/reset also refuse while either configured development server is active.
Config symlink targets are materialized into the snapshot rather than retained
as links to installed state. Separate worktrees therefore have separate config, data, databases and
logs. Explicit `HIVE_DESKTOP_DATA_DIR` / `HIVE_DESKTOP_CONFIG_DIR` values still
win.

`development.vite` uses the required `127.0.0.1` host because Wails constructs
its frontend URL with localhost; its port defaults to `0`. `development.wails`
uses a loopback host and port `0` by default. `cmd/devtools prepare` chooses
distinct free ports and atomically writes the gitignored, non-secret
`launch.env`; the `desktop:dev` mise task loads it, then loads the optional,
gitignored developer-authored `overrides.env` so explicit overrides win without
special handling in devtools. The generated values bridge to framework-owned
`WAILS_VITE_*` and `WAILS_SERVER_*` variables. Override those framework names
for framework addresses; use `HIVE_DESKTOP_*` names for application settings.
Setup and a missing-file-only enter hook prepare `launch.env`; mise derives
`VITE_HIVE_DEV_BRANCH` from Git for each launch rather than persisting it, then
`desktop:dev` invokes Wails directly. Wails/Vite do not read application YAML
themselves. Their
preselected ports have an unavoidable preflight-to-bind race because neither
framework accepts an open listener. `cmd/devtools` uses urfave/cli subcommands
and zerolog console output; pass `--log-level` before the subcommand or set
`HIVE_DESKTOP_DEVTOOLS_LOG_LEVEL` to adjust its default `info` verbosity.

The OS keychain and fixed `bootstrap.yaml` remain shared. Use a mock mode when
credential isolation matters: signing out of a live dev instance can affect the
installed app's keychain credential.

The alpha supports server builds. `desktop:serve` builds the frontend, then
compiles the pure HTTP-server variant without GUI dependencies to
`desktop/bin/hive-desktop-server` and runs it. The assets are `//go:embed`ded,
so frontend edits require re-running the task; the fast frontend loop is
`desktop:dev` with Vite HMR. The server defaults to `localhost:8080`; if that
port is taken, override it with the Wails-native `WAILS_SERVER_PORT` env var
(e.g. `WAILS_SERVER_PORT=9000 mise run desktop:serve`).

## Icons

The desktop icon masters live in `build/icons/`: `hive-mark.svg` is the
1024px amber Hive mark on its dark rounded-square field, and
`tray-template.svg` is the separate black-only 18px macOS template mark.
Regenerate every committed desktop icon with `mise run desktop:icons`.

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
mise run desktop:serve
```

Drive and inspect the app at `http://localhost:8080` with Playwright or browser
tooling, read the screenshots in `desktop/e2e/screenshots`, edit, and repeat.
Set `HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE=onboarding` to drive the first-run screen offline.
Run `mise run desktop:e2e` as the Docker-only regression gate. Its harness
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

`mise run desktop:e2e` is Docker-only. It builds the digest-pinned
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
