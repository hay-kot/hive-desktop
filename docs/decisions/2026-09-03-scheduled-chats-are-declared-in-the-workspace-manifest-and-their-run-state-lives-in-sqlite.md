# Scheduled chats are declared in the workspace manifest and their run state lives in SQLite

- **Status:** accepted
- **Date:** 2026-09-03

## Context

A workspace's agent should be able to run recurring work on its own: every
Friday at 09:00, start a chat in the "Product" workspace with a prompt that
asks the agent to query its MCP servers and write a summary. The user
schedules this from the Chats area, the app launches the chat when it comes
due, and a launch the app missed because it was closed at the scheduled time
still happens the next time the app is running. A prompt is a template, so
one schedule can say "summarize since `{{ .LastRun }}`."

Nothing like this exists today: there is no cron parser, no scheduler, and no
way to start an agent workspace session with an initial prompt. The word
"job" is already taken (`internal/app/jobs` is live action runs), so this
feature is called **schedules** for the definition and **runs** for one
execution.

## Decision

**The schedule is authored config; its run state is app-local data.** A
schedule lives under a new `schedules:` list in `agent-workspace.yaml`,
alongside `mcps:` and `skills:`: it is user-authored and agent-editable, it
travels with the workspace's dotfiles, the directory watcher already sees the
manifest change, and `agentws/write.go`'s node-tree editor already knows how
to add and remove a key without disturbing comments or unrelated content. How
far each schedule has been evaluated (its cursor) and its run history are not
something a user authors, so they live in `desktop-pipeline.db` instead, in
two new tables: `schedule_cursor` and `schedule_run`.

**The scheduling logic is a leaf package, `internal/app/schedule`.** It
imports only the standard library and `robfig/cron/v3`, so cron parsing,
prompt rendering, and the catch-up decision are testable with no tmux, no
database, and no `agentws` package in the loop. `Source`, `Store`, and
`Launcher` are consumer-defined interfaces the package declares; `App`
satisfies them with adapters over `agentws` and `store`, and the package
itself never imports either. One `Scheduler` runs as a single goroutine with
an App-owned `Start`/`Stop` lifecycle, the same shape every other
long-running subsystem in this app uses, started after the agent-workspace
watcher and stopped before `terminals.Stop`.

**A cursor never lets a schedule fire for the past.** A schedule with no
cursor yet, or one whose stored cursor was evaluated against a different
`cron` expression, gets its cursor set to now with no run: a schedule that
was just created, or whose timing was just edited, starts counting from that
moment rather than replaying whatever it would have fired since some earlier
default. This is what keeps an edited `cron` from surfacing a pile of
catch-up runs nobody asked for.

**A missed occurrence is one run, not one run per occurrence.** If the app
was closed through several scheduled times, the next pass sees every
occurrence since the cursor, coalesces them into a single run tagged
`catch_up`, and records how many were folded in. `on_missed: skip` replaces
that launch with a `skipped` record instead, for a schedule where a stale
catch-up would do more harm than a gap. Either way the cursor advances past
everything in the window, so a schedule never falls further and further
behind.

**A run is skipped, not queued, while the previous one is still live.** Before
launching, the scheduler checks whether the schedule's last launched run's
tmux session is still up (`Launcher.SessionLive`). If it is, the new
occurrence is recorded as `skipped` rather than starting a second chat on top
of one still running. A launch failure is recorded and never retried; the run
history is where a user finds out.

**The loop sleeps in bounded steps, capped at one minute.** darwin stops a
process's monotonic clock while it is asleep, so a single long timer set
before sleep fires late by however long the machine was asleep. Waking on a
bounded sleep instead means the scheduler is never more than a minute behind
catching up once the machine is running again.

**The prompt is `text/template`, rendered against a fixed data set** (`Now`,
`ScheduledFor`, `LastRun`, `Reason`, `Missed`, the schedule's own fields, and
the workspace's), with `missingkey=error` so a typo in a variable name fails
the schedule instead of leaking `<no value>` into the agent's opening
message.

**A scheduled launch is an ordinary session launch, only detached.** It goes
through `AgentWorkspacesService.StartScheduledSession`, which opens the
workspace first, so the manifest and generated files (`.mcp.json` and the
rest) are current before anything runs, then starts the session the same way
a user-initiated one starts, with the rendered prompt appended behind the `--`
end-of-options marker as the agent's final positional argument -- a prompt is
prose, and one that opens with a hyphen (a markdown list) is otherwise read as
an unknown option. `Detached: true` skips the tmux attach a
UI-driven launch performs, since there is no control client waiting to
receive it, and the chat then shows up in the sidebar like any other session
once its window exists.

## Consequences

- A user can rely on: a schedule never fires for a time before it (or its
  current `cron`) existed; a launch missed while the app was closed still
  happens, once, the next time the app runs, unless `on_missed: skip` says
  otherwise; and an edit to `schedules:` -- whether made in the editor or by
  the agent itself -- takes effect on the workspace's next open, with no
  restart needed.
- A user cannot rely on: automatic retry of a failed launch (a failure is
  recorded, not retried); anything but local time (a `cron` expression carries
  no timezone field); or a second chat starting on top of one that is still
  running for the same schedule.
- `desktop-pipeline.db` gains two tables and a migration
  (`0008_schedules.up.sql`); their rows are app-local state, not part of the
  workspace folder a user might sync or put under version control.
