---
title: settings.yaml
description: Every setting the app reads, section by section, with its default and the environment variable that overrides it.
group: Configuration
order: 0
---

`settings.yaml` holds the app's own preferences: polling, updates,
notifications, appearance, the local HTTP listener, keyboard shortcuts, and a
few paths. Flows and actions are separate files, covered by
[Flows](/docs/concepts/flows) and [Actions](/docs/concepts/actions).

Every key is optional. An omitted key uses the compiled default shown below.
Most settings are also editable from **Settings** in the app, which writes
this file; the sections below note the few that are not.

> [!TIP] Ask the Hive workspace
> The **Hive** workspace under **Chats** carries the `hive-settings` skill,
> which is this page with your install's real path. "Poll every two minutes
> and stop making sound" is a complete request.

## Where the files live

Hive uses XDG-style paths on macOS and Linux alike.

| What | Default path | Override |
| --- | --- | --- |
| Config directory | `~/.config/hive/desktop/` | `HIVE_DESKTOP_CONFIG_DIR`, or `XDG_CONFIG_HOME` |
| Settings | `<config>/settings.yaml` | |
| Flows | `<config>/flows/` | `HIVE_DESKTOP_FLOWS_DIR` |
| Actions | `<config>/actions.yml` | `HIVE_DESKTOP_ACTIONS_PATH` |
| Agent workspaces | `<config>/workspaces/` | `agent_workspaces.dir` below |
| Data directory | `~/.local/share/hive/` | `HIVE_DESKTOP_DATA_DIR`, or `XDG_DATA_HOME` |
| Log | `<data>/desktop/desktop.log` | level via `HIVE_DESKTOP_LOG_LEVEL` |
| Pipeline database | `<data>/desktop/desktop-pipeline.db` | |
| Tokens | the OS keychain | `HIVE_GITHUB_TOKEN` (read-only, headless use) |

The data directory is shared with the `hive` CLI: `hive.db`, the sessions
database, lives at its root. Everything the desktop app owns is under
`desktop/` inside it.

## How overrides work

Every scalar setting has an environment variable named after its YAML path
with the prefix `HIVE_DESKTOP_`, so `updates.channel` is
`HIVE_DESKTOP_UPDATES_CHANNEL`. An override is process-local: it wins for that
launch and is never written to the file. The file has to be valid on its own,
so an override cannot paper over a broken setting.

The app reloads user-facing settings on save. Two sections are read once at
startup and take effect on the next launch: `http` and everything under
`development`.

## `version`

```yaml
version: 3   # written and migrated by the app; do not edit
```

## `polling`

```yaml
polling:
  interval: 5m   # HIVE_DESKTOP_POLLING_INTERVAL. Go duration; minimum 60s
```

How often polled sources fetch. A tick drains every source in sequence.
<kbd>r</kbd> refreshes a feed now regardless.

## `updates`

```yaml
updates:
  enabled: true      # HIVE_DESKTOP_UPDATES_ENABLED. Check for and offer updates
  channel: stable    # HIVE_DESKTOP_UPDATES_CHANNEL. stable, beta, or dev; omit to follow the build
```

See [Updates and channels](/docs/help/updates).

## `notifications`

```yaml
notifications:
  enabled: true      # HIVE_DESKTOP_NOTIFICATIONS_ENABLED. The master switch
  delivery: auto     # HIVE_DESKTOP_NOTIFICATIONS_DELIVERY. auto, system, or app
  sound: true        # HIVE_DESKTOP_NOTIFICATIONS_SOUND. System banners only
```

`auto` raises a system banner only while Hive is unfocused and a toast
otherwise; `system` always raises a banner; `app` never does. These settings
win over every flow's notify node, and on macOS a banner also needs the
[system permission](/docs/getting-started/notifications).

## `appearance`

```yaml
appearance:
  theme: ""                        # HIVE_DESKTOP_APPEARANCE_THEME. A theme id from Settings ▸ Appearance
  font_family: ""                  # HIVE_DESKTOP_APPEARANCE_FONT_FAMILY. The UI face; empty is the bundled one
  mono_font_family: ""             # HIVE_DESKTOP_APPEARANCE_MONO_FONT_FAMILY. Monospace text outside terminals
  terminal_font_size: ""           # HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_SIZE. small, medium, large, xl, xxl; empty is medium
  terminal_font_family: ""         # HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_FAMILY. Any installed monospace family
  terminal_font_weight: 0          # HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_WEIGHT. 300, 350, 400, 600, 700; 0 is the default, 350
  terminal_font_weight_bold: 0     # HIVE_DESKTOP_APPEARANCE_TERMINAL_FONT_WEIGHT_BOLD. 0 is the default, 700
  terminal_line_height: 0          # HIVE_DESKTOP_APPEARANCE_TERMINAL_LINE_HEIGHT. 1.0 to 1.6; 0 is the default, 1.2
  terminal_letter_spacing: 0       # HIVE_DESKTOP_APPEARANCE_TERMINAL_LETTER_SPACING. Extra device pixels per cell, 0 to 3
  terminal_show_windows: true      # HIVE_DESKTOP_APPEARANCE_TERMINAL_SHOW_WINDOWS. List every active session's windows in the Code sidebar
  terminal_show_status_bar: false  # HIVE_DESKTOP_APPEARANCE_TERMINAL_SHOW_STATUS_BAR. Git and PR state above the attached session
  terminal_pool_size: 3            # HIVE_DESKTOP_APPEARANCE_TERMINAL_POOL_SIZE. Sessions kept attached for instant switching, 1 to 6
```

A font family names an installed face. `system-ui` and `ui-monospace` select
the platform stacks. The terminal values are carried verbatim and healed by
the frontend: a value outside the listed range reads as the default rather
than failing validation.

## `profiles`

```yaml
profiles:
  order:            # flow ids, top of the rail first
    - personal
    - work
```

The order of the workspace rail. It never has to be exhaustive: an id it omits
sorts alphabetically after the ones it names, and an id naming no flow is
ignored, so deleting a workspace does not invalidate it. Dragging a tile in
the rail rewrites this key; editing it by hand takes effect on the next launch.

## `http`

```yaml
http:
  enabled: true      # HIVE_DESKTOP_HTTP_ENABLED
  host: 127.0.0.1    # HIVE_DESKTOP_HTTP_HOST. Loopback only
  port: 0            # HIVE_DESKTOP_HTTP_PORT. 0 lets the OS choose; otherwise 1024 to 65535
```

The local loopback server. The webhook listener, the terminal transport, and
the MCP server all ride it, so turning it off turns those off. Read at
startup. With `port: 0` the port can change between launches; pin one here or
under **Settings ▸ Integrations ▸ Webhooks** when a script needs a stable URL.

## `telemetry`

```yaml
telemetry:
  enabled: false                          # HIVE_DESKTOP_TELEMETRY_ENABLED
  endpoint: ""                            # HIVE_DESKTOP_TELEMETRY_ENDPOINT. Signal-less OTLP base URL, https
  instance_id: ""                         # HIVE_DESKTOP_TELEMETRY_INSTANCE_ID
  token: "op://vault/grafana/otlp-token"  # HIVE_DESKTOP_TELEMETRY_TOKEN. Must be a reference, never a literal
```

Exports the app's own traces, metrics, and logs over OTLP to a backend you
run. Off by default and never on without an endpoint. `endpoint` and
`instance_id` may be written out or be references; `token` **must** be a
reference (`env:NAME`, `file:/path`, or `op://vault/item/field`) so a
credential never sits in a dotfiles-managed file. On Grafana Cloud the
endpoint is `https://otlp-gateway-<zone>.grafana.net/otlp` and the instance
id is the one on the stack's OpenTelemetry tile, not the stack id.

## `keybindings`

```yaml
keybindings:
  feed.next: [j, arrowdown]
  launcher.lazygit: [alt+g]
  tasks.toggle: ["g t"]
```

Sparse overrides keyed by command id. A binding is a single combo or a
space-separated sequence pressed in order. The ids, the defaults, and the
combo syntax are on [Keyboard shortcuts](/docs/configuration/keybindings);
**Settings ▸ Keyboard** edits the same key.

## `paths`

```yaml
paths:
  tmux: ""     # HIVE_DESKTOP_PATHS_TMUX. Absolute path to a tmux binary; empty discovers one
```

Hive finds tmux on your login shell's PATH and in the usual package-manager
prefixes. Set this only when it cannot.

## `editor`

```yaml
editor:
  command: ""  # HIVE_DESKTOP_EDITOR_COMMAND. zed, code, cursor, subl, or an absolute path
```

What "Open in editor" launches on a directory. A single word, never a command
line with flags.

## `agent_workspaces`

```yaml
agent_workspaces:
  dir: ""      # HIVE_DESKTOP_AGENT_WORKSPACES_DIR. Empty is <config>/workspaces; ~ is expanded
```

Where [agent workspaces](/docs/concepts/agent-workspaces) live. Configurable so
the root can sit in iCloud Drive or another synced folder.

## `development`

Everything here is for working on Hive itself. A shipped build reads it but
leaves every value at its default, and the settings UI does not expose it.
Read at startup.

```yaml
development:
  mocks:
    mode: live             # HIVE_DESKTOP_DEVELOPMENT_MOCKS_MODE. live, feed, pipeline, onboarding, action-smoke
  instance:
    id: ""                 # HIVE_DESKTOP_DEVELOPMENT_INSTANCE_ID. Names this instance in logs and telemetry
  github:
    api_base: ""           # HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE. A loopback proxy for the GitHub API
  vite:  { host: 127.0.0.1, port: 0 }   # HIVE_DESKTOP_DEVELOPMENT_VITE_HOST / _PORT
  wails: { host: 127.0.0.1, port: 0 }   # HIVE_DESKTOP_DEVELOPMENT_WAILS_HOST / _PORT
  pprof:
    enabled: false         # HIVE_DESKTOP_DEVELOPMENT_PPROF_ENABLED. Serve /debug/pprof on the local server
  perf:
    enabled: false         # HIVE_DESKTOP_DEVELOPMENT_PERF_ENABLED. Record UI spans to perf.jsonl
  metrics:
    enabled: false         # HIVE_DESKTOP_DEVELOPMENT_METRICS_ENABLED. A local Prometheus scrape endpoint
  devtools:
    enabled: false         # HIVE_DESKTOP_DEVELOPMENT_DEVTOOLS_ENABLED. In-app developer tools in a shipped build
  debug:
    pause_ingest: 0s       # HIVE_DESKTOP_DEVELOPMENT_DEBUG_PAUSE_INGEST. Up to 10m
    pause_commit: 0s       # HIVE_DESKTOP_DEVELOPMENT_DEBUG_PAUSE_COMMIT. Up to 10m
```

The mock modes replace the live backends with fixtures so the app can be
driven offline and first run replayed without wiping anything:

- **`live`**: the real thing. The default.
- **`feed`**: starts with a fake GitHub account (`github/octocat`) connected
  and a fixed set of items seeded into the feed.
- **`pipeline`**: the same connected account with nothing seeded, so the
  end-to-end suite can drive the ingest and routing path itself.
- **`action-smoke`**: like `feed`, and the mode the end-to-end suite runs
  actions under.
- **`onboarding`**: starts with nothing connected, an isolated flows
  directory, and a device flow that approves itself after about a second and
  a half. Replays first run without touching your state.

In every mock mode the live poller and the output worker are skipped, and the
OS keychain is never read.

## Validation

The file is rejected as a whole, and the app keeps running on defaults, when:

- it contains an unknown key or more than one YAML document;
- `polling.interval` is under 60 seconds;
- a closed-set value is outside its set (`updates.channel`,
  `notifications.delivery`, `development.mocks.mode`);
- `http.host` or a development listener host is not a loopback address, or a
  port is outside 1024 to 65535;
- `paths.tmux` is relative, or `editor.command` has more than one word;
- `telemetry.enabled` is true without an endpoint, instance id, and a token
  that is a reference;
- `development.github.api_base` is not a loopback URL.

Durations are strings such as `5m` or `90s`. A bare number is not seconds.
