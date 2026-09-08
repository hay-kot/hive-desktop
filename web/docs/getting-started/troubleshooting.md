---
icon: lucide/life-buoy
description: Fix common setup, source, notification, and terminal problems.
---

# Troubleshooting

Check **Activity** for flow, source, and action errors. Open **Settings ▸ System ▸ Diagnostics** for the application log.

## tmux is not installed

Code, Chats, and quick terminals require tmux 3.2 or newer.

```sh
brew install tmux
```

Return to Code after installation. Hive checks again without a restart. If tmux is installed in an unusual location, set `paths.tmux` in [Settings](../configuration/settings.md#advanced-configuration).

## A coding agent is not found

Hive currently supports the `claude` and `codex` commands. Check the selected workspace agent, then confirm the command is available:

```sh
which claude
which codex
```

Hive reads your login shell environment at launch. Fix the shell's `PATH`, then restart Hive.

## Notifications never appear as banners

Open **Settings ▸ Notifications** and check the master switch, delivery mode, and system permission.

On macOS, select **Allow notifications** if permission has not been requested. If permission is denied, enable Hive under **System Settings ▸ Notifications**.

## The GitHub code expired

Start **Connect GitHub** again and approve the new code. You can also select **Use a token instead** and provide a classic token with `repo` and `notifications` scopes.

## The feed is empty after skipping GitHub

Connecting GitHub after first run does not add the starter feeds automatically. Open the flow editor or ask the **Hive** workspace in Chats to create them.

See [Flows](../inbox/flows.md) for a minimal source-to-feed example.

## A config change did nothing

Hive keeps the last valid flow or action file active when an edit fails validation. Open Activity, fix the reported error, and save again.

Common errors include unknown fields, invalid durations, duplicate node IDs, and unsupported action targets.

## A webhook does not arrive

Check the sender's result:

- `401` means the `X-Hive-Secret` header is missing or incorrect.
- Connection refused means the local listener is disabled or the port changed.
- `202` with no feed item usually means the path, node state, flow state, or wiring is wrong.

**Settings ▸ Integrations ▸ Webhooks** shows the current URL and lets you set a fixed port. The webhook node editor shows the most recent accepted delivery.

## The terminal does not fill its pane

Another tmux client may be setting the shared window size. Detach or resize that client, or add this to `tmux.conf`:

```text
set -g window-size largest
```

See [Terminal mode](../code/terminal-mode.md#shared-tmux-sizing).

## Report a problem

Open **Settings ▸ System** and select **Report a problem**. The report includes build details, system details, a limited log tail, and a scrubbed configuration snapshot. It excludes account tokens and the pipeline database.

You can also open an issue at [github.com/hay-kot/hive-desktop/issues](https://github.com/hay-kot/hive-desktop/issues). Include the build number from **Settings ▸ About** and relevant log lines.

The default log path is:

```text
~/.local/share/hive/desktop/desktop.log
```
