# Development

How to build and run the repository. For what to build and where it goes, read
[`architecture.md`](architecture.md) first — it is the standing spec, and new
work is reviewed against it.

## Layout

| Component                     | Path                                               | Notes                                                                                        |
| ----------------------------- | -------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Desktop app (Wails v3, Vue 3) | `desktop/` + `internal/app/` + `internal/adapter/` | The Wails shell and its native behaviour are in `desktop/README.md`                           |
| Vendored hive core            | `internal/hivecore/`                               | CLI-managed by `cmd/vendorhive` — **read-only**                                              |
| Landing page and public docs  | `web/`                                             | Zensical on Cloudflare Workers static assets → [hivedesktop.com](https://hivedesktop.com); `cd web && mise run build` |
| Development and release tools | `cmd/`                                             | Release publisher, vendor sync, ADR tool, dev GitHub proxy. None of it ships inside the app.  |

- Root Go module `github.com/hay-kot/hive-desktop` owns the desktop app and the vendored core.
- Code under `internal/hivecore/` is vendored from `colonyops/hive` at the SHA pinned in `cmd/vendorhive/vendor.lock`. Never edit it here: change hive first, then re-vendor.
- Releases are signed, notarized, and uploaded to Cloudflare R2 behind a stable domain — versioned zips plus a `latest.json` manifest that drives the in-app updater and the landing-page download link. GitHub releases are not user-facing.

## Setup

```sh
mise trust                         # once per clone, before mise reads mise.toml
mise install                       # toolchain + git hooks (lefthook)
cd desktop/frontend && npm ci      # frontend deps, for the desktop app and its tests
```

`mise install` also installs the git hooks, so a fresh clone gets the quality gates with no extra step (`mise run setup` re-installs them on demand). `mise tasks` lists every gate and build task.

Installing the hooks sets this clone's `core.hooksPath` to its own `.git/hooks`, which takes precedence over a global `core.hooksPath` — global hooks will not run in this repo.

## Running it

```sh
mise run dev                       # the app, against this worktree's isolated instance
mise run build                     # a local app build
```

Each worktree gets its own desktop instance and generated `launch.env`, so two
checkouts never share state. `mise run dev:fresh` recreates one.

## Quality gates

Every gate is a mise task, and lefthook runs the relevant ones as git hooks.

- **pre-commit** formats staged Go files and blocks edits to vendored `internal/hivecore/`.
- **pre-push** runs `mise run check` (generated-code drift, ADRs, migrations, tidy, lint, test), plus the frontend unit tests when the push touches `desktop/frontend/`.
- **`mise run ci`** is the superset: everything CI runs, plus the Wails bindings check, deadcode, govulncheck, and the e2e suite that GitHub CI does not run.

Read the `check` task rather than a copy of it — it is the single definition the
hooks, CI, and the release workflow all share. Details, including why each gate
sits where it does, are in [`../AGENTS.md`](../AGENTS.md).

Never bypass a hook. A failing gate is a task to finish, not a flag to add.
