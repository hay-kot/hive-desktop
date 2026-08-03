# PostHog insight alerts source

A **PostHog insight alerts source** node emits one item per insight alert configured in a connected project. It has no inputs — this is where a flow starts.

## Fields

- `credential` — required. The connected PostHog project to fetch as, written as `posthog/<account>`. Connect a project under Settings ▸ Integrations; the key needs the `project:read` and `alert:read` scopes.
- `firing_only` — emit only alerts that are currently firing. Off by default, because emitting every alert is what lets one that stops firing update the item that was already there rather than silently disappear from the feed.

## Behavior

The source runs in the backend: Go lists the project's alerts on each tick and appends the result to the event log under topic `source:<flowId>/<nodeId>`. Each alert becomes **one message keyed by its alert id**, so a firing→resolved cycle updates one durable item.

The payload carries the alert's `title` (its name, falling back to the insight's), a `url` that deep-links to the monitored insight, a normalized `state` — `firing`, `not_firing`, `snoozed` or `errored` — and the raw `threshold` and `condition` for a `function` node to route on. PostHog reports states as display strings (`Not firing`); the source normalizes them, so route on `not_firing` rather than on what the API returns.

An alert that starts breaching is summarized as **Firing**; one that stops is summarized as **Resolved** and archived. Only `firing` counts as active — a snoozed or errored alert is not a breach asking to be looked at.

An alert that stops appearing is **not** treated as resolved: the list carries every alert with its current state, so an alert missing from it was deleted in PostHog. A resolution always arrives as a state change on an alert that is still listed.

One page of alerts is polled per tick. A project with more alerts than that logs a warning rather than quietly serving a partial set.
