---
icon: lucide/settings
description: Change Hive Desktop preferences in the app or through settings.yaml.
---

# Settings

Open Settings with <kbd>⌘,</kbd> or from the command palette.

## In the app

Settings shows or changes:

- general behavior such as polling and the default editor;
- the theme, interface fonts, and terminal typography;
- terminal spacing, visible windows, and the status bar;
- keyboard shortcuts;
- notification delivery and sound;
- connected source accounts and the local webhook listener;
- actions and quick terminals;
- the agent workspace folder location;
- diagnostics, updates, and release channels.

The app validates changes before saving them.

## Configuration files

Hive keeps its user configuration under `~/.config/hive/desktop/` by default.

| Content | Path |
| --- | --- |
| Settings | `settings.yaml` |
| Flows | `flows/` |
| Actions | `actions.yml` |
| Agent workspaces | `workspaces/` |
| Environment file | `.env` |
| Application data and logs | `~/.local/share/hive/desktop/` |
| Account tokens | OS keychain |

Set `HIVE_DESKTOP_CONFIG_DIR` or `HIVE_DESKTOP_DATA_DIR` to move the config or data root. You can also move the flow, action, and workspace paths separately.

!!! tip "Ask the Hive workspace"
    The **Hive** workspace in **Chats** can update settings with the `hive-settings` skill. For example, ask it to change the polling interval or terminal font.

## Advanced configuration

Most users can use the Settings screens. Edit `settings.yaml` for values that are not exposed there or when you manage configuration with dotfiles.

```yaml
polling:
  interval: 5m
http:
  enabled: true
  host: 127.0.0.1
  port: 0
paths:
  tmux: ""
editor:
  command: ""
agent_workspaces:
  dir: ""
environment:
  file: ""
```

- `polling.interval` has a minimum of 60 seconds.
- The local HTTP server provides webhooks, MCP, and terminal connections. It only binds to loopback. Set a fixed port if another local tool needs a stable webhook URL.
- `paths.tmux` accepts an absolute path when Hive cannot find tmux.
- `editor.command` accepts an executable name or absolute path without arguments.
- `agent_workspaces.dir` changes where Chats workspaces are stored.
- `environment.file` names an env file Hive reads at startup. See [Environment file](#environment-file).

Every scalar setting can be overridden for one launch with an environment variable based on its YAML path. For example, `polling.interval` becomes `HIVE_DESKTOP_POLLING_INTERVAL`.

## Environment file

Hive starts from the Dock or Spotlight, not from your terminal, so it does not
see the variables your shell exports. Put them in an env file instead:

```bash title="~/.config/hive/desktop/.env"
HIVE_DEFAULT_AGENT=pi
ANTHROPIC_API_KEY=sk-...
EDITOR=nvim
```

Hive reads the file once at startup. It uses the variables for its own
configuration, for `env:` references in settings, and for every command it runs:
agents, session hooks, and shell actions.

The default path is `~/.config/hive/desktop/.env`. Point somewhere else with:

```yaml
environment:
  file: ~/.hive.env
```

Setting the path here rather than fixing it lets you keep the same
`settings.yaml` on two machines and a different env file on each. The default
path stays put even if you move the config folder, for the same reason.

What to expect:

- One `NAME=value` per line. Quote a value with spaces. A `#` after a space
  starts a comment. Values cannot span lines.
- Hive reads the file at startup only. Restart the app after you edit it.
- A variable already set for the launch keeps its value.
- `PATH` and any `HIVE_DESKTOP_*` name are ignored. Hive finds `PATH` from your
  login shell, and desktop settings belong in `settings.yaml`.
- A missing file is fine. A file with a mistake in it is skipped whole, so
  nothing changes and the log names the line.

!!! warning "The file holds secrets"
    Keep it out of a synced folder and out of version control. Hive never logs
    a value from it, only the variable names.

## Updates

**Settings ▸ About** shows the current channel and controls automatic update checks.

To follow a different channel, edit `settings.yaml`:

```yaml
updates:
  channel: beta
```

Available channels are Stable, Beta, and Dev. An omitted channel follows the channel used for the current build.

## Telemetry

Telemetry is disabled by default. Advanced users can send traces, metrics, and logs to an OTLP endpoint they operate. Configure `telemetry.endpoint`, `telemetry.instance_id`, and a referenced token in `settings.yaml`.

Token values must use `env:`, `file:`, or `op://` references. Hive rejects literal telemetry tokens in the settings file.
