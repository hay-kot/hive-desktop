---
title: See your first items
description: The starter feeds you get for free, and the core loop — read, act, and let an agent configure the rest.
group: Getting started
order: 3
---

The moment you connect an account, Hive seeds a **starter workspace** so you get
value without configuring anything.

## The starter feeds

Out of the box you get feeds for:

- **My open PRs** — `is:open is:pr author:@me`
- **Assigned** — `is:open assignee:@me`
- **Notifications** — your GitHub notification inbox
- **Review requests** — PRs where your review was requested, which also fires a
  **Review requested** notification when a new one arrives.

Feeds show up in the left sidebar. Click a feed to see its items, and an item to
open its detail pane. New items land **unread**, and the sidebar shows unread
counts.

Items refresh by polling — **every 5 minutes** by default (tunable via
`polling.interval` in `settings.yaml`, minimum 60s). Press <kbd>r</kbd> to
refresh a feed now.

Handy feed keys: <kbd>j</kbd>/<kbd>k</kbd> move, <kbd>o</kbd> or <kbd>Enter</kbd>
opens the item in your browser, <kbd>e</kbd> archives/unarchives,
<kbd>u</kbd> toggles the unread-only filter, <kbd>Shift</kbd>+<kbd>U</kbd> marks
unread, <kbd>p</kbd> toggles the preview pane, and <kbd>Shift</kbd>+<kbd>A</kbd>
marks the feed read. <kbd>?</kbd> lists every shortcut and
[Keyboard shortcuts](/docs/configuration/keybindings) shows how to change them.

## Run an action

An **action** is a reusable command you run on an item — from the item's **…**
menu, under **Actions**. The app ships examples you can edit in
**Settings ▸ Actions**, for instance:

- **Review PR** — clones the repo and launches an agent session to review the PR.
- **start-implementation** — launches an agent session on an issue.
- **open-in-editor** — opens the item in your `$EDITOR`.

See [Actions](/docs/concepts/actions) for the full model and the `actions.yml` schema.

## Let an agent configure the rest

Hive's config is plain text, so you don't have to hand-write YAML. Open
**Chats** (<kbd>g</kbd> then <kbd>a</kbd>) and pick the **Hive** workspace:
it ships with a skill for each config file — flows, actions, settings, webhook
sources — that already includes the schema, the rules, a worked example, and
*your machine's real file paths*.

Tell it what you want — *"watch `owner/repo` for review requests and notify
me"* — and it writes the correct file; the app reloads it on save. It needs the
`claude` CLI on your PATH. [Agent workspaces](/docs/concepts/agent-workspaces)
covers what else it can do.

## Build a feed by hand

If you'd rather wire one yourself, open the **Flows** editor, drag a **GitHub
source** node onto the canvas, set its account and a search **Query** (e.g.
`is:open is:pr archived:false`), wire it into a **Feed** node, and click
**Deploy**. Add a **Notify** node in the same graph to get a system notification
when matching items arrive. [Flows](/docs/concepts/flows) builds one up node
by node.
