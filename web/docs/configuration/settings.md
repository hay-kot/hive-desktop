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
- the optional Hive CLI compatibility config;
- diagnostics, updates, and release channels.

The app validates changes before saving them.

## Configuration files

Hive Desktop and the hive CLI use separate configuration paths. They share Code sessions and tasks when both use the same Hive data root, which is the default.

| Scope | Common default | Configures |
| --- | --- | --- |
| Hive CLI and Code session engine | `~/.config/hive/config.yaml` | Repositories, agent profiles, clone and recycle rules, setup commands, starting tmux windows, and shared session behavior |
| Hive Desktop | `~/.config/hive/desktop/` | Inbox, Code presentation, Chats, notifications, integrations, shortcuts, and app behavior |

The hive CLI config honors `HIVE_CONFIG` and `XDG_CONFIG_HOME`. The Desktop config honors `HIVE_DESKTOP_CONFIG_DIR` and `XDG_CONFIG_HOME`. Restart Hive Desktop after changing the hive CLI configuration.

Session and task sharing also depends on the data root. The common default is `~/.local/share/hive/`. If you move it, point the hive CLI's `HIVE_DATA_DIR` and Hive Desktop's `HIVE_DESKTOP_HIVE_DATA_DIR` at the same directory. See the hive CLI [configuration reference](https://colonyops.github.io/hive/configuration/) and [repository rules](https://colonyops.github.io/hive/configuration/rules/).

Hive Desktop keeps these files under its config directory by default:

| Content | Path |
| --- | --- |
| Settings | `settings.yaml` |
| Flows | `flows/` |
| Actions | `actions.yml` |
| Agent workspaces | `workspaces/` |
| Application data and logs | `~/.local/share/hive/desktop/` |
| Account tokens | OS keychain |

Set `HIVE_DESKTOP_CONFIG_DIR` or `HIVE_DESKTOP_DATA_DIR` to move the config or data root. You can also move the flow, action, and workspace paths separately.

!!! tip "Ask the Hive workspace"
    The **Hive** workspace in **Chats** can update settings with the `hive-settings` skill. For example, ask it to change the polling interval or terminal font.

## Hive CLI compatibility config

Hive Desktop includes the Hive runtime it needs. A separately installed Hive CLI is not required.

If you use the Hive CLI, Desktop also reads its optional configuration file at startup. It checks `HIVE_CONFIG` first, then looks for `config.yaml`, `config.yml`, `hive.yaml`, or `hive.yml` under `$XDG_CONFIG_HOME/hive/` with `~/.config/hive/` as the fallback. No file is required. Hive uses built-in defaults when none exists.

**Settings ▸ Hive CLI** shows the exact path selected at startup. From there you can copy the path, open or reveal an existing file, or create a missing file and open it. Restart Hive Desktop after editing the file.

To choose a default agent, set `agents.default` to a configured agent profile. `HIVE_DEFAULT_AGENT` takes precedence when it names a configured profile.

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
```

- `polling.interval` has a minimum of 60 seconds.
- The local HTTP server provides webhooks, MCP, and terminal connections. It only binds to loopback. Set a fixed port if another local tool needs a stable webhook URL.
- `paths.tmux` accepts an absolute path when Hive cannot find tmux.
- `editor.command` accepts an executable name or absolute path without arguments.
- `agent_workspaces.dir` changes where Chats workspaces are stored.

Every scalar setting can be overridden for one launch with an environment variable based on its YAML path. For example, `polling.interval` becomes `HIVE_DESKTOP_POLLING_INTERVAL`.

## Updates

**Settings ▸ About** shows the current channel and controls automatic update checks.

To follow a different channel, edit `settings.yaml`:

```yaml
updates:
  channel: beta
```

Available channels are Stable, Beta, and Dev. An omitted channel follows the channel used for the current build.

## Telemetry

Telemetry is disabled by default. You can send traces, metrics, and logs to an OTLP endpoint with `telemetry.enabled`. Continuous profiles use a separate `telemetry.profiles.enabled` gate and push directly to a Pyroscope-compatible endpoint such as Grafana Cloud Profiles.

Grafana Cloud provides different URLs and basic-auth users for OTLP and Profiles. A Cloud Access Policy token can serve both when it includes `profiles:write`, but configure each destination separately:

```yaml
telemetry:
  enabled: true
  endpoint: https://otlp-gateway-prod-us-central-0.grafana.net/otlp
  instance_id: "123456"
  token: op://Private/Grafana Cloud/otlp-token
  profiles:
    enabled: true
    endpoint: https://profiles-prod-us-central-0.grafana.net
    user: "123456"
    token: op://Private/Grafana Cloud/profiles-token
```

Profile export collects CPU, allocation, and in-use heap profiles. It does not collect goroutine, mutex, or block profiles. OTLP and profile export can run independently.

Tokens must use `env:`, `file:`, or `op://` references. Hive rejects literal telemetry tokens in the settings file. Endpoints and users may also use these references.
