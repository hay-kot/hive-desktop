---
name: desktop-api
description: Observe the Hive Desktop notification pipeline and force a re-poll through its loopback agent HTTP API, instead of reading desktop-pipeline.db. Use to assert what an event produced in the inbox, force a refresh after a devserver overlay/action/scenario, discover the webhook port for a push, or confirm an actions.yml/flow edit actually loaded.
compatibility: Requires a running desktop instance built from this worktree. The loopback HTTP server (webhook listener + agent API) is on by default; launch.env pins its port via HIVE_DESKTOP_HTTP_PORT. curl and jq.
---

# Observe the pipeline through the agent API

The desktop exposes a loopback read + reload API over `app.App` (ADR 0021), so a
test asserts against a supported surface instead of `desktop-pipeline.db`. One
loopback `http` server (on by default) serves both webhook push (`/hooks/…`) and
this API (`/api/…`) on the same port.

Pair this with the **devserver** skill: devserver *acts* (rewrites GitHub,
pushes webhooks), this API *observes and reloads*.

## 1. Find the port

The HTTP port is written to the worktree's `launch.env`:

```bash
API=127.0.0.1:$(grep HIVE_DESKTOP_HTTP_PORT launch.env | cut -d'"' -f2)
curl -s $API/api/status  | jq .   # {webhook:{running,host,port,pathPrefix}, api:{pathPrefix}}
curl -s $API/api/version | jq .   # revision + dirty flag of the running build
```

If `/api/status` refuses the connection, the app is not running, was not built
from this worktree, or the HTTP server is disabled (`http.enabled: false`). A
fresh `mise run desktop:dev:prepare` turns it on.

## 2. The loop: reload → read → retry

There is **no server-side wait**. `RefreshSources` returns once the event log is
appended; the flow engine commits a moment later on its own goroutine. So force
the poll, then read and retry a few times:

```bash
curl -sf -XPOST $API/api/sources/refresh    # drop fetch caches, re-poll pull sources
for i in $(seq 1 10); do
  hit=$(curl -s "$API/api/inbox" \
    | jq --arg repo acme/widgets --argjson num 42 \
        '[.items[] | select(.payload.repo==$repo and .payload.num==$num)][0]')
  [ "$hit" != "null" ] && echo "$hit" | jq '{lifecycle, sourceState, unread, reason: .payload.reason}' && break
  sleep 1
done
```

A **webhook push** appends directly (no refresh needed) — push via devserver,
then read + retry the same way.

## 3. Read endpoints

```bash
# all items, newest first (payload carries repo/num/reason/state)
curl -s "$API/api/inbox" | jq '.items[] | {externalId, title, lifecycle, sourceState, unread}'

# one feed (presence == routed there); needs a profile (read it off any item's .profileId)
curl -s "$API/api/inbox?profile=$P&feed=$FEED" | jq '.items | length'
curl -s "$API/api/inbox?profile=$P&feed=$FEED&archived=true" | jq '.items | length'

# by source id (a GitHub global node id, NOT repo#num); returns every match
curl -s "$API/api/inbox?externalId=$EXT" | jq '.items'

# per-feed counts, and one item's event history
curl -s "$API/api/feeds?profile=$P" | jq '.feeds'
curl -s "$API/api/inbox/events?itemId=$ID" | jq '.events'
```

## 4. Did my config edit load?

Both config surfaces report their own load status, so an edit is verified by
reading it back rather than by clicking through the UI. A file that fails to
parse leaves the previous version in effect, so `valid` is the only signal that
separates "accepted" from "rejected and ignored":

```bash
curl -s "$API/api/actions"  | jq '{path, valid, error, ids: [.actions[].id]}'
curl -s "$API/api/profiles" | jq '[.profiles[] | {id, valid}]'
```

`/api/actions` returns each action's type-specific config too, so a template
edit is confirmed by the value that came back — `.actions[] |
select(.id=="…") | .clipboard.textTemplate`.

## Guardrails

- **Match on the payload**, not the external id: `external_id` is the source's
  own id (a GitHub global node id), while the devserver and your test think in
  `repo`/`num`. Filter `.items[] | select(.payload.repo==… and .payload.num==…)`.
  The same external id can exist across profiles/scopes, so by-id reads return a
  slice.
- **Reload only helps pull sources.** `POST /api/sources/refresh` re-polls
  `sources.github`; it does nothing for webhook-fed feeds (those append on
  delivery). It returns `503` in mock modes (no producer).
- **The API needs the HTTP server on.** It shares that loopback server (on by
  default); with `http.enabled: false` there is nothing to mount on.
- **Read-only + reload.** This surface never mutates triage or config; drive
  changes through the app or the devserver, and assert here.
