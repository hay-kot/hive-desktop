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

## A feed source reports an error

Open Activity and read the error. The usual cause is a URL that points at a page instead of at its feed document. Look for a `<link rel="alternate" type="application/rss+xml">` tag in the page source, or try `/feed`, `/rss`, or `/atom.xml`.

Hive also fails the fetch when the feed asks for credentials, answers a non-2xx status, or is larger than 8 MiB. A failed fetch changes nothing, so the entries already in the feed stay where they are.

## The terminal does not fill its pane

Another tmux client may be setting the shared window size. Detach or resize that client, or add this to `tmux.conf`:

```text
set -g window-size largest
```

See [Terminal mode](../code/terminal-mode.md#shared-tmux-sizing).

## Report a problem

Open **Settings ▸ System** and select **Report a problem**. Hive writes a diagnostic bundle to your data directory and opens a new issue at [github.com/hay-kot/hive-desktop/issues](https://github.com/hay-kot/hive-desktop/issues) with your version and platform filled in. Nothing is uploaded.

The bundle always holds build and system details. Logs, settings, flows, and actions are separate switches, and all four start off. Switch on what the problem needs.

Tokens, secrets, and API keys are removed from everything the bundle holds. Names are not. A log tail names your home directory, repositories, and branches, and flows name the orgs and hosts they poll. Read the file before you attach it to a public issue.

Bundles are written to:

```text
~/.local/share/hive/desktop/reports/
```

Read one with `gunzip -c hive-report-<id>.json.gz | jq .`. Attach it to the issue by dragging it into the **Diagnostic bundle or logs** box, or paste the relevant log lines instead. The default log path is:

```text
~/.local/share/hive/desktop/desktop.log
```
