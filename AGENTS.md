# Agent Instructions

Private product monorepo: Hive desktop app, future admin server, and landing page.

## Module layout

- **Root module** `github.com/hay-kot/hive-desktop` — the desktop app (`desktop/`, `internal/desktop/`) and vendored hive core (`internal/hivecore/`).
- **`server/`** — future Go admin backend (analytics, licenses, purchases). When built, it gets its own nested `go.mod` (`github.com/hay-kot/hive-desktop/server`) so the deployed service does not carry wails/charm dependencies; a root `go.work` is added at that point. Shared wire types (analytics events, license payloads) go in a `shared/` nested module if needed.
- **`web/`** — plain static HTML landing page (`web/public/`), served as Cloudflare Workers static assets (`web/wrangler.jsonc`; the custom domain `hivedesktop.com` is declared there and attaches on deploy). No build step. Deploys via `.github/workflows/deploy-web.yml` on pushes to main touching `web/**`, or locally with `npm run deploy`.

## Vendored code — `internal/hivecore/`

- Vendored from `colonyops/hive` `internal/` packages at the SHA pinned in `scripts/vendorhive/vendor.lock`; import paths are rewritten by the sync tool.
- **Never edit vendored files.** Changes land in `colonyops/hive` first, then re-run the vendor sync. CI enforces this with a drift check.
- `pkg/` packages from hive are a normal `go.mod` dependency, pinned to the same SHA by the sync tool.

## Distribution

Release CI signs and notarizes the macOS app, then uploads versioned artifacts plus a `latest.json` manifest (version, URL, sha256) to Cloudflare R2 behind a stable download domain. The in-app updater polls the manifest; the landing page download link resolves through it. Tags use the `desktop-v*` namespace as the version anchor.

## Documentation

- Record notable architecture/infrastructure decisions as ADRs in `docs/decisions/` (next number, Status/Date/Context/Decision/Consequences) and add them to the index in `docs/README.md`. Mark superseded ADRs instead of deleting them.
- Concrete distribution facts (bucket, domains, manifest schema, publish/rollback runbook, credentials) live in `docs/distribution.md` — keep it current when infra changes.

## Quality gates

Every gate is a mise task (`mise tasks`); lefthook runs the relevant ones as git hooks so they fire without anyone remembering to. `mise install` wires them up (mise `postinstall` → `scripts/hooks/install.sh`); re-run `mise run setup` after editing `lefthook.yml`. CI remains the source of truth — hooks are a fast local mirror.

- **pre-commit** (~0.1s): formats staged Go files (`golangci-lint fmt`) and re-stages them; blocks edits to vendored `internal/hivecore/`; when a generator input is staged, regenerates and blocks if the committed output differs. A partially staged Go file gets its unstaged hunks staged too — stage whole files.
- **pre-push** (~5s): `check:tidy`, `lint`, `test`, `check:generate`, plus the frontend unit tests when the push touches `desktop/frontend/`.

Wails TS bindings and the e2e suite are deliberately not hooked — both need a full app build. Run `mise run desktop:generate` / `mise run desktop:e2e` when the change warrants it; CI covers them either way.

**Never bypass a hook** — no `LEFTHOOK=0`, `git commit -n`, or `git push --no-verify`. The escape hatch exists for human emergencies; a failing gate is a task to finish, not a flag to add.

## Git standards

- Never push to main; branch and PR (`feat/`, `chore/`, `fix/` prefixes).
- All commits signed.
