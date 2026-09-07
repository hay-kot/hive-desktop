---
icon: lucide/rocket
description: Install Hive Desktop, what its pieces are, and the shortest path from a fresh install to a working feed.
---

# Getting started

Install Hive Desktop, what its pieces are, and the shortest path from a fresh
install to a working feed.

Hive Desktop pulls the work that wants your attention into one place: pull
requests, issues, review requests, notifications, firing alerts, and anything
that can POST a webhook. It records each of those as an item in a local queue,
then runs **flows** you own to decide which items land in which feed, which
ones interrupt you, and which ones get handed to a coding agent or a command.

Everything runs on your machine. Triage never writes back to GitHub, tokens
stay in the OS keychain, and the configuration is plain YAML you can keep in
your dotfiles.

## Install

<span id="install-version"></span>

One command pulls the right binary for your machine from the latest release,
checks it against the published checksum, and installs it.

```sh
curl -fsSL https://hivedesktop.com/install.sh | bash
```

=== "macOS"

    **Apple silicon and Intel.** Detects your arch, verifies the checksum,
    installs to `/Applications/Hive.app`, and symlinks `hive` onto your PATH.

=== "Linux"

    **Linux builds are coming.** The release pipeline ships macOS today; Linux
    x86_64 and arm64 are next in line. The installer already knows how to
    install them, and [building from source](build-from-source.md) works now.

Good to know:

- **Updating.** The app updates itself from its release channel; re-running the
  install command does the same by hand. [settings.yaml](../configuration/settings.md#updates)
  covers the channels.
- **Beta and dev builds.** `curl -fsSL https://hivedesktop.com/install.sh | bash -s -- --channel dev`
- **Read it first.** Drop the `| bash` to inspect the script.
- **Uninstall.** Delete `/Applications/Hive.app` and the `hive` symlink the
  installer put in `/usr/local/bin` (or `~/.local/bin`). Your configuration
  under `~/.config/hive/desktop` and the data under `~/.local/share/hive` stay
  until you delete them.

## The mental model

A **workspace** is a flow: a small graph with sources on one side, feeds on the
other, and optional filters, functions, actions, and notifications in between.

- **Sources** watch something and emit items. GitHub and Gitea searches and
  notification inboxes, Grafana alerts and PromQL queries, PostHog errors, a
  local webhook endpoint your own scripts post to, or a command that prints
  JSON. [Sources and webhooks](../inbox/sources.md) lists them all.
- **Feeds** are the lists in the sidebar where matching items land. An item
  arrives unread, and you read, archive, or act on it from there.
- **Actions** are commands you run on an item or wire into a flow: review a
  PR in an agent session, open something in your editor, copy a ready-made
  command, publish a message. See [Actions](../inbox/actions.md).
- **Notifications** raise a system banner when an item reaches a notify node,
  with dedup and a cooldown so one PR does not ping you on every poll.

The app has three areas, and these docs are grouped the same way. **Inbox** is
the feeds. **Code** attaches to the tmux sessions your feeds launch, so an
agent's terminal is readable inside the app
([Terminal mode](../code/terminal-mode.md)). **Chats** runs a coding agent in
a named, durable workspace against Hive's own MCP tools
([Agent workspaces](../chats/agent-workspaces.md)).

## First run

Once the app is open, three pages take you from first launch to a live feed:

1. [Sign in to GitHub](sign-in.md), the first-run device flow.
2. [Turn on notifications](notifications.md), required for banners on macOS.
3. [See your first items](first-feed.md), the starter feeds and the core loop.

## Let an agent do the configuring

!!! tip "The Hive workspace"
    Hive ships a Chats workspace named **Hive** that carries every skill this
    build knows: flows, actions, settings, webhooks, agent workspaces, and the
    app's MCP server. Open **Chats** (press <kbd>g</kbd> then <kbd>a</kbd>),
    pick **Hive**, and describe the feed, filter, or shortcut you want. The
    agent edits the same YAML files these docs describe, and the app reloads
    them on save. It needs the `claude` CLI on your PATH.

Every reference page here still tells you what the agent wrote, so you can
read it, tweak it by hand, or write it yourself from the start.

## Go deeper

- [How Hive works](../inbox/how-it-works.md): workspaces, flows, sources, feeds, actions, and notifications as one model.
- [Flows](../inbox/flows.md): the node types, wiring, the editor, and a worked example.
- [settings.yaml](../configuration/settings.md): every setting, its default, and the environment variable that overrides it.
- [Keyboard shortcuts](../configuration/keybindings.md): the defaults and how to rebind them.
- [Troubleshooting](troubleshooting.md): tmux missing, an agent not on PATH, a denied notification permission, and how to report the rest.

These docs are also published as [llms.txt](/llms.txt) and
[llms-full.txt](/llms-full.txt), and every page has a Markdown twin at its own
URL plus `.md`, so a coding agent can read them the way you do.
