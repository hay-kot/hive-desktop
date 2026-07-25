# devserver

A development-only proxy between desktop instances and the GitHub API. It does two things:

1. **Caches GitHub responses** across every dev instance and every restart, so N worktrees share one rate-limit budget instead of each burning their own.
2. **Simulates events** — config-driven overlays rewrite what GitHub appears to say, and a webhook pusher injects payloads that never came from GitHub at all. A dashboard drives both.

Not shipped, not imported by the app, loopback-only. Design rationale: [ADR 0017](../../docs/decisions/0017-devserver-github-proxy.md).

## Quick start

```bash
mise run devserver                                    # starts on 127.0.0.1:7777
HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE=http://127.0.0.1:7777 mise run desktop:dev
```

Open <http://127.0.0.1:7777> for the dashboard. No setup step: the mise task passes the checked-in [`devserver.yaml`](devserver.yaml), which ships working scenarios and webhook payloads.

That config declares **no overlays**, so starting devserver never rewrites what a connected instance sees — you get the caching proxy, which is the whole fix for the rate-limit problem, and nothing changes until you click something. A proxy that silently faked data the moment it started would make every subsequent bug suspect.

The app logs a warning at startup when the override is set, and every proxied response carries an `X-Devserver-Outcome` header (`hit`, `miss`, `revalidated`, `stale`, `passthrough`).

## Why it helps

GitHub's limits are **per token**, so concurrent dev instances share one budget. Three things make development worse than the shipped app:

- `LiveProvider`'s cache is in-memory, and `wails3 dev` restarts the Go process on every backend edit
- `dev-data-dir.sh` gives each worktree an ephemeral data dir, so nothing is shared
- `ConfirmTerminal` is unbatched and uncached — a churning query bursts individual REST calls, which trips *secondary* limits early

devserver also stores ETags and revalidates with `If-None-Match`. A 304 costs no primary quota, so even an expired entry is usually far cheaper than a refetch. The client itself has none of this ([#62](https://github.com/hay-kot/hive-desktop/issues/62)).

## Config

`mise run devserver` passes the checked-in `cmd/devserver/devserver.yaml`. Edit it freely — it is a development default, not a fixture.

For a personal config that stays out of git, write `$XDG_CONFIG_HOME/hive/desktop/devserver.yaml` and run `go run ./cmd/devserver` with no `--config`; that path is the default when the flag is absent. An explicit `--config` that does not exist is an error rather than a silent fallback.

Every section is optional — the annotated shipped file is the reference.

```yaml
listen: 127.0.0.1:7777
upstream: https://api.github.com
cache:
  ttl: 5m                       # how long before revalidating

# Not set in the shipped config — anything here applies from the first request,
# before you have touched the dashboard. Add entries only to pin a state you
# want back on every restart.
overlays:
  - match: {repo: hay-kot/hive-desktop, num: 58}
    set:
      reason: approval_requested
      labels: [needs-review]

scenarios:                      # multi-step sequences, one dashboard click
  pr-review-cycle:
    description: Review requested, then approval requested, then merged
    steps:
      - match: {repo: hay-kot/hive-desktop, num: 58}
        set: {reason: review_requested}
      - wait: 60s
      - match: {repo: hay-kot/hive-desktop, num: 58}
        set: {state: merged, absent: true}

webhooks:
  # Commented out in the shipped config: the desktop's webhook port is random
  # per install, so no committed URL can be right.
  targets:
    - name: local-desktop
      url: http://127.0.0.1:24681/hooks/devserver   # from Settings ▸ Webhooks
      secret: dev-secret                            # the node's X-Hive-Secret
  payloads:
    pr-opened:
      id: devserver-pr-1
      kind: PR
      repo: acme/widgets
      title: Add retry to fetch
      url: https://github.com/acme/widgets/pull/1
      state: open
```

Overlay state is in-memory. Config seeds it; the dashboard and control API mutate it; a restart returns to exactly what the file says. A debugging session cannot leave permanent fake data behind.

The shipped scenarios point at `hay-kot/hive-desktop#58` as a placeholder. Retarget them at an item your feed actually shows — the dashboard lists everything the proxy has observed, so start devserver, let an instance poll once, and copy a repo and number from there.

### Mutation fields

| Field | Effect |
| --- | --- |
| `state` | `open`, `closed`, or `merged` — drives lifecycle transitions |
| `labels` | Replaces the label set |
| `reason` | Notification reason; becomes the feed's activity summary |
| `title`, `body` | Display text |
| `draft` | Marks a PR draft |
| `absent` | Removes from search results while single-item endpoints still answer |
| `updated_at` | Overrides the timestamp; auto-stamped to now when omitted |

`updated_at` is auto-stamped because the classifier ignores any change that does not advance it — an un-stamped mutation would be silently invisible.

## What "simulating an event" actually means

The desktop's `github-source` reads exactly four things: `state`, `updatedAt`, `labels`, and a notification `reason`. It never fetches review state. So the action vocabulary is GitHub's own:

| Action | What the app sees |
| --- | --- |
| `review-requested` | "Review requested" |
| `approval-requested` | "Approval requested" |
| `comment` | "New comment activity" |
| `ci-activity` | "CI status changed" |
| `mention` | "Mentioned on GitHub" |
| `state-change` | "State changed on GitHub" |
| `draft` / `ready` | Draft flag |
| `merge` / `close` | Terminal transition + drops out of search |
| `reopen` | Back to open, back into search |

**There is no `approve` action.** GitHub has no such notification reason, and the app never reads review state — an approval reaches it as activity on the item. Inventing a richer vocabulary would teach a wrong model of how the app behaves.

`merge` and `close` also set `absent`, because that is how GitHub behaves once an item leaves an `is:open` query, and it is the only way to exercise the desktop's `ConfirmAbsence` path. One overlay is rendered consistently into all three response shapes the app reads — search nodes, notification entries, and single-item REST — so a simulated merge cannot produce a state the real API could never return.

## Control API

The dashboard is a thin renderer over these. All under `/_ctl/`.

```bash
# everything the dashboard shows, in one read
curl localhost:7777/_ctl/state

# quick action
curl -XPOST localhost:7777/_ctl/action \
  -d '{"repo":"hay-kot/hive-desktop","num":58,"action":"merge"}'

# arbitrary mutation
curl -XPOST localhost:7777/_ctl/overlay \
  -d '{"repo":"hay-kot/hive-desktop","num":58,"set":{"state":"open","labels":["wip"]}}'

curl -XPOST localhost:7777/_ctl/overlay/clear -d '{"repo":"hay-kot/hive-desktop","num":58}'
curl -XPOST localhost:7777/_ctl/overlays/clear
curl -XPOST localhost:7777/_ctl/scenarios/pr-review-cycle/run
curl -XPOST localhost:7777/_ctl/cache/purge

# push a webhook; overrides merge over the named payload
curl -XPOST localhost:7777/_ctl/webhooks/push \
  -d '{"target":"local-desktop","payload":"pr-opened","overrides":{"state":"resolved"}}'

# or an inline body
curl -XPOST localhost:7777/_ctl/webhooks/push \
  -d '{"target":"local-desktop","body":{"id":"x","title":"Inline","state":"open"}}'
```

Unknown JSON fields are rejected, so a typo in a hand-written call fails loudly instead of silently doing nothing.

## Webhook pusher

Targets point at a running instance's local webhook listener. Get the base URL from **Settings ▸ Webhooks** (the port is random per install — see ADR 0007) and append a `webhook-source` node's path. The `secret` must match that node's configured secret.

A payload keyed on a stable top-level `id` updates the same inbox item on re-delivery; without one the listener hashes the body, so every push is a new item. Payloads that follow the [canonical item contract](../../docs/decisions/0008-canonical-item-contract.md) render as first-party feed rows.

## Scope

Six endpoints are cached and rewritten: `POST /graphql`, `GET /notifications`, `GET /user`, and issue/pull single items. Everything else passes straight through, so a new client call keeps working without devserver knowing about it.

Only 2xx responses are cached — caching a 401 would outlive the bad token, and caching a rate-limit 403 would extend the outage past its own reset. When upstream fails and a cached copy exists, it is served stale rather than failing the instance.

The cache key includes a hash of the bearer token, so two accounts pointed at one devserver never see each other's data. The token is hashed, never stored.

The OAuth device flow is **not** proxied — `development.github.api_base` only moves the API base. Sign-in still goes to github.com.
