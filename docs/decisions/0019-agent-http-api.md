# 0019 — Agent-facing HTTP API sharing the webhook port

- **Status:** accepted
- **Date:** 2026-07-27

## Context

Testing the notification pipeline against a live instance meant reading
`desktop-pipeline.db` directly (`inbox_item`, `webhook_capture`) to see what the
pipeline concluded, waiting up to a poll interval (≤60s) for an overlay to
surface, and enabling the webhook listener by hand. There was no supported,
agent-facing surface for *observing* the pipeline or *forcing* a re-poll — only
the SQLite schema, which a test then couples to.

`architecture.md` already names an `httpapi/` adapter (REST + SSE) as a target
that is "not yet built". This is the first slice of it, scoped to the harness
need: read the pipeline's conclusions and force a re-evaluation. The devserver
control API (ADR 0018) is the *act* half — this is the *observe/arrange* half.

Two shapes were open:

1. **Where does the API listen?** Its own loopback listener (a second port and a
   `development.api` host/port section) versus riding a server that already
   exists. The webhook listener (ADR 0016) already owns a loopback server with a
   lifecycle and a `WebhookService` that reports its bound address. `architecture.md`
   describes `httpapi` as "mounted via ServeHTTP at a Route" — i.e. mounted onto
   a server, not necessarily owning one.
2. **How does a test wait for a result?** The producer tick is synchronous
   through the event-log append, but the flow engine (`runtime.Engine`) is a
   single level-triggered goroutine that commits *after* the append, on its own
   schedule. So the moment a fetch or a webhook delivery returns, the inbox item
   may not be committed yet.

## Decision

1. **A driving adapter, `internal/adapter/httpapi`, over `*app.App`.** It maps
   `app.Kind` to HTTP status once and holds no logic of its own — reads go to
   `InboxService`, reload to `App.RefreshSources`. Endpoints: `GET /api/{help,
   status,feeds,inbox,inbox/events}` and `POST /api/sources/refresh`.

2. **It rides the webhook listener's loopback server, not a second port.**
   `webhook.Listener.MountAPI(prefix, http.Handler)` mounts an opaque handler
   the listener knows nothing about; `App.MountAPI` forwards to it; `main.go`
   builds the handler and mounts it before `Start`. One port serves both webhook
   push (`/hooks/…`) and this API (`/api/…`). The port is the webhook port,
   which devtools already writes to `launch.env`, so an agent reads one value and
   does both. The consequence is deliberate: **the API is up only when webhooks
   are enabled.** It is gated by `development.api.enabled` (loopback-only,
   default off), so a production build with webhooks on does not expose it.

3. **No server-side wait; the workflow is reload → read → retry.** `RefreshSources`
   drops the fetch caches and drives one producer tick, returning once the log is
   appended. The engine commits a moment later on its own goroutine, so a harness
   retries the read a few times. A long-poll `waitFor` and a synchronous engine
   drain were both considered and dropped — see Alternatives.

4. **devtools bootstraps it.** `prepare` writes `HIVE_DESKTOP_WEBHOOKS_ENABLED`,
   a free `HIVE_DESKTOP_WEBHOOKS_PORT`, and `HIVE_DESKTOP_DEVELOPMENT_API_ENABLED`
   into `launch.env`, so a prepared worktree boots with a reachable listener and
   API at a known port with no manual step.

## Consequences

- **One port, discovered once.** The agent reads the webhook port from
  `launch.env`; `GET /api/status` confirms the build and reports the webhook
  host/port/path so a devserver inline push target can be constructed from the
  same value.
- **API availability is coupled to webhooks.** Enabling the API without a webhook
  listener is a no-op (logged). Acceptable: the harness needs both, and dev turns
  webhooks on by default.
- **Layering is interim.** A whole-app API hosted inside the webhook *connector's*
  listener is a smell. When the product `httpapi` (REST + SSE) is built it may
  graduate to an App-owned loopback server; the webhook package only ever sees an
  opaque `http.Handler`, so that move is contained.
- **Reads return slices.** An `external_id` is the source's own id (a GitHub
  global node id, not `repo#num`) and can exist across profiles and source
  scopes, so `FindItems` / the by-id read return every match; the harness matches
  on the payload's `repo`/`num`/`reason`.

## Alternatives considered

- **Separate port + `development.api{enabled,host,port}` section.** Rejected:
  a second listener and settings block, and an extra value to discover, for no
  gain over sharing the webhook port the harness already needs.
- **Server-side `waitFor` long-poll** (subscribe to `InboxUpdated`, block until a
  predicate holds). Rejected: it puts real logic in the core for a concern a
  client retry handles, and reaching a *terminal* state can take more than one
  cycle (GitHub absence confirmation), which a client loop rides over naturally.
- **Synchronous engine drain** so `reload`/push block until committed. Rejected:
  the engine is deliberately single-goroutine and latch-driven; adding a
  block-until-committed path is invasive, still needs a separate settle for the
  webhook path, and still can't express "reached state X".
