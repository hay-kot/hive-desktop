# Docs

Documentation for the hive-desktop monorepo.

## Decisions

Notable architecture/infrastructure decisions are recorded as ADRs in
[`decisions/`](decisions/), one file per decision — the directory listing is
the index.

An ADR is identified by its filename, `YYYY-MM-DD-slug.md`. Nothing allocates a
number, so two branches can add one without colliding, and prose cites the slug
alone: `(ADR terminal-transport)`. Start one with `mise run adr:new -- "Title"`;
`mise run check:adr` verifies the ids and the citations. Superseded
ADRs are marked, not deleted.

## References

- Tester-facing getting-started docs live on the website — source in [`web/src/content/docs/`](../web/src/content/docs/), published at [hivedesktop.com/docs](https://hivedesktop.com/docs). [`distribution.md`](distribution.md) is the maintainer counterpart.
- [`architecture.md`](architecture.md) — how the app is structured and how it should grow: the core/adapter shape, named patterns, directory layout, extension points, cross-cutting conventions, and the rules PRs are reviewed against. Read this before adding a subsystem, entrypoint, or extension point.
- [`source-pipeline.md`](source-pipeline.md) — the pipeline's runtime behaviour: ingestion, the `Msg` contract, flows, membership replay, retention, and actions.
- [`distribution.md`](distribution.md) — concrete distribution infra: bucket, domains, bucket layout, manifest schema, publish/rollback runbook, credentials.

## Related documents outside this repo

The desktop app is being extracted from `colonyops/hive`; the phase-by-phase extraction plan (vendor tool spec, hive-side removal, sequencing) lives in the hive context directory: `plans/2026-07-23-hive-desktop-repo-extraction.md`.
