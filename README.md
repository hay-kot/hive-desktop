# Hive Desktop

Private product monorepo for the Hive desktop application and its supporting services.

| Component | Path | Status |
| --------- | ---- | ------ |
| Desktop app (Wails v3, Vue 3) | `desktop/` + `internal/desktop/` | Arrives via import from `colonyops/hive` (extraction Phase 2) |
| Vendored hive core | `internal/hivecore/` | Script-managed by `scripts/vendorhive` — **read-only** |
| Admin server (analytics, licenses, purchases) | `server/` | Future — nested Go module when built |
| Landing page | `web/` | Static HTML, deployed to Cloudflare Pages |

## Layout & conventions

- Root Go module `github.com/hay-kot/hive-desktop` owns the desktop app and vendored core. `server/` becomes its own nested module (plus a root `go.work`) when it exists — see `AGENTS.md`.
- Code under `internal/hivecore/` is vendored from `colonyops/hive` at the SHA pinned in `scripts/vendorhive/vendor.lock`. Never edit it here: change hive first, then re-vendor.
- Releases are signed/notarized in CI and uploaded to Cloudflare R2 behind a stable domain — versioned zips plus a `latest.json` manifest that drives the in-app updater and the landing-page download link. GitHub releases are not user-facing.

## Extraction status

The desktop app still lives in `colonyops/hive`; this repo is being stood up ahead of the extraction. The full plan (phases, vendor tool spec, release pipeline, risks) lives in the hive context directory: `plans/2026-07-23-hive-desktop-repo-extraction.md`.
