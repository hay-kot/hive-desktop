# 0042 — Loopback listener lifecycle

- **Status:** accepted
- **Date:** 2026-07-30

## Context

The shared loopback listener serves webhooks and the agent API. Its settings
must apply while that API is handling a settings-reload request, and terminal
WebSockets are hijacked connections that `http.Server.Shutdown` cannot drain.

## Decision

`webhook.Listener` is restartable: state-based `Stop` idempotence replaces its
lifetime `stopOnce`; mounts are keyed by prefix and rebuilt into a fresh mux on
each start; and `SetAddr` changes the next bind only.

`App` owns restart policy. Listener-affecting applies schedule a
self-converging reconcile under one App-level mutex. Each reconcile re-reads
the settings store before comparing desired and live state. It cannot run
inline: the reload endpoint is served by the listener being restarted, and an
inline shutdown would wait on its own handler and lose the response. The mutex
also prevents watcher reloads, HTTP reloads, and `WebhookService.SetState`
from interleaving stop, rebind, and start.

A restart first detaches every tmux control-mode client, then gives the HTTP
server three seconds to drain using a context derived with `WithoutCancel`,
and force-closes it when that budget expires. Accepted requests drain during
the grace period; new connections during the rebind gap are refused. Terminal
attachments close while tmux sessions remain available.

Non-mock apps always construct the listener; `http.enabled` controls whether
it starts. `WebhookService.SetState` persists first and then schedules the
same apply half, so UI and external edits converge through one path. After
convergence, the reconcile publishes `SettingsUpdated` for the `http.*` fields
so consumers re-read the listener's live state.

## Consequences

The loopback host, port, and enablement fields are live settings. Bind failures
leave the app running and are surfaced from the listener's live state. Mounts
added while serving take effect after the next restart.
