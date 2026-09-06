---
title: Overview
description: What Hive Desktop is, how its pieces fit together, and the shortest path from a fresh install to a working feed.
group: Getting started
order: 0
---

Hive Desktop pulls the work that wants your attention into one place: pull
requests, issues, review requests, notifications, firing alerts, and anything
that can POST a webhook. It records each of those as an item in a local queue,
then runs **flows** you own to decide which items land in which feed, which
ones interrupt you, and which ones get handed to a coding agent or a command.

Everything runs on your machine. Triage never writes back to GitHub, tokens
stay in the OS keychain, and the configuration is plain YAML you can keep in
your dotfiles.

## The mental model

A **workspace** is a flow: a small graph with sources on one side, feeds on the
other, and optional filters, functions, actions, and notifications in between.

- **Sources** watch something and emit items. GitHub and Gitea searches and
  notification inboxes, Grafana alerts and PromQL queries, PostHog errors, a
  local webhook endpoint your own scripts post to, or a command that prints
  JSON. [Sources and webhooks](/docs/concepts/sources) lists them all.
- **Feeds** are the lists in the sidebar where matching items land. An item
  arrives unread, and you read, archive, or act on it from there.
- **Actions** are commands you run on an item or wire into a flow: review a
  PR in an agent session, open something in your editor, copy a ready-made
  command, publish a message. See [Actions](/docs/concepts/actions).
- **Notifications** raise a system banner when an item reaches a notify node,
  with dedup and a cooldown so one PR does not ping you on every poll.

Two more areas sit beside the inbox. **Code** attaches to the tmux sessions
your feeds launch, so an agent's terminal is readable inside the app
([Terminal mode](/docs/concepts/terminal-mode)). **Chats** runs a coding agent
in a named, durable workspace against Hive's own MCP tools
([Agent workspaces](/docs/concepts/agent-workspaces)).

## Start here

Install with the one-liner on the [install page](/install), or
[build from source](/docs/getting-started/build-from-source). Once the app is
open, three pages take you from first launch to a live feed:

1. [Sign in to GitHub](/docs/getting-started/sign-in), the first-run device flow.
2. [Turn on notifications](/docs/getting-started/notifications), required for banners on macOS.
3. [See your first items](/docs/getting-started/first-feed), the starter feeds and the core loop.

## Let an agent do the configuring

> [!TIP] The Hive workspace
> Hive ships a Chats workspace named **Hive** that carries every skill this
> build knows: flows, actions, settings, webhooks, agent workspaces, and the
> app's MCP server. Open **Chats** (press <kbd>g</kbd> then <kbd>a</kbd>),
> pick **Hive**, and describe the feed, filter, or shortcut you want. The
> agent edits the same YAML files these docs describe, and the app reloads
> them on save. It needs the `claude` CLI on your PATH.

Every reference page here still tells you what the agent wrote, so you can
read it, tweak it by hand, or write it yourself from the start.

## Go deeper

- [How Hive works](/docs/concepts/how-it-works): workspaces, flows, sources, feeds, actions, and notifications as one model.
- [Flows](/docs/concepts/flows): the node types, wiring, the editor, and a worked example.
- [settings.yaml](/docs/configuration/settings): every setting, its default, and the environment variable that overrides it.
- [Keyboard shortcuts](/docs/configuration/keybindings): the defaults and how to rebind them.
- [Troubleshooting](/docs/help/troubleshooting): tmux missing, an agent not on PATH, a denied notification permission, and the rest.

These docs are also published as [llms.txt](/llms.txt) and
[llms-full.txt](/llms-full.txt), and every page has a Markdown twin at its own
URL plus `.md`, so a coding agent can read them the way you do.
