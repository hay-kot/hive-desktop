---
icon: lucide/plug
description: Supported source providers, local webhooks, and command-based inputs.
---

# Sources

Add sources in the flow editor. Connect provider accounts under **Settings ▸ Integrations**.

| Source | Support | Available inputs |
| --- | --- | --- |
| GitHub | Stable | Search and notifications |
| Gitea and Forgejo | Beta | Filtered search and notifications |
| Grafana | Stable | Managed alerts, IRM alerts, and Prometheus metrics |
| PostHog | Experimental | Error tracking and insight alerts |
| Webhooks | Stable | JSON sent to a local endpoint |
| Commands and CLIs | Stable | JSON returned by a shell command |

Provider tokens are stored in the OS keychain. Flow files refer to a connected account by name.

## GitHub

GitHub sources support:

- any GitHub search query;
- the connected account's notification inbox;
- multiple accounts with separate polling and rate limits.

Connect with GitHub's device flow or a classic personal access token. Hive requests `repo` for private repository results and `notifications` for the notification inbox.

Use `sources.github` in a flow. Choose `search` and provide a query, or choose `notifications`.

Hive does not update read, archive, or notification state on GitHub.

## Gitea and Forgejo

Gitea sources work with Gitea and Forgejo instances. They support:

- issue and pull request searches;
- filters for state, owner, labels, involvement, and text;
- the connected account's notifications.

Connect an instance URL and access token under **Settings ▸ Integrations**. The token needs `read:user`, `read:issue`, and `read:notification`. Use `sources.gitea` in a flow.

This source is beta.

## Grafana

Connect a Grafana stack with its URL and a Viewer service account token. IRM also needs `grafana-irm-app.alert-groups:read`. The same account can be used by three source types:

- `sources.grafana_alerts` for firing Grafana-managed alerts;
- `sources.grafana_irm_alerts` for active IRM alert groups;
- `sources.grafana_metrics` for a PromQL query result.

Alert sources can be filtered by labels, integration, or team. Metrics sources require a Prometheus-compatible data source UID and an expression.

## PostHog

Connect a PostHog instance and personal API key, then select a project. Error tracking needs `project:read` and `error_tracking:read`; insight alerts need `project:read` and `alert:read`. PostHog provides:

- `sources.posthog_errors` for error-tracking issues;
- `sources.posthog_alerts` for insight alerts.

Both PostHog source types are experimental.

## Webhooks

A webhook source accepts JSON from scripts and local tools. It listens on:

```
http://127.0.0.1:<port>/hooks/<path>
```

Create a `sources.webhook` node and give it a path. The local HTTP server must be enabled. **Settings ▸ Integrations ▸ Webhooks** shows the current base URL and lets you choose a fixed port.

```sh
curl -X POST "http://127.0.0.1:$PORT/hooks/ci-alerts" \
  -H "Content-Type: application/json" \
  -H "X-Hive-Secret: $SECRET" \
  -d '{
    "id": "build-4821",
    "kind": "build",
    "title": "Unit tests failed",
    "url": "https://ci.example.com/builds/4821",
    "state": "failing"
  }'
```

Webhook rules:

- The listener only accepts local connections.
- Requests use `POST` with a JSON body of up to 1 MiB.
- An optional node secret is sent in `X-Hive-Secret`. Hive stores this secret in the flow file, so handle that file as a shared secret.
- A stable top-level `id` updates the same item on later deliveries.
- `title`, `url`, and `kind` control the basic feed display.
- `resolved`, `closed`, and `done` states archive the item.

Other fields remain available to function nodes and action templates.

!!! tip "Ask the Hive workspace"
    The **Hive** workspace can create the webhook node and sender with the `hive-webhook-sources` skill.

## Commands and CLIs

A command source runs on the polling schedule and reads a JSON snapshot from stdout.

```yaml
- id: todo
  type: sources.exec
  command: "todo list --json | jq -s ."
  timeout: 30s
  interval: 1h
```

The command must define a timeout of no more than two minutes, exit successfully, and print a JSON array. Each object needs a stable top-level `id`. Items missing from the next successful snapshot are archived.

A failed command leaves the previous snapshot unchanged and records the error in Activity.
