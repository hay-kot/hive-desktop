# Agent Instructions

Monorepo for the Hive desktop app and the landing page.

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

- **Root module** `github.com/hay-kot/hive-desktop` — the desktop app (`desktop/`, `internal/app/`, `internal/adapter/`) and vendored hive core (`internal/hivecore/`). See [`desktop/AGENTS.md`](desktop/AGENTS.md).
- **`web/`** — the landing page and public docs at `hivedesktop.com`, a Zensical site served as Cloudflare Worker static assets, with its own `mise.toml` (`cd web && mise run build`). See [`web/AGENTS.md`](web/AGENTS.md).

## Development tooling — `cmd/`

Binaries that support development and release; none ship inside the app.

- **`cmd/release`** — version selection, signing, notarization, and R2 publishing (`mise run release`).
- **`cmd/vendorhive`** — the `internal/hivecore/` sync tool (`mise run vendor`).
- **`cmd/adr`** — decision records: `adr new` mints `docs/decisions/YYYY-MM-DD-slug.md` (`mise run adr:new`), `adr check` gates ids and citations (`mise run check:adr`). See ADR adr-ids-are-not-allocated.
- **`cmd/devserver`** — a loopback GitHub proxy for development (`mise run devserver`). Caches responses across dev instances so concurrent worktrees share one rate-limit budget, and rewrites them from config to simulate lifecycle events; also pushes webhook payloads at a running instance. **One proxy serves every worktree** and `dev` is routed through it by default (`launch.env`); opt out by setting `HIVE_DESKTOP_DEVELOPMENT_GITHUB_API_BASE=""` in `overrides.env`. Starting a second one parks it as a standby that takes over when the live one stops. Its only config is the checked-in `cmd/devserver/devserver.yaml`. See `cmd/devserver/README.md` and ADR devserver-github-proxy.
- **`cmd/internal/devproxy`** — the address-and-health contract shared by `cmd/devserver` and `cmd/devtools`, so the proxy's port and the worktree's `launch.env` cannot drift apart.

**Dev session:** `solo up` brings up the whole session from the checked-in `.solo.yml` — a `devserver` tmux window and a `dev` window — and `solo down` tears it down. The config is versioned in the repo rather than `~/.config/solo` so it stays in step with the mise tasks it invokes and works from any worktree.

## Vendored code — `internal/hivecore/`

- Vendored from `colonyops/hive` `internal/` packages at the SHA pinned in `cmd/vendorhive/vendor.lock`; import paths are rewritten by the sync tool.
- **Never edit vendored files.** Changes land in `colonyops/hive` first, then re-run the vendor sync. CI enforces this with a drift check.
- `pkg/` packages from hive are a normal `go.mod` dependency, pinned to the same SHA by the sync tool.

## Comments

Much of the existing code is densely commented. **It is not the target; do not
match it.** Draft, then delete every comment a competent reader could derive
from the code itself.

The same restraint applies to prose. An ADR states the decision and the
constraint that forced it, not every alternative considered. Keep additions to
`docs/architecture.md` to what a future implementer must follow.

## Documentation

- `docs/architecture.md` is the standing architectural reference — see [Before building a feature](#before-building-a-feature). Keep it current when the shape changes; it is reviewed as a spec, not as prose.
- Record notable architecture/infrastructure decisions as ADRs in `docs/decisions/`. **Start one with `mise run adr:new -- "The decision, as a sentence"`** — it writes `YYYY-MM-DD-slug.md` with the Status/Date/Context/Decision/Consequences skeleton. Never hand-name the file and never add a number: there is nothing to allocate, which is what lets concurrent branches each add an ADR without colliding (ADR adr-ids-are-not-allocated).
- Cite an ADR by its slug alone — `(ADR terminal-transport)` — and link it as `[…](decisions/2026-07-28-terminal-transport.md)`. `mise run check:adr` fails on a citation that does not resolve, so renaming an ADR means fixing its citations in the same change. Mark superseded ADRs instead of deleting them.
- An ADR records _why one choice was made_; `architecture.md` records _the shape that resulted_. A decision that changes the shape updates both.
- Concrete distribution facts (bucket, domains, manifest schema, publish/rollback runbook, credentials) live in `docs/distribution.md` — keep it current when infra changes.

## Quality gates

Every gate is a mise task (`mise tasks`); lefthook runs the relevant ones as git hooks so they fire without anyone remembering to. `mise install` wires them up (mise `postinstall` → `scripts/hooks/install.sh`); re-run `mise run setup` after editing `lefthook.yml`. CI remains the source of truth — hooks are a fast local mirror.

- **pre-commit** (~0.1s): formats staged Go files (`golangci-lint fmt`) and re-stages them; blocks edits to vendored `internal/hivecore/`; when a generator input is staged, regenerates and blocks if the committed output differs. A partially staged Go file gets its unstaged hunks staged too — stage whole files.
- **pre-push** (seconds; longer when the push touches `desktop/frontend/`): `mise run check`, plus the frontend unit tests when the push touches `desktop/frontend/`. The gate's contents and their order live in the `check` task, so the hook, CI, and the release workflow cannot drift from it — read the task, not a copy of it. Jobs are piped, so the first failure stops the rest. `check` regenerates in place to detect drift, verifies released SQLite migrations are immutable and newly appended in order, but no longer tidies `go.mod` behind your back — `mise run tidy` is the mutating counterpart to `check:tidy`.

- **CI-only** (too slow or too network-bound for a hook, so they are deliberately not in `check`): `check:deadcode` reports functions unreachable from any `main` or test — run it after deleting a caller, since that is what strands a helper. `check:vuln` runs `govulncheck` and fails only when a vulnerable dependency symbol is actually reachable from this code. Both are mise tasks, so you can run either locally when a change warrants it.

**`mise run ci` runs every gate, including the one CI does not.** Prefer it over
pushing to find out: it is the superset `check` does not cover (bindings, vendor
drift, deadcode, govulncheck, the frontend build and tests), it installs
frontend dependencies if they are missing, and it ends with the e2e suite. PR CI
runs `mise run test`; merges to main run `mise run test:race`, so a race only
the detector sees is caught post-merge rather than in review (ADR ci-runs-on-main-to-seed-the-cache-prs-read).

Everything up to e2e finishes in ~20s; e2e itself builds a container image and
takes minutes, so it runs last and alone. **It is not in GitHub CI** — it was
dropped for being too slow, which means `mise run ci` is the only thing that
runs it. Skipping it locally means nobody does (ADR the-e2e-suite-runs-locally-via-mise-run-ci-not-in-github-ci).

Wails TS bindings are deliberately not hooked — they need a full app build. Run `mise run bindings` when the change warrants it; CI checks them either way.

**Never bypass a hook** — no `LEFTHOOK=0`, `git commit -n`, or `git push --no-verify`. The escape hatch exists for human emergencies; a failing gate is a task to finish, not a flag to add.
