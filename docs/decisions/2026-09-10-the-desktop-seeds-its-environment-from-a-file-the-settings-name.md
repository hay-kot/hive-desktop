# The desktop seeds its environment from a file the settings name

- **Status:** accepted
- **Date:** 2026-09-10

## Context

A launched `.app` has the launcher's environment, not a shell's: a running
instance carries about a dozen variables and no `HIVE_*` at all. Two routes
already work around that and neither is a place to put a value on purpose.
`execenv`'s login shell probe answers what a terminal would show
(ADR [hive-env-overrides-resolve-through-the-login-shell](2026-08-06-hive-env-overrides-resolve-through-the-login-shell.md)),
which is implicit — it depends on startup files behaving, and a user who sets
nothing in an rc file has nothing to read. `launchctl setenv` is invisible,
session-scoped and gone at logout.

Hive's own config load reads `os.Getenv` once, deep in vendored code
(`config.applyEnvironmentOverrides`), so in the desktop it is a permanent no-op:
`HIVE_CONTEXT_BASE_DIR` and `HIVE_GIT_PATH` cannot be set at all. #438 was the
same gap seen from the other side — the New Session form read the probe and the
launch read `os.Getenv`, so the two disagreed about which agent to run.

A config directory is frequently synced. Putting the env file at a fixed
location inside it would carry one machine's environment to the other, which is
the opposite of what a per-machine override is for.

## Decision

1. **`environment.file` names the file; the default is machine-local.** The
   *setting* syncs — both machines name the same path — while the file at that
   path does not. Empty resolves to `<XDG_CONFIG_HOME>/hive/desktop/.env`,
   beside `bootstrap.yaml` and, like it, ignoring a relocated config root
   (`settings.FixedConfigDir`).

2. **It seeds the process environment at startup, and adds no resolver.**
   `execenv.ApplyFile` sets the names this process does not already carry,
   before telemetry resolves its `env:` references and before `app.New` loads
   hive's config. Everything downstream reads the environment exactly as it did:
   `Getenv` prefers the process over the shell, `Environ` builds on
   `os.Environ`, and hive's overrides start working for the first time. The
   order is **launch environment, then this file, then the login shell** — most
   specific first, and no new place for the app to disagree with itself.

3. **`PATH` and `HIVE_DESKTOP_*` are refused, everything else is adopted.**
   `PATH` is the resolver's answer and its layering is an ADR. `HIVE_DESKTOP_*`
   is `settings.yaml`'s namespace, and `settings.yaml` is the file that named
   this one: honouring it here would apply on a later `Effective()` reload but
   not at the startup read, which is precisely the disagreement #438 was. Both
   are logged and skipped; the rest of the file still applies. Adoption is
   otherwise open, for the reason the PATH prefixes were never enough — the set
   of variables a spawned tool reads is not closed.

4. **Startup only.** `settings.yaml`, `flows/` and `actions.yml` hot-reload;
   this does not. A process that is already running holds the values it started
   with, so re-reading would only make the app's environment disagree with the
   environment of the sessions it has already spawned. Editing the file, or the
   setting that names it, takes a relaunch.

5. **A missing file is a no-op; a broken one applies nothing.** A file that does
   not parse is rejected whole — half an environment is worse than none — and
   the app starts anyway, since the UI is where the user fixes it. Errors name
   the line and the variable, never the value: a `.env` is where tokens live, so
   nothing logs a value.

## Consequences

- Hive's environment overrides work in the desktop. `HIVE_DEFAULT_AGENT` no
  longer needs the `tmux set-environment` patch a user wrote by hand, and
  `HIVE_CONTEXT_BASE_DIR`/`HIVE_GIT_PATH` become reachable without a terminal
  launch.
- `telemetry.token: env:NAME` becomes usable in an installed app. It was
  documented as a poor choice there because a launched `.app` has no
  environment; now it has whatever this file gives it.
- The file changes every subprocess the app spawns, since the values reach
  `Environ`. That is the point — it is the shell rc a GUI launch never reads —
  but it is a wider blast radius than a Hive-only file would have been.
- A per-machine `HIVE_DESKTOP_*` override still has no home. The two that
  relocate directories (`HIVE_DESKTOP_CONFIG_DIR`, `HIVE_DESKTOP_DATA_DIR`)
  could not be honoured from here anyway — they decide where the settings file
  that named this one lives. `bootstrap.yaml` is that mechanism.
- A mock or e2e launch skips the file, the same way it skips the login shell
  probe: a fixture run must not take the machine's environment.
- Values are single-line. A quoted value that does not close on its line is an
  error rather than the start of a multi-line value, so a pasted PEM has to be
  a `file:` reference instead.
