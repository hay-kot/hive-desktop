# 0007 — Local webhook listener for generic pipeline ingress

- **Status:** accepted
- **Date:** 2026-07-23

## Context

The pipeline's only source is `github-source`, polled by the Go producer. A generic ingress was needed so systems without a first-party source (CI, monitoring, home-grown tooling, Zapier/n8n bridges) can push arbitrary JSON into flows. The desktop app had no general-purpose HTTP listener — the only HTTP surface is Wails' own asset server plus marker-gated `/_e2e/*` test middleware — and webhooks are push-shaped, which fits the poll producer badly (a buffer drained per tick would add up to a full poll interval of latency and distort the "authoritative snapshot per tick" contract).

## Decision

A dedicated `net/http` server (`pipeline.WebhookListener`) bound to **127.0.0.1 only**, serving `POST /hooks/<path>` for every enabled `webhook-source` flow node:

1. **Routes resolve per request from the current flow set** (the same late-binding posture as the producer's per-tick `SourceLister`), so node edits need no restart and the listener holds no route state. Several nodes may declare the same path; each receives the delivery.
2. **Deliveries bypass the producer** and call `IngestObservation` directly — the same production source boundary the smoke fixture uses — under topic `source:<flowId>/<nodeId>`, `source_kind: "webhook"`, `source_scope: <nodeId>`. After a write the listener appends the topic's complete unarchived item set as the authoritative snapshot, so startup/deploy membership replay treats webhook sources exactly like polled ones.
3. **Identity is payload-derived:** a top-level `id` (string/number) is the stable key; otherwise the body's SHA-256, so exact duplicate deliveries dedupe. `title`/`url` are promoted for feed rendering; the payload is otherwise opaque per the `Msg` contract — reshaping stays downstream `function`-node work.
4. **Port** comes from `settings.yaml`'s `webhook_port` (default 4483), overridable by `HIVE_DESKTOP_WEBHOOK_PORT`. In mock modes the listener starts only with an explicit env port so parallel e2e servers never collide. Non-localhost exposure is out of scope; users who need remote senders can front it themselves (tailscale/ssh/reverse proxy).
5. **Auth is a per-node shared secret** (`X-Hive-Secret`, constant-time compare) — optional, since the bind is loopback-only.
6. The last request body per topic is kept in a `webhook_capture` table (one row per topic) to power the node editor's payload preview, its non-blocking feed-shape hint, and the copyable LLM transform prompt.

## Consequences

- The desktop now opens a localhost TCP port in live mode. A bind failure logs and the app runs on; the port is never fatal.
- Webhook items ingest with zero poll latency, and one delivery costs one snapshot append over the topic's unarchived items — acceptable at webhook volumes, revisit if a topic accumulates very large active sets.
- Feed rendering of an item comes from what was ingested (title/url promotion plus payload fields), not from downstream `function` transforms — transforms affect routing, filtering, and action payloads. Senders that want rich rows send feed-item-shaped JSON.
- A future e2e lane can exercise the listener by claiming a port through `HIVE_DESKTOP_WEBHOOK_PORT`.
