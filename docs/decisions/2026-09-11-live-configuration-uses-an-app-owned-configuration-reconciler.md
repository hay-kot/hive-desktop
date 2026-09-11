# Live configuration uses an App-owned Configuration Reconciler

- **Status:** proposed
- **Date:** 2026-09-11

## Context

Actions, flows, and agent workspaces each currently own a filesystem watcher.
Settings reads disk and environment for each getter. Their reload callbacks
have no common ordering, revision vocabulary, or result boundary. A file save
can therefore reach dependent domains in different states, and a status cannot
say whether it describes detected bytes, accepted configuration, or a running
resource.

Settings, actions, flows, and workspaces have different acceptance boundaries.
Actions accept a whole catalog. Flows accept independent definitions and then
install them asynchronously. Workspaces accept manifests and libraries without
claiming generation or scheduler installation. Settings accept desired values,
apply core-live effects, and retain startup resources for restart fields.

## Decision

**App owns one Configuration Reconciler.** It serializes app-authored writes
and reconciliation passes, evaluates sources in settings, actions, flows, and
workspaces order, calls domains directly, and publishes typed Observer events
after a pass. It is not a command bus or a generic configuration store.

A later `configwatch.Manager` owns only filesystem observation and dirty
generations. Domains continue to own parsing, validation, writes, snapshots,
and last-good policy. `configstate` supplies shared revisions, diagnostics, and
status vocabulary. Domain status and detection status remain separate until the
reconciler joins them for callers.

A loaded revision records accepted domain content. An active revision records
only the declared acceptance scope: catalog, flow runtime, or core settings.
Workspaces report no runtime-active revision. Settings do not claim Wails or
updater acknowledgement, and restart fields retain startup-active resources.

## Consequences

- Existing watchers and current flow parse-failure behavior remain until their
  replacement phases.
- App writes and external edits converge through the same ordered path.
- Each domain keeps its own acceptance and last-good rules rather than sharing
  a generic live-configuration implementation.
- Status can distinguish detection, candidate validity, accepted content, and
  runtime application without exposing configuration content or raw errors.
