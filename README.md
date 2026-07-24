# Hive Desktop

Private product monorepo for the Hive desktop application and its supporting services.

| Component | Path | Status |
| --------- | ---- | ------ |
| Desktop app (Wails v3, Vue 3) | `desktop/` + `internal/desktop/` | Imported from `colonyops/hive` — see `desktop/README.md` |
| Vendored hive core | `internal/hivecore/` | Script-managed by `scripts/vendorhive` — **read-only** |
| Admin server (analytics, licenses, purchases) | `server/` | Future — nested Go module when built |
| Landing page | `web/` | Static HTML on Cloudflare Workers static assets → [hivedesktop.com](https://hivedesktop.com) |

## Layout & conventions

- Root Go module `github.com/hay-kot/hive-desktop` owns the desktop app and vendored core. `server/` becomes its own nested module (plus a root `go.work`) when it exists — see `AGENTS.md`.
- Code under `internal/hivecore/` is vendored from `colonyops/hive` at the SHA pinned in `scripts/vendorhive/vendor.lock`. Never edit it here: change hive first, then re-vendor.
- Releases are signed/notarized in CI and uploaded to Cloudflare R2 behind a stable domain — versioned zips plus a `latest.json` manifest that drives the in-app updater and the landing-page download link. GitHub releases are not user-facing.

## Setup

```sh
mise trust                         # once per clone, before mise reads mise.toml
mise install                       # toolchain + git hooks (lefthook)
cd desktop/frontend && npm ci      # frontend deps, for the desktop app and its tests
```

`mise install` also installs the git hooks, so a fresh clone gets the quality gates with no extra step (`mise run setup` re-installs them on demand). `mise tasks` lists every gate and build task. Hooks format staged Go files on commit and run tidy/lint/test on push — see [`docs/decisions/0006-lefthook-quality-gates.md`](docs/decisions/0006-lefthook-quality-gates.md).

Installing the hooks sets this clone's `core.hooksPath` to its own `.git/hooks`, which takes precedence over a global `core.hooksPath` — global hooks will not run in this repo.

## Docs

Architecture and infrastructure decisions are recorded as ADRs in [`docs/decisions/`](docs/decisions/); the concrete distribution setup (bucket, domains, layout, runbook) is in [`docs/distribution.md`](docs/distribution.md). Index: [`docs/README.md`](docs/README.md).

## Extraction status

The desktop app is imported (see the import commit for the source SHA), and the release pipeline is ported: R2 upload + channel manifests (ADR 0003/0004) via `scripts/release/release-desktop.sh`, the tag-triggered publish workflow, and the manifest-polling in-app updater. The desktop still needs removal from `colonyops/hive`. The full plan lives in the hive context directory: `plans/2026-07-23-hive-desktop-repo-extraction.md`.
