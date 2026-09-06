---
title: Sources and webhooks
description: Every source node Hive ships, what each one needs to connect, and how to point your own scripts at the local webhook endpoint.
group: Concepts
order: 2
---

A source is a node with no inputs: the start of a flow. Account-backed sources
are **polled** on the tick set by `polling.interval` (5 minutes by default,
minimum 60 seconds; <kbd>r</kbd> refreshes a feed now). Webhook sources are
**pushed** to, so a delivery lands as soon as it arrives. Only changed
observations produce work, and each connected account polls independently
with its own cache and rate-limit budget.

Connect an account under **Settings ▸ Integrations**. A flow then names it by
a `credential` reference such as `github/octocat`, never by a token: the
secret stays in the OS keychain and only the account name is written into a
file you might keep in a dotfiles repo.

## GitHub

Sign in with GitHub's device flow, or paste a personal access token. Either
way Hive asks for two scopes:

- **`repo`**, because PR and issue search on private repositories needs it.
  It is broader than what Hive does with it, which is read.
- **`notifications`**, for the notification inbox feed.

If you would rather not approve an OAuth app, **Use a token instead** on the
connect screen accepts a classic personal access token with those two scopes.

Triage never writes back: Hive reads your inbox and searches and leaves them
as they are.

```yaml
- id: reviews
  type: sources.github
  credential: github/octocat      # a connected account, from Settings ▸ Integrations
  kind: search                    # search, or notifications
  query: "is:open is:pr review-requested:@me"   # any GitHub search; search only
  limit: 50                       # optional; search caps at 100, notifications at 50
```

Any GitHub search syntax works in `query`, so `is:open assignee:@me`,
`org:acme label:bug`, and `involves:@me updated:>2026-01-01` are all feeds.
A `notifications` node drains the account's inbox and carries each thread's
reason (`mention`, `review_requested`, and so on), which a `github-filter`
node can route on.

Connect as many accounts as you have; sources on different accounts fetch
independently, so one being throttled does not stall another.

## Gitea and Forgejo

Connect an instance by URL and access token. The token needs `read:user`
(to resolve which account it is), plus `read:issue` for a search source and
`read:notification` for a notifications one. The account reference includes
the host, so one node type covers any number of instances.

```yaml
- id: forge
  type: sources.gitea
  credential: gitea/git.example.com-octocat
  kind: search                    # search, or notifications
  items: pulls                    # all (default), issues, or pulls
  state: open                     # open (default), closed, or all
  involving: [review_requested]   # created, assigned, mentioned, review_requested, reviewed
  owner: acme                     # one user or organization
  labels: [bug]
  text: "flaky"                   # free text over title and body
  limit: 50
```

Filters are typed fields rather than a query string because Gitea has no
search syntax to write one in. Everything is optional: a search with no
filters returns every open issue and pull request the token can see. Items
arrive in the same normalized shape GitHub's do, so the same filters and
actions work on both.

## Grafana

Connect a stack by pasting its URL and a service-account token. A Viewer-role
service account is enough to read alerts and query metrics; the IRM source
additionally needs `grafana-irm-app.alert-groups:read`, and a `403` there
means the role is too low rather than the token wrong. Three nodes share the
credential.

**Alerts**, one item per firing Grafana-managed alert, keyed by fingerprint
and archived when it stops firing:

```yaml
- id: firing
  type: sources.grafana_alerts
  credential: grafana/acme-prod
  matchers:                       # Alertmanager label matchers; all must match
    - team=payments
    - severity=~critical|warning
```

With no matchers this fetches the stack's entire active set, which on a large
shared stack is tens of thousands of alerts per tick. Scope it here rather
than in a function node downstream.

**IRM alerts**, one item per active Grafana IRM (OnCall) alert group, carrying
its triage state (`firing`, `acknowledged`, `silenced`, `resolved`). Use this
when a feed should mirror what an on-call channel sees:

```yaml
- id: oncall
  type: sources.grafana_irm_alerts
  credential: grafana/acme-prod
  integration: CFRPV98RPR1U8      # optional; pins the feed to one integration
  team: T1234                     # optional
```

**Metrics**, one item per node holding a PromQL query's result, updated on
every poll where the value changed:

```yaml
- id: error-rate
  type: sources.grafana_metrics
  credential: grafana/acme-prod
  datasource_uid: prom-main
  expr: sum(rate(http_requests_total{status=~"5.."}[5m]))
  title: 5xx rate                 # optional; defaults to the query
```

A function node downstream reads `msg.Payload.result` and decides whether the
item appears in a feed by routing it or not.

## PostHog

Connect a project by instance URL and a personal API key, then pick a project.
Both source nodes are experimental.

```yaml
- id: errors
  type: sources.posthog_errors    # needs project:read and error_tracking:read
  credential: posthog/acme
  status: active                  # active (default), resolved, suppressed, all
  order_by: last_seen             # last_seen, first_seen, occurrences, users, sessions
  date_from: -7d
  limit: 25

- id: insight-alerts
  type: sources.posthog_alerts    # needs project:read and alert:read
  credential: posthog/acme
  firing_only: false
```

An error-tracking issue is one item however many occurrences it has, and it
is archived only when PostHog reports it resolved, never because it aged out
of the window.

## A command

`sources.exec` runs a shell command on every poll tick and ingests what it
prints. Any CLI that can produce JSON becomes a source, with no scheduler and
no state file.

```yaml
- id: todo
  type: sources.exec
  command: "todo list --json | jq -s ."
  timeout: 30s                    # required; at most 2m
  cwd: ~/notes                    # optional
  interval: 1h                    # optional floor between runs
```

The output must be a **JSON array of objects** on stdout with exit code 0, and
each object needs a top-level `id`. The array is the whole snapshot: an item
that stops appearing is archived, and `[]` archives everything. A non-zero
exit, no output, or anything that is not an array is a failure that changes
nothing and is recorded in Activity, so a command that breaks cannot silently
empty your feed. It runs with the PATH your login shell reports and without
shell aliases.

## Webhooks

The one source that needs no account. Hive runs a local HTTP listener on
`127.0.0.1`, and any JSON POSTed to it becomes items in a flow. This makes it
the natural first source for a script, a CI job, or a tool you do not want to
hand a token to.

### The endpoint

A webhook node names a path under `/hooks/`:

```yaml
- id: ci
  type: sources.webhook
  path: ci-alerts                 # slug segments separated by /; several nodes may share one
  secret: a-long-random-string    # optional; senders must present it
  icon: zap
```

The secret is written into the flow file as-is, so treat a flow with one the
way you treat any file holding a shared secret. The full URL is the listener's
base plus that path:

```
http://127.0.0.1:<port>/hooks/ci-alerts
```

**Settings ▸ Integrations ▸ Webhooks** shows this install's base URL, lets
you pin the port, and turns the listener off. The port is allocated when the
listener starts and differs per machine, so a script that needs it should read
it from there or from `http.port` in `settings.yaml` once you have pinned one.
The listener only ever binds loopback; nothing off this machine can reach it.

### Sending a delivery

```sh
curl -X POST "http://127.0.0.1:$PORT/hooks/ci-alerts" \
  -H "Content-Type: application/json" \
  -H "X-Hive-Secret: $SECRET" \
  -d '{
    "id": "build-4821",
    "kind": "build",
    "title": "main is red: unit tests",
    "url": "https://ci.example.com/builds/4821",
    "state": "failing",
    "branch": "main"
  }'
```

Rules of the road:

- POST only, JSON body only, any shape, at most 1 MiB. An accepted delivery
  answers `202`.
- When the node has a `secret`, the request must carry it in the
  `X-Hive-Secret` header or it is rejected with `401`.
- Routes are resolved live from the current flows, so adding or editing a
  webhook node needs no restart.

### What the payload means

A few top-level fields have meaning; everything else rides along in the
payload for a function node or an action to read.

- `id` (string or number) is the item's stable key. Posting the same id again
  updates the same item. Without one, the body's content hash is the key, so
  an exact duplicate deduplicates and any change is a new item.
- `title` and `url` are what the feed shows.
- `kind` is the item's type label and what an action's `applies_to` matches.
  Omit it and the item is kind `Item`.
- `state` drives lifecycle: `resolved`, `closed`, or `done` archives the item
  with that reason, and a later delivery with any other state resurfaces it.
  A payload with no state is triaged by hand only.

A payload that already looks like a first-party item (`id`, `kind`, `repo`,
`title`, `url`) renders like one. Anything else still ingests and renders as
title plus link. To reshape a payload, put a `function` node after the webhook
node; its editor shows the last captured delivery and flags a payload missing
the fields the feed needs. The function must only change `msg.Payload`, since
`msg.Key` is how feed membership resolves.

### A minimal receiving flow

```yaml
version: 1
name: CI Alerts
nodes:
  - { id: ci, type: sources.webhook, path: ci-alerts }
  - { id: ci-feed, type: feed, icon: zap }
wires:
  - { from: ci, to: ci-feed }
```

> [!TIP] Ask the Hive workspace
> The **Hive** workspace's `hive-webhook-sources` skill knows this install's
> real base URL and can write both halves: the sender script and the receiving
> flow.
