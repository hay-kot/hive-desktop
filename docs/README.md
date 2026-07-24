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

## References

- [`distribution.md`](distribution.md) — concrete distribution infra: bucket, domains, bucket layout, manifest schema, publish/rollback runbook, credentials.

## Related documents outside this repo

The desktop app is being extracted from `colonyops/hive`; the phase-by-phase extraction plan (vendor tool spec, hive-side removal, sequencing) lives in the hive context directory: `plans/2026-07-23-hive-desktop-repo-extraction.md`.
