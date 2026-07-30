# 0041 — settings.yaml reloads at runtime, with a last-good snapshot and one restart-pending answer

- **Status:** accepted
- **Date:** 2026-07-30

## Context

ADR 0014 resolves `settings.yaml` once at startup and injects the result. Every
subsystem that copied a value out of that snapshot — and every value baked into
composition — kept it until the app was relaunched, so editing the file did
nothing to the running app. `flows/*.yaml` and `actions.yml` had hot-reload with
last-good semantics; the third config file in the same directory did not.

Two consequences, both hit by beta users:

- Flipping `experimental.terminal` or pointing `paths.tmux` somewhere new
  appeared to do nothing. The documented answer — restart after any settings
  change — was true but indistinguishable from a bug.
- `Effective()` and `Persisted()` were pure per-call file reads with no
  snapshot, so while the file was momentarily unparsable a **running** app
  degraded silently: the notification gate suppressed every flow notification,
  the prompts page reported the HTTP listener as disabled, the problem reporter
  reported an empty channel, and every settings write failed — including
  `App.Start`'s write-back of an OS-allocated HTTP port.

Three separate hand-rolled comparisons had also grown to answer the same
question — does the persisted value differ from what this process mounted:
`terminalRestartPending` in the frontend, `WebhookState.RestartRequired`, and the
directory-override banner. Each covered one field and none covered a field added
later.

## Decision

**The store serves a last-good snapshot.** `settings.Store` holds a guarded
`Settings` plus the error from the most recent load. `Current()` returns it by
value and never fails; `Reload()` swaps it and leaves the previous one in service
when the new file will not parse or validate. Every core read goes through
`Current()`, which also removes observation skew: two values read within one
operation come from one atomic swap rather than two file loads an edit can land
between. `Effective()`/`Persisted()` remain pure reads for the composition root
and tooling.

A **write** still fails while the file is broken. Last-good is what keeps the app
reading; writing it back would overwrite whatever the user has mid-edit.

**One reload path.** `App.ReloadSettings` reloads, diffs against the snapshot it
replaces, applies the fields this process can adopt, publishes
`events.SettingsUpdated{Changed}`, and records the reload to the activity log. A
file that fails validation publishes nothing, keeps the running values, and
records the failure instead — mirroring "a flow that cannot be built keeps its
predecessor in service". The watcher, `POST /api/settings/reload`, and any future
caller all go through it.

**`settings.Watcher` makes an outside edit live.** A near-verbatim copy of
`actions.ActionsWatcher`: watch the directory (editors replace by rename),
`MkdirAll` first, 250 ms debounce, exact basename match, `stopOnce`, and a
construction failure that degrades to no hot-reload rather than a fatal. The
basename match must be exact because `Store`'s atomic write creates
`.settings-*.yaml` siblings in the same directory. Startup keeps its
fail-fast behaviour: a broken file at launch is still fatal in `main`, because
there is no last-good to fall back to yet.

**Every field is classified, and the classification is exhaustive.**
`settingsReload` in `internal/app` maps each dotted field to either "" (the
running process adopts it) or the reason it cannot. A test fails when a field of
`settings.Settings` is missing from that map, so adding one to the schema forces
the decision instead of defaulting it to silence. The diff itself walks the
schema by reflection over the YAML tags, so it cannot fall behind the struct.

Startup-only, and why:

| Field | Why a relaunch |
| --- | --- |
| `http.enabled` / `host` / `port` | The loopback server binds once, and `MountAPI` attaches the agent API, the terminal stream and pprof before `Start` — a replacement listener would lose all three. |
| `experimental.terminal` | Its Wails service is fixed at `application.New`, its bearer token is minted per run, and route *absence* is the semantic (ADR 0037 §2). |
| `updates.channel` | The update engine is handed its channel once, at `Init`. |
| `skills.auto_update` | Installed skills are re-synced once, at startup. |
| `development.mocks.mode` | Selects which objects exist and re-resolves the path snapshot. |
| `development.github.api_base` | The GitHub client is built once with its API base. |
| `development.pprof.enabled` | Mounts on the loopback server before it starts (ADR 0023). |
| `development.debug.pause_*` | Read when the store opens. |
| `development.vite.*` / `wails.*`, `development.instance.id` | The dev launcher bridges these before Go starts. |

**`App.RestartPending` is the one restart-pending answer.** It diffs the
persisted settings against a `mounted` snapshot — what this process started with
— filtered to the startup-only set, and appends the bootstrap `data_dir` /
`config_dir` overrides compared against the injected path snapshot. Each row
carries the field, the reason, the running value and the persisted one.
`WebhookState.RestartRequired` is that list filtered to `http.*`, and the
frontend's terminal hint and restart banner are filters over the same list, so a
startup-only setting added later surfaces with no new comparison written.

**Persist and apply are separate.** `SetGithub` and the updater's `SetEnabled`
each split into a persist half and an apply half; the reload path calls only the
apply half. A reload subscriber that persisted would rewrite the file and
retrigger the watcher that called it. The frontend's one-time theme adoption is
guarded to the first hydrate for the same reason.

## Consequences

- Editing `settings.yaml` in an editor, or syncing it from another machine,
  takes effect in about 250 ms for everything that is not startup-only. The app's
  own writes are live before the watcher sees them — `Update` swaps the snapshot
  as it saves — so the reload they trigger diffs to nothing and publishes
  nothing. `SettingsUpdated` fires only when the file has moved away from what
  the process is serving, which is exactly when a subscriber is stale. Unlike
  flows and actions, which publish unconditionally on every watcher tick, the
  diff gate also means a subscriber that persists cannot retrigger itself — the
  persist/apply split below is the backstop, not the primary mechanism.
- A broken `settings.yaml` no longer degrades a running app. It keeps the last
  good values, records `Could not reload settings.yaml` to the activity log, and
  answers `400` on `POST /api/settings/reload`.
- ADR 0014's "resolve once at startup" is amended for `settings.yaml` values.
  The **path** snapshot is unchanged and still immutable for the process's life —
  the config root determines where `settings.yaml` lives, so it cannot itself
  move underneath a reload.
- `app.Config` no longer carries a `Settings` value. A copy handed in beside the
  store would be a second source a reload could not reach.
- `settings.yaml` is `yaml.Marshal`ed on every UI write (ADR 0032), so comments
  are still lost. That was already true; a watcher makes it more visible, because
  a user is now more likely to be editing the file by hand.
- `Update`'s load-modify-save is still not atomic against an external editor: an
  edit landing between its load and its save is overwritten. Making the window
  smaller is a follow-up, not a blocker.
- `POST /_e2e/reset` rewrites `settings.yaml` only when a spec changed it, so a
  reset triggers a reload exactly when an external edit would.
