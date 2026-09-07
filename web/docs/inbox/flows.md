---
icon: lucide/git-branch
description: The graph behind every workspace. Nodes, wiring, the node types, the editor, and a feed built up one node at a time.
---

# Flows

The graph behind every workspace. Nodes, wiring, the node types, the editor, and a feed built up one node at a time.

A flow is a small pipeline graph. Source nodes emit items, process nodes filter
or reshape them, and destination nodes decide what happens to whatever gets
through: it lands in a feed, raises a notification, or fires an action.

## Workspaces, flows, and feeds

The three words name one thing at three levels:

- A **flow** is a file, `flows/<id>.yaml` in your config directory. The
  filename stem is the flow's id and is never written inside the file. A
  sibling `flows/<id>.ui.yaml` holds the canvas positions; the app maintains it.
- A **workspace** is what the sidebar calls a flow. The rail on the left lists
  one tile per flow, and switching workspaces switches which flow's feeds you
  are looking at.
- A **feed** is a `feed` node inside that flow. A workspace's feeds are exactly
  its feed nodes, in the sidebar under the workspace's name.

So "create a workspace" means "create a flow file", and "add a feed" means "add
a feed node and wire something into it". An item is never owned by a feed: a
flow *claims* an item for one or more feeds, and changing a filter recomputes
what is visible without losing the item, its read state, or its history.

!!! tip "Ask the Hive workspace"
    The **Hive** workspace under **Chats** carries the `hive-flows` skill, which
    is the same node reference as this page, rendered with your install's real
    paths. Describe the feed you want and it writes the file. Everything below
    is what it knows.

## Anatomy of a flow file

```yaml
version: 1
name: Frontend Triage
enabled: true            # optional, default true
nodes:
  - id: prs              # unique within the flow
    type: sources.github # one of the node types below
    name: Open PRs       # optional display name
    credential: github/octocat
    kind: search
    query: "is:open is:pr review-requested:@me"
  - id: review-feed
    type: feed
    icon: git-branch
wires:
  - { from: prs, to: review-feed }
```

Every node has an `id`, a `type`, an optional `name`, an optional `disabled`
flag (a disabled node drops everything it receives), and that type's own
fields **flattened at the same level**, never nested under a `config:` key.

A wire is `{ from, out, to }`. `from` and `to` are node ids. `out` is the
output port index on the source node and defaults to `0`, so you only write it
for a node with more than one output. Ports are fixed by type: wiring into a
source, out of a terminal, or to a port the type does not have is a validation
error.

Three rules hold for every config file Hive reads:

- The schema is strict. An unknown key is an error, not a warning.
- The app watches the file and reloads on save. No restart, no build step.
- A file that fails to parse is rejected as a whole and the last good version
  stays live, so a typo degrades to "nothing changed" rather than a broken app.

## Node types

### Sources

A source has no inputs; it is where a flow starts. Polled sources run on the
tick set by `polling.interval` (5 minutes by default, minimum 60 seconds).
Webhooks are pushed, so they land as soon as the delivery arrives.

| Type | Emits | Fed by |
| --- | --- | --- |
| `sources.github` | a GitHub search query, or the account's notification inbox | polling |
| `sources.gitea` | a filtered Gitea or Forgejo search, or its notifications | polling |
| `sources.grafana_alerts` | one item per firing Grafana-managed alert | polling |
| `sources.grafana_irm_alerts` | one item per active Grafana IRM alert group | polling |
| `sources.grafana_metrics` | one item holding a PromQL query's result | polling |
| `sources.posthog_errors` | one item per PostHog error-tracking issue | polling |
| `sources.posthog_alerts` | one item per PostHog insight alert | polling |
| `sources.exec` | whatever JSON a command prints, as a snapshot | polling |
| `sources.webhook` | JSON POSTed to a local endpoint | push |

Each one's fields and quirks are on [Sources and webhooks](sources.md).
Every account-backed source names its account with a `credential` reference
such as `github/octocat`, never a token: the flow file is meant to live in a
dotfiles repo, and the secret stays in the keychain.

### Process

**`github-filter`** narrows a stream of GitHub-shaped items. One input, two
outputs: port 0 passes, port 1 fails. Leave port 1 unwired to drop rejects, or
wire it somewhere else to route them.

```yaml
- id: mine
  type: github-filter
  repos: ["colonyops/*"]          # doublestar globs on owner/repo
  exclude_authors: ["*[bot]"]     # case-insensitive globs
  labels: ["bug"]                 # any label matches
  types: [pr]                     # pr and/or issue
  reasons: [review_requested]     # GitHub notification reasons; search items never match
```

Groups AND together, values within a group OR, and an exclude group wins over
its include.

**`function`** runs your own JavaScript against every message. One input, up
to 16 outputs.

```yaml
- id: route
  type: function
  outputs: 2
  timeout: 5s
  on_message: |
    if (msg.Payload.state === "closed") return null;   // discard
    if (msg.Payload.labels?.includes("urgent")) return [msg, null];
    return [null, msg];
```

The body is `function on_message(msg, node, state, kv)`. Return one message
for port 0, an array of messages for port 0, a port-indexed array once
`outputs` is more than 1, or `null` to discard. `msg.Payload` is the item as the
source shaped it and is the only part carried to the next node, so put any
data you add there. `state` lives for one deploy; `kv` is a small durable
store scoped to the node that survives restarts and is the memory behind
"notify once" and "notify on change". A function fed by a metrics source runs
on every changed poll, so keep it a pure function of its input.

### Destinations

A destination has one input and no outputs.

```yaml
- id: review-feed
  type: feed
  icon: git-branch                # from the curated feed icon set
  description: PRs waiting on me  # up to 500 characters, cosmetic

- id: ping
  type: notify
  title: "{{ .Payload.repo }} needs review"
  body: "{{ .Payload.title }}"
  severity: info                  # info, success, warning, error
  sound: true
  cooldownSeconds: 300            # per-item floor; 0 disables

- id: spawn-review
  type: action
  action: review-pr               # an id from actions.yml
```

A **feed** never interrupts. It is a place items live, read at your own pace.
A **notify** node is how a flow says "this one is worth a banner": it
deduplicates on the item's occurrence key, applies the cooldown, drops a
notification that sat undelivered for more than ten minutes, and never fires
during a replay. The app's notification settings always win over it. An
**action** node fires the named action for every item routed there, once per
item, and the action's own definition decides what that means
([Actions](actions.md)).

## The editor

Every workspace has a canvas: the node palette on one side, the graph in the
middle, and a node editor for the selected node. Dragging a wire between ports
is the same as writing a `wires:` entry, and **Deploy** writes the file. A
webhook node's editor shows the last captured delivery so you can see what a
function downstream has to work with.

The file and the canvas are two views of one thing. Edit the YAML in your
editor and the canvas follows; deploy from the canvas and the YAML follows.

To test a change without waiting for a poll, the app's MCP server has an
`execute_flow` tool that dry-runs a flow, saved or not, against a payload you
supply, and reports what every node received, emitted, and dropped, with the
function nodes' console output. Nothing is committed. The Hive workspace can
call it for you.

## A feed, built up

Start with one source and one feed:

```yaml
version: 1
name: Reviews
nodes:
  - { id: prs, type: sources.github, credential: github/octocat, kind: search, query: "is:open is:pr review-requested:@me" }
  - { id: reviews, type: feed, icon: git-branch }
wires:
  - { from: prs, to: reviews }
```

Filter out bots, and keep what they open in a second feed instead of dropping
it:

```yaml
nodes:
  - { id: prs, type: sources.github, credential: github/octocat, kind: search, query: "is:open is:pr review-requested:@me" }
  - { id: humans, type: github-filter, exclude_authors: ["*[bot]"] }
  - { id: reviews, type: feed, icon: git-branch }
  - { id: bots, type: feed, icon: sparkles }
wires:
  - { from: prs, to: humans }
  - { from: humans, out: 0, to: reviews }
  - { from: humans, out: 1, to: bots }
```

Add a banner for the ones from people, as a second branch off the same filter
so the item both lands in the feed and interrupts you:

```yaml
nodes:
  # ...the four nodes above...
  - id: ping
    type: notify
    title: "Review requested: {{ .Payload.repo }}"
    body: "{{ .Payload.title }}"
wires:
  - { from: prs, to: humans }
  - { from: humans, out: 0, to: reviews }
  - { from: humans, out: 0, to: ping }
  - { from: humans, out: 1, to: bots }
```

Finally, hand each one to an agent automatically by routing the same branch
into an action node whose action is a headless `launch-session`:

```yaml
nodes:
  # ...
  - { id: spawn-review, type: action, action: review-pr }
wires:
  # ...
  - { from: humans, out: 0, to: spawn-review }
```

The `review-pr` action itself lives in `actions.yml`; the
[Actions](actions.md) page shows one that clones the repository and
opens a session with the PR as its prompt.

## What a deploy does not do

Recomputing a flow, on startup or after a deploy, replays every source's
current snapshot through the graph to work out which items belong in which
feed. That replay commits feed membership only. Notify and action nodes see
nothing during it, so editing a filter never re-pings you about items you
already have, and a restart never re-runs an action.
