---
title: How Hive works
description: Workspaces, flows, sources, feeds, actions, and notifications — the model behind the inbox, and where each piece lives on disk.
group: Concepts
order: 0
---

Hive is inbox-first: sources collect observations, the app durably records
each one as an item, and flows decide which items belong in which feed. An
item's identity and read state stay stable even as you change the flows,
filters, and feeds around it.

## Workspaces and flows

A **workspace** is a **flow** — a small pipeline graph. You wire **source**
nodes to terminal nodes (**feeds**, **actions**, **notifications**), optionally
through **filters** and **functions**. Flows are stored as plain YAML and are
hot-reloaded, so an edit takes effect without a restart. [Flows](/docs/concepts/flows)
covers the node types and builds one up from scratch.

## Sources

A source watches something and emits observations:

- **GitHub** — either a **search** query (any GitHub search, e.g.
  `is:open is:pr review-requested:@me`) or your **notifications** inbox. Each
  connected account polls independently, with its own cache and rate-limit
  budget.
- **Gitea and Forgejo** — a filtered issue and pull-request search, or the
  account's notifications, from any instance.
- **Grafana** — one item per firing alert, one per active IRM alert group, or
  a PromQL query's result.
- **PostHog** — error-tracking issues and insight alerts. *Experimental.*
- **A command** — anything that prints a JSON array, run on the poll tick.
- **Webhooks** — external systems POST JSON to a local endpoint; the one
  source that needs no account.

Account-backed sources are **polled** on a schedule (every 5 minutes by
default); only changed observations produce work. Webhook sources are pushed
to, so they land as soon as the delivery arrives. Every one of them, and what
each needs to connect, is on [Sources and webhooks](/docs/concepts/sources).

## Feeds and triage

A **feed** is a list in the sidebar. An item is never *owned* by a feed — a flow
*claims* an item for one or more feeds, so changing a filter recomputes what's
visible without losing the item or its history. New items arrive **unread**, and
you can archive, unarchive, or mark items unread right from the feed.

## Actions

An **action** is a named, reusable command defined in `actions.yml`. Four
types:

- **`launch-session`** — start an agent / coding session (e.g. clone a repo and
  hand a PR to an agent).
- **`shell`** — run a shell command.
- **`publish-message`** — publish to a message topic.
- **`clipboard`** — render text and put it on the clipboard.

Actions run from an item's **…** menu, or automatically when a flow routes an
item into an **action** node. An action can also say it belongs to a terminal
session or one of its windows instead, with `targets` — see
[Actions](/docs/concepts/actions).

## Notifications

A **notify** node raises a system banner when an item reaches it — with a
title and body rendered from the item's fields, plus optional severity, sound,
and a cooldown. The app's notification settings always win: if you've set
delivery to in-app only, a notify node produces a toast instead of a banner. On
macOS, banners require the [system permission](/docs/getting-started/notifications).

## The three areas

- **Inbox** (<kbd>g</kbd> <kbd>i</kbd>) is the feeds, the detail pane, and the
  workspace rail.
- **Code** (<kbd>g</kbd> <kbd>c</kbd>) attaches to the tmux sessions your
  actions launched, so an agent's terminal is readable in the app —
  [Terminal mode](/docs/concepts/terminal-mode).
- **Chats** (<kbd>g</kbd> <kbd>a</kbd>) runs a coding agent in a named
  workspace against Hive's own MCP tools —
  [Agent workspaces](/docs/concepts/agent-workspaces).

## Where config lives

Everything above is plain text you can edit directly. Hive uses XDG-style
paths on **both macOS and Linux**:

| What | Path |
| --- | --- |
| Settings | `~/.config/hive/desktop/settings.yaml` |
| Flows | `~/.config/hive/desktop/flows/` |
| Actions | `~/.config/hive/desktop/actions.yml` |
| Agent workspaces | `~/.config/hive/desktop/workspaces/` |
| Tokens | your OS keychain — not in any file |

You can relocate the roots with the `HIVE_DESKTOP_CONFIG_DIR` and
`HIVE_DESKTOP_DATA_DIR` environment variables; the full list is on the
[settings reference](/docs/configuration/settings#where-the-files-live).

> [!TIP] Or have an agent edit it
> The **Hive** workspace under Chats ships with a skill for each of these files,
> rendered with your install's real paths. Describe the change; it writes the
> YAML, and the app reloads it on save.
