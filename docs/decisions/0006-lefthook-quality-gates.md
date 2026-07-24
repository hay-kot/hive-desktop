# 0006 — Quality gates enforced by lefthook git hooks

- **Status:** accepted
- **Date:** 2026-07-23

## Context

The quality gates (`fmt`, `lint`, `test`, generated-file and vendor drift checks) existed only as opt-in mise tasks. Most commits here come from headless agent sessions with no human watching the terminal, so "remember to run the gates" is not an enforcement mechanism: unformatted or broken commits land, and CI catches them a push and several minutes later.

## Decision

`lefthook.yml` defines pre-commit and pre-push hooks. Every job shells out to a mise task, so a gate has exactly one definition shared by the hook and by CI.

The split is by cost, because a slow pre-commit stalls agent loops:

- **pre-commit** (~0.1s): `golangci-lint fmt` on staged Go files, a guard against editing vendored `internal/hivecore/`, and a generated-code drift check that only fires when a generator input is staged.
- **pre-push** (~5s): `check:tidy`, `lint`, `test`, `check:generate`, and the frontend unit tests when the push touches `desktop/frontend/`. Push is the wrap/PR boundary for headless work, so the full gates belong here.

Formatting **auto-fixes and re-stages** rather than failing: a blocked commit costs an agent a whole turn to re-run and re-commit for whitespace. The trade-off is that a partially staged file gets its unstaged hunks staged along with the formatting.

Hooks install from mise's `postinstall` hook (`scripts/hooks/install.sh`), so `mise install` on a fresh clone is the only setup step. The installer pins the clone's local `core.hooksPath` to its own `.git/hooks`: lefthook refuses to install while a `core.hooksPath` is set (a common global dotfiles setting), and pinning it locally means the forced install can only ever write into this repo.

## Consequences

- Unformatted Go, hand-edited vendored code, stale generated output, an untidy `go.mod`, and failing tests are all caught locally within seconds of being written.
- CI stays the source of truth. Gates too slow for a hook — Wails TS binding drift, the Docker/Playwright e2e suite, `-race`, and the network-dependent `check:vendor` — run only there.
- A global `core.hooksPath` is overridden in this repo; global hooks do not run here.
- `lefthook.yml` changes need `mise run setup` to re-sync the hook files, since lefthook's auto-sync declines to run while `core.hooksPath` is set.
- `LEFTHOOK=0` / `--no-verify` remain available for human emergencies; AGENTS.md forbids agents from using them.
