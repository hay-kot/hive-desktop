# Agent Instructions

Private product monorepo: Hive desktop app, future admin server, and landing page.

## Module layout

- **Root module** `github.com/hay-kot/hive-desktop` — the desktop app (`desktop/`, `internal/desktop/`) and vendored hive core (`internal/hivecore/`).
- **`server/`** — future Go admin backend (analytics, licenses, purchases). When built, it gets its own nested `go.mod` (`github.com/hay-kot/hive-desktop/server`) so the deployed service does not carry wails/charm dependencies; a root `go.work` is added at that point. Shared wire types (analytics events, license payloads) go in a `shared/` nested module if needed.
- **`web/`** — plain static HTML landing page, deployed to Cloudflare Pages. No build step.

## Vendored code — `internal/hivecore/`

- Vendored from `colonyops/hive` `internal/` packages at the SHA pinned in `scripts/vendorhive/vendor.lock`; import paths are rewritten by the sync tool.
- **Never edit vendored files.** Changes land in `colonyops/hive` first, then re-run the vendor sync. CI enforces this with a drift check.
- `pkg/` packages from hive are a normal `go.mod` dependency, pinned to the same SHA by the sync tool.

## Distribution

Release CI signs and notarizes the macOS app, then uploads versioned artifacts plus a `latest.json` manifest (version, URL, sha256) to Cloudflare R2 behind a stable download domain. The in-app updater polls the manifest; the landing page download link resolves through it. Tags use the `desktop-v*` namespace as the version anchor.

## Git standards

- Never push to main; branch and PR (`feat/`, `chore/`, `fix/` prefixes).
- All commits signed.
