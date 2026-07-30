# Docs

Documentation for the hive-desktop monorepo.

## Decisions

Notable architecture/infrastructure decisions are recorded as ADRs in [`decisions/`](decisions/). New decisions get the next number; superseded ADRs are marked, not deleted.

| # | Decision |
| - | -------- |
| [0001](decisions/0001-monorepo-structure.md) | Product monorepo structure and clean import from hive |
| [0002](decisions/0002-vendor-hive-internals.md) | Vendor hive `internal/` packages via sync tool |
| [0003](decisions/0003-r2-manifest-distribution.md) | Distribution and auto-update via R2 + channel manifests |
| [0004](decisions/0004-release-channels.md) | Release channels: stable, beta, dev |
| [0005](decisions/0005-web-workers-static-assets.md) | Landing page as Cloudflare Workers static assets |
| [0006](decisions/0006-lefthook-quality-gates.md) | Quality gates enforced by lefthook git hooks |
| [0007](decisions/0007-local-webhook-listener.md) | Local webhook listener for generic pipeline ingress |
| [0008](decisions/0008-canonical-item-contract.md) | Canonical inbox item contract |
| [0009](decisions/0009-go-owned-llm-prompts.md) | LLM prompts owned by Go, node docs live with the schema |
| [0010](decisions/0010-goja-script-runtime.md) | goja for function nodes, behind a ScriptRuntime port |
| [0011](decisions/0011-flow-engine-in-go.md) | The flow engine moves to Go |
| [0012](decisions/0012-source-connector-registry.md) | Source connectors are declared, not sniffed |
| [0013](decisions/0013-credential-store.md) | Credentials are keyed by account, in a store of our own |
| [0014](decisions/0014-desktop-configuration.md) | Typed desktop configuration and worktree-local development instances |
| [0015](decisions/0015-owned-github-client.md) | The desktop owns its GitHub client |
| [0016](decisions/0016-webhook-listener-placement.md) | The webhook listener stays in the core as push ingress |
| [0017](decisions/0017-devserver-github-proxy.md) | Development GitHub proxy and event simulator |
| [0018](decisions/0018-source-http-toolkit.md) | A shared HTTP toolkit for source connectors |
| [0019](decisions/0019-batched-absence-confirmation.md) | Batched, keyed GitHub absence confirmation |
| [0020](decisions/0020-devserver-agent-control-api.md) | devserver agent-facing control API (runtime scenarios, inline targets, discovery) |
| [0021](decisions/0021-agent-http-api.md) | Agent-facing HTTP API (control surface: read, reload, mutate) sharing the webhook port |
| [0022](decisions/0022-http-handler-conventions.md) | HTTP handler conventions: errchain, extractors, criterio validation |
| [0023](decisions/0023-pprof-debug-endpoint.md) | pprof debug endpoint mounted on the shared loopback HTTP server |
| [0024](decisions/0024-in-app-problem-reporting.md) | In-app problem reporting: redacted diagnostics to a private R2 bucket |
| [0025](decisions/0025-profile-images.md) | Profile images: normalized PNG in the data dir, hash-referenced from the flow |
| [0026](decisions/0026-install-script.md) | One-line install script served behind an obscure path |
| [0027](decisions/0027-self-describing-agent-api.md) | Self-describing agent API: one operations table backs the mux, a GET /api index, and a generated OpenAPI document |
| [0028](decisions/0028-linux-tarball-distribution.md) | Linux ships as a tarball, not a package |
| [0029](decisions/0029-clipboard-action-type.md) | Clipboard action type with a render-only, non-durable invocation path |
| [0030](decisions/0030-commit-resilience-and-scope-backfill.md) | Commit resilience, pre-#63 scope backfill by self-healing lookup, and superseded-snapshot retention |
| [0031](decisions/0031-webhook-source-image-marks.md) | Webhook source image marks: content-addressed PNG in the data dir, hash in the flow |
| [0032](decisions/0032-yaml-config-migration.md) | Forward-only in-place YAML config migration |
| [0033](decisions/0033-skill-installer.md) | Skill installer: install the paste-ready prompts as agent skills, kept in sync by content hash |
| [0034](decisions/0034-github-tags-and-releases.md) | Publish GitHub tags and Releases as the source-side record of a desktop release |

## References

- Tester-facing getting-started docs live on the website — source in [`web/src/content/docs/`](../web/src/content/docs/), published at [hivedesktop.com/docs](https://hivedesktop.com/docs). [`distribution.md`](distribution.md) is the maintainer counterpart.
- [`architecture.md`](architecture.md) — how the app is structured and how it should grow: the core/adapter shape, named patterns, directory layout, extension points, cross-cutting conventions, and the rules PRs are reviewed against. Read this before adding a subsystem, entrypoint, or extension point.
- [`source-pipeline.md`](source-pipeline.md) — the pipeline's runtime behaviour: ingestion, the `Msg` contract, flows, membership replay, retention, and actions.
- [`distribution.md`](distribution.md) — concrete distribution infra: bucket, domains, bucket layout, manifest schema, publish/rollback runbook, credentials.

## Related documents outside this repo

The desktop app is being extracted from `colonyops/hive`; the phase-by-phase extraction plan (vendor tool spec, hive-side removal, sequencing) lives in the hive context directory: `plans/2026-07-23-hive-desktop-repo-extraction.md`.
