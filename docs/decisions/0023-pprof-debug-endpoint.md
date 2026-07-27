# 0023 — pprof debug endpoint on its own loopback listener

- **Status:** accepted
- **Date:** 2026-07-27

## Context

`development.pprof` has been typed, validated (loopback host, port-zero-safe),
and defaulted off since ADR 0014, but nothing started the endpoint. The
architecture's Background lifecycle section deferred it twice over: it must not
add a teardown branch in `main`, and the intended `appkit/plugs` manager was
declined (ADR 0016's note), leaving the App-owned lifecycle — an idempotent,
context-taking `Stop` behind a `stopOnce`, started in `App.Start` and unwound
in `App.Close` — as the standing pattern the webhook listener already follows.

The open question was where the handlers bind: a standalone listener, or the
`net/http/pprof` mux mounted under the always-on loopback HTTP server (ADR
0021) that already hosts the webhook listener and agent API.

## Decision

**A standalone `internal/app/pprofsrv.Server` on its own loopback listener,
owned by the App-owned lifecycle.** The config's dedicated `host`/`port`
decide it: mounting under the shared server would make those two fields
meaningless, since that server has its own binding. Three things follow:

- **`development.pprof` is its whole gate.** When disabled — the default —
  `App.openPprof` leaves the server nil and `Start` runs nothing, so there is
  no route, not even a disabled one, on any server.
- **Off-by-default pprof does not ride an on-by-default server.** The HTTP
  server (ADR 0021) is on by default; pprof is off. Keeping profiling — CPU
  and trace endpoints block for a caller-chosen duration — off the always-on
  surface means enabling it is one explicit act with its own address.
- **The lifecycle is the webhook listener's, not a bespoke one.** `Start`
  binds and serves in a goroutine and returns its bind error; a busy port logs
  and the app carries on. `Stop` is idempotent and context-taking. `main`
  holds none of it.

The handlers are registered on a private `http.ServeMux`, not the
`http.DefaultServeMux` that importing `net/http/pprof` writes to, so the
default mux is never served.

## Consequences

- Enabling pprof is `development.pprof.enabled: true`; the bound host and port
  are logged at startup (port 0 asks the OS to allocate). There is no UI and no
  persisted allocated port — it is a developer tool read from the log.
- The webhook listener (ADR 0016) remains the lifecycle template; a second
  subsystem now follows it, which is the evidence the App-owned pattern
  generalises before plugs is revisited.
- If a product need later wants pprof on the shared server (one port to
  discover), that is a reversal of the host/port independence this ADR chose,
  and belongs in a superseding decision, not a quiet mount.
