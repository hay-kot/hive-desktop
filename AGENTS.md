# Agent Instructions

Private product monorepo: Hive desktop app, future admin server, and landing page.

## Before building a feature

**Read [`docs/architecture.md`](docs/architecture.md) first.** It is the
standing reference for how this app is structured and how it should grow —
the core/adapter shape, the named patterns each part of the app follows, the
directory layout, the four extension points, and the rules every PR is
reviewed against.

Two tables in it answer most "how do I build this?" questions directly:
**Named patterns** (what each pattern is called and where it applies) and
**Which pattern governs what** (what you are building → the section that
specifies it). Use the pattern names in code review and commit messages —
naming them is what keeps independently-built features consistent.

The document describes a **target state**; parts of it are not built yet and
are marked as such. Where the current code and the document disagree, the
document wins for new work — do not extend the shape it is replacing.

## Module layout

- **Root module** `github.com/hay-kot/hive-desktop` — the desktop app (`desktop/`, `internal/app/`, `internal/adapter/`) and vendored hive core (`internal/hivecore/`).
- **`server/`** — future Go admin backend (analytics, licenses, purchases). When built, it gets its own nested `go.mod` (`github.com/hay-kot/hive-desktop/server`) so the deployed service does not carry wails/charm dependencies; a root `go.work` is added at that point. Shared wire types (analytics events, license payloads) go in a `shared/` nested module if needed.
- **`web/`** — the landing page, an Astro static build (`web/src/`) output to `web/dist/` and served as Cloudflare Workers static assets (`web/wrangler.jsonc`; the custom domain `hivedesktop.com` is declared there and attaches on deploy). Page copy is data, not markup: the JSON in `web/src/data/` is schema-validated in `web/src/data/index.ts`, so a bad edit fails the build. `web/worker/index.ts` adds three routes on top of the assets — `/api/latest` (proxies the release manifest for the download CTA), `/api/subscribe` (private-beta signups, honeypot-filtered and forwarded to listmonk), and `/api/report` (gzipped diagnostic bundles from the app's problem reporter, written to the private `hive-desktop-reports` R2 bucket; ADR 0024). Deploys via `.github/workflows/deploy-web.yml` on pushes to main touching `web/**`, or locally with `npm run deploy`.
  - Nav and footer links with `"href": null` in `web/src/data/site.json` are not rendered; filling in the URL is all it takes to bring one back.
  - A `/docs` section is a content change: add an Astro content collection under `web/src/content/` and a route — the page shell, styles and deploy path stay as they are.

## Development tooling — `cmd/`

Binaries that support development and release; none ship inside the app.

- **`cmd/release`** — version selection, signing, notarization, and R2 publishing (`mise run release:desktop`).
- **`cmd/vendorhive`** — the `internal/hivecore/` sync tool (`mise run vendor`).
- **`cmd/devserver`** — a loopback GitHub proxy for development (`mise run devserver`). Caches responses across dev instances so concurrent worktrees share one rate-limit budget, and rewrites them from config to simulate lifecycle events; also pushes webhook payloads at a running instance. **One proxy serves every worktree** and `desktop:dev` is routed through it by default (`launch.env`); opt out by setting `HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE=""` in `overrides.env`. Starting a second one parks it as a standby that takes over when the live one stops. Its only config is the checked-in `cmd/devserver/devserver.yaml`. See `cmd/devserver/README.md` and ADR 0017.
- **`cmd/internal/devproxy`** — the address-and-health contract shared by `cmd/devserver` and `cmd/devtools`, so the proxy's port and the worktree's `launch.env` cannot drift apart.

**Dev session:** `solo up` brings up the whole session from the checked-in `.solo.yml` — a `devserver` tmux window and a `desktop:dev` window — and `solo down` tears it down. The config is versioned in the repo rather than `~/.config/solo` so it stays in step with the mise tasks it invokes and works from any worktree.

## Vendored code — `internal/hivecore/`

- Vendored from `colonyops/hive` `internal/` packages at the SHA pinned in `cmd/vendorhive/vendor.lock`; import paths are rewritten by the sync tool.
- **Never edit vendored files.** Changes land in `colonyops/hive` first, then re-run the vendor sync. CI enforces this with a drift check.
- `pkg/` packages from hive are a normal `go.mod` dependency, pinned to the same SHA by the sync tool.

## Distribution

Release CI signs and notarizes the macOS app, then uploads versioned artifacts plus a `latest.json` manifest (version, URL, sha256) to Cloudflare R2 behind a stable download domain. The in-app updater polls the manifest; the landing page download link resolves through it. Tags use the `desktop-v*` namespace as the version anchor.

## Comments

Comment sparingly. A comment earns its place only where a reader would
otherwise be confused: a non-obvious invariant, a footgun, or why an unusual
choice was made over the obvious one. Never narrate what the code does, and
never encode reasoning that was worked out while writing it — a design
rationale belongs in an ADR, or nowhere.

Draft, then delete every comment a competent reader could derive from the code
itself.

Much of the existing code is densely commented. It is not the target; do not
match it.

The same restraint applies to prose. An ADR states the decision and the
constraint that forced it, not every alternative considered. Keep additions to
`docs/architecture.md` to what a future implementer must follow.

## Documentation

- `docs/architecture.md` is the standing architectural reference — see [Before building a feature](#before-building-a-feature). Keep it current when the shape changes; it is reviewed as a spec, not as prose.
- Record notable architecture/infrastructure decisions as ADRs in `docs/decisions/` (next number, Status/Date/Context/Decision/Consequences) and add them to the index in `docs/README.md`. Mark superseded ADRs instead of deleting them. An ADR records *why one choice was made*; `architecture.md` records *the shape that resulted*. A decision that changes the shape updates both.
- Concrete distribution facts (bucket, domains, manifest schema, publish/rollback runbook, credentials) live in `docs/distribution.md` — keep it current when infra changes.

## Quality gates

Every gate is a mise task (`mise tasks`); lefthook runs the relevant ones as git hooks so they fire without anyone remembering to. `mise install` wires them up (mise `postinstall` → `scripts/hooks/install.sh`); re-run `mise run setup` after editing `lefthook.yml`. CI remains the source of truth — hooks are a fast local mirror.

- **pre-commit** (~0.1s): formats staged Go files (`golangci-lint fmt`) and re-stages them; blocks edits to vendored `internal/hivecore/`; when a generator input is staged, regenerates and blocks if the committed output differs. A partially staged Go file gets its unstaged hunks staged too — stage whole files.
- **pre-push** (~2s, ~6s when the push touches `desktop/frontend/`): `mise run check` — `check:generate`, `check:tidy`, `lint`, `test` — plus the frontend unit tests when the push touches `desktop/frontend/`. The gate's contents and their order live in the `check` task, so the hook, CI, and the release workflow cannot drift from it. Jobs are piped, so the first failure stops the rest. `check` regenerates in place to detect drift, but no longer tidies `go.mod` behind your back — `mise run tidy` is the mutating counterpart to `check:tidy`.

- **CI-only** (too slow or too network-bound for a hook, so they are deliberately not in `check`): `check:deadcode` reports functions unreachable from any `main` or test — run it after deleting a caller, since that is what strands a helper. `check:vuln` runs `govulncheck` and fails only when a vulnerable dependency symbol is actually reachable from this code. Both are mise tasks, so you can run either locally when a change warrants it.

Wails TS bindings and the e2e suite are deliberately not hooked — both need a full app build. Run `mise run desktop:generate` / `mise run desktop:e2e` when the change warrants it; CI covers them either way.

**Never bypass a hook** — no `LEFTHOOK=0`, `git commit -n`, or `git push --no-verify`. The escape hatch exists for human emergencies; a failing gate is a task to finish, not a flag to add.

## Git standards

- Never push to main; branch and PR (`feat/`, `chore/`, `fix/` prefixes).
- All commits signed.
