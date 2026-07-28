# Grafana alerts source

A **Grafana alerts source** node emits one item per currently firing Grafana-managed alert on a connected stack. It has no inputs — this is where a flow starts.

## Fields

- `credential` — required. The connected Grafana stack to fetch as, written as `grafana/<account>`. Connect a stack under Settings ▸ Integrations by pasting its URL and a service-account token. Alerts are not scoped to a datasource, so there is nothing else to configure.

## Behavior

The source runs in the backend: Go polls the stack's Alertmanager on each tick and appends the result to the event log under topic `source:<flowId>/<nodeId>`. Each firing alert becomes **one message keyed by its fingerprint**, so an alert maps to its own durable item. The payload carries the alert's `title` (its summary annotation, or its `alertname`), a `state` of `firing`, and the raw `labels` and `annotations` for a `function` node to route on.

An alert that is still firing re-reports the same fingerprint and payload and is deduplicated — it produces no new event. When an alert stops firing it leaves the Alertmanager's complete active set, so the source treats its absence as authoritative: the item is marked `resolved` and archived on the next poll, rather than left to age out.
