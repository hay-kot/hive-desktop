# 0017 — Development GitHub proxy and event simulator

- **Status:** accepted
- **Date:** 2026-07-26

## Context

GitHub's API rate limits are scoped to the **token**, not the caller. Every concurrent development instance draws from one 5000/hour budget, and development multiplies that in ways the shipped app never sees:

1. `LiveProvider`'s fetch cache is an in-memory map built fresh in `NewLiveProvider` (`internal/app/sources/github/feed/live.go:83`). `wails3 dev` restarts the Go process on every backend edit, so each edit refetches every source immediately — the persistent worktree-local instance ADR 0014 introduced does not help, because the cache never reaches disk.
2. Each worktree owns an isolated data and config root (ADR 0014). That isolation is the point — two worktrees must not mutate each other's state — but it also means two instances polling the same queries share no fetch results.
3. `ConfirmTerminal` (`live.go:422`) is unbatched, uncached, and outside the singleflight group — one REST call per item that left a source result, per tick. A churning query produces a burst, which is the shape that trips GitHub's *secondary* limits well before the primary quota is exhausted.

The client also has no ETag support anywhere; the only conditional request is `If-Modified-Since` on `/notifications` (`ghclient/client.go:371`). Since ADR 0015 the client is **owned code**, so that is now a local fix rather than a cross-repo round trip — ADR 0015 names cache integration as one of the things ownership was meant to unblock, and it remains worth doing (#62).

It would not remove the need for this decision. A client-side cache lives in one process, and points 1 and 2 are about *state that cannot be shared between processes*. Even a perfect in-client cache leaves N instances and every `wails3 dev` restart each paying full price. That is the gap devserver closes, and it is the only part of this problem a better client cannot reach.

Separately, exercising lifecycle workflows (a PR getting review activity, then merging) meant waiting for real GitHub state to change, or hand-editing fixtures. The `sources.webhook` connector (ADRs 0007, 0016) can already inject arbitrary events, but only into flows built on webhook nodes — not into the `sources.github` nodes where the interesting classification logic lives.

## Decision

A development-only binary, `cmd/devserver`, that sits between development instances and GitHub. It is not shipped, not imported by the app, and holds no credentials of its own.

1. **Caching reverse proxy.** Responses are cached in SQLite (`modernc.org/sqlite`, plain `database/sql` — `sqlc.yaml` stays scoped to `internal/app/store`) at `$XDG_CACHE_HOME/hive/devserver/cache.db`, shared by every instance pointed at it. The location is the cache dir and is deliberately independent of the desktop's data root: ADR 0014 gives each worktree an isolated instance that `desktop:dev:fresh` and `desktop:dev:reset` exist to delete, so deriving from it would unshare the cache per worktree and discard it on reset — the two properties the cache exists to provide. `golang.org/x/sync/singleflight` collapses the herd that several instances ticking on the same 60s boundary produce. Only 2xx responses are cached: caching a 401 would outlive the bad token, and caching a rate-limit 403 would extend the outage past its own reset.

2. **The cache key includes a hash of the bearer token.** GitHub responses are account-scoped, and two accounts can point at one devserver. Keying only on the request would serve one account's private data to the other. The token is hashed, never stored. The key also includes the request body, because the desktop batches every search into `POST /graphql` where the URL is identical for all of them.

3. **devserver stores ETags and revalidates with `If-None-Match`.** A 304 does not draw down the primary quota, so an expired entry is usually far cheaper than a fresh fetch. This is headroom the client does not have today, and the proxy provides it without an app change — and keeps providing it across processes if the client later gets its own cache.

4. **Config-driven overlays rewrite responses on the way out.** One `Mutations` value is rendered into all three shapes the desktop reads the same logical item through — GraphQL search nodes, REST `/notifications` entries, and single-item REST responses. Rewriting on egress rather than ingress means an overlay can be changed without purging the cache.

   Overlay consistency across shapes is the load-bearing property. GitHub reports a merged PR as `state=closed, merged=true` on the pulls endpoint and drops it from an `is:open` search; simulating a merge therefore has to do **both**, or it produces a state the real API can never return and the resulting bug looks like an app bug. The `absent` field exists for exactly this, and is the only way to exercise the desktop's absence-confirmation path.

5. **The action vocabulary is GitHub's, not an invented one.** The GitHub connector's classifier reads exactly four things — `state`, `reason`, `updatedAt`, and `labels` (`githubPayload` in `internal/app/sources/github/classify.go`) — so those are the only levers that exist. Notably there is **no "approve" action**: GitHub has no such notification reason and the desktop never fetches review state. An approval reaches the app as activity on the item, so it is simulated as the reason it actually produces. Inventing a richer vocabulary would teach a wrong model of the app.

6. **A webhook pusher** delivers configured JSON payloads to a running instance's local webhook listener, with the `X-Hive-Secret` header ADR 0007 specified. This is the second, independent lever: the proxy makes GitHub say something different, the pusher injects an event that never came from GitHub at all.

7. **A dashboard** at the listen root drives both, over a JSON control API under `/_ctl/`. Assets are embedded and self-contained; the page is a plain 2s poll of one `/_ctl/state` read.

8. **A checked-in development config**, `cmd/devserver/devserver.yaml`, passed as `--config` by `mise run devserver` so a fresh clone works with no setup. It ships scenarios and webhook payloads but **declares no overlays**: a config that rewrote responses from the first request would mean a developer who never opened the dashboard could still be looking at fabricated data, and every subsequent bug would be suspect. Personal configuration goes at `$XDG_CONFIG_HOME/hive/desktop/devserver.yaml`, which is the default when `--config` is absent.

9. **The app opts in through `development.github.api_base`** (`HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE`), applied via the client's `WithAPIBase` option in `ghsource.NewProductionClient`. One client template backs both the fetch layer and the connect flow, so a redirected instance cannot split its traffic between the proxy and real GitHub.

   This is a development setting rather than a bare environment variable, which reverses the position an earlier draft of this ADR took. That draft was written before ADR 0014: there was no `development` namespace, so "not a setting" was the only way to say "not a user-facing knob". ADR 0014 created a better place to say it — `development.*` is where dev-only configuration lives, alongside `mocks.mode`, which is at least as consequential — and being a typed setting is what buys the guarantee below.

10. **Loopback only, enforced by validation.** `Settings.Validate()` rejects any `api_base` that is not an `http`/`https` URL on a loopback host, following the fail-closed posture ADR 0014 set for every dev-only listener. Because validation runs on the persisted value *and* on the effective value after environment overrides, this holds no matter where the value came from.

    This is a stronger guarantee than the env-only design it replaces, which read whatever string it was handed. devserver fronts a GitHub token and can change what an instance sees; neither belongs on a LAN, and now nothing can put them there.

11. **The OAuth base is never redirected.** Only the API base moves. A device-flow token exchange has no business passing through dev tooling, and it draws no rate-limit budget, so redirecting it would be all risk and no benefit. Sign-in reaches github.com even on a fully proxied instance.

## Consequences

- N instances plus their restarts cost one upstream request per unique call per TTL, which was the original problem. Restarting devserver itself does not refetch, since the cache is on disk.
- Overlay state is **in-memory only**: config seeds it, the control API mutates it, and a restart returns to exactly what `devserver.yaml` declares. A debugging session cannot leave permanent fake data behind, at the cost of not being able to save a mutation without editing config.
- An instance pointed at devserver is seeing possibly-stale, possibly-rewritten data by construction. Startup logs a warning naming the base and whether it came from the environment or from `settings.yaml`, and every proxied response carries `X-Devserver-Outcome`.
- The override is reachable through `settings.yaml`, which the env-only design made impossible. Loopback validation bounds the consequence to "this machine", and a stray value is visible in a file and in the startup log rather than in invisible process state. Accepted deliberately; it is the cost of the namespace being typed and validated.
- devserver knows five route kinds (`POST /graphql`, `/notifications`, `/user`, and single-item issue/pull GETs). Anything else passes through untouched, so a new client call keeps working — uncached and un-rewritten — without devserver needing to know about it.
- The client defects in #62 remain. devserver works around them for development and does not fix them for users; an in-client persisted cache is still worth having, and ADR 0015 made it a local change. The two do not conflict — devserver's value is cross-process sharing, which stays useful afterward.
- The dashboard's HTML and JS are not covered by automated tests. The Go control API it consumes is; the page is a thin renderer over it.
