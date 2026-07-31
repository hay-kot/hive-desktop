# 0045 — A process-managed terminal backend beside the tmux one, to measure what tmux costs

- **Status:** proposed
- **Date:** 2026-07-30

## Context

Every terminal in this app goes through tmux. `tmuxcc` attaches `tmux -C` to a
session, parses control mode, decodes `%output`'s octal escapes, and renders
what tmux says the pane holds. tmux buys three things: sessions that outlive the
app, a screen that can be re-rendered from `capture-pane` on re-attach, and
sharing with whatever else the user has attached.

It also sets the terms for everything around it. Size is a negotiation this app
is only one voice in (ADR 0036), so a pane can render smaller than the box it
sits in and the frontend has a banner explaining why. Availability depends on a
binary that must be discovered (ADR 0039) and can be missing or too old. The
slug has to equal the tmux session name in both products (ADR 0040). A session
that was never spawned needs a start affordance (ADR 0044).

Nothing had measured what that costs, and the alternative — this process owning
a PTY per tab, which is what every terminal emulator does — was never built to
compare against. There are also uses where tmux's one advantage does not apply
at all: a pop-up terminal, a scratch shell, anything meant to be ephemeral.

## Decision

1. **`internal/app/ptyterm` is a second terminal backend, built as a sibling to
   `tmuxcc` rather than behind an abstraction over it.** It owns a PTY and a
   login shell per tab, a byte ring per window for replay, and one coalescing
   subscriber per session. It emits the same event vocabulary — `Output`,
   `WindowChanged`, `LifecycleChanged` with the same kind strings — so both
   backends serve one wire and one renderer.

2. **The two are addressed identically and differ only in prefix.**
   `/api/terminal/pty/…` mirrors `/api/terminal/…` operation for operation,
   reusing its request and response types, its bearer token, and its CORS
   policy by sitting under the same path prefix. `app.PtyTerminalsService`
   mirrors `app.TerminalsService`. The frontend's `TerminalClient` is one
   interface with two constructions, so `useTerminalWindows` — the pool, the
   xterm wiring, the resize handling — is shared unchanged.

   This is what makes the comparison honest: if the transports differed, the
   numbers would measure the socket rather than the engine. A frame that
   encodes differently on one backend is a bug, and
   `TestPtyFramesMatchTheTmuxWire` is what says so.

3. **Attach never spawns on either backend.** A shell in a checkout costs far
   less than a tmux session running an agent, so ADR 0044's reasoning does not
   transfer on its own — but one contract means one frontend, and the start
   panel is where the difference in what gets started is explained.

4. **The comparison is a tool, not an impression.** `cmd/termbench` drives both
   `Manager`s directly, below HTTP and below xterm, running the same `/bin/sh`
   on both so a user's prompt cannot dominate the result. On an M-series
   laptop, 200 round trips and 10 cold starts:

   | metric | tmux | pty |
   | --- | --- | --- |
   | cold start p50 | 13.2 ms | 0.92 ms |
   | first paint p50 | 0.13 ms | 0.02 ms |
   | echo round trip p50 | 0.11 ms | 0.03 ms |
   | echo round trip p99 | 0.23 ms | 0.10 ms |
   | throughput (8 MiB `cat`) | 37.3 MiB/s | 130.9 MiB/s |

   Both engines delivered the same 8.1 MiB, so the throughput gap is the cost
   of control mode's encode/decode and the extra process hop, not a difference
   in what reached the screen.

5. **The switch is a comparison control, not a setting.** Terminal mode renders
   a tmux|pty toggle in the sidebar only while both backends are mounted, and
   switching drops every pooled attach — window ids and sessions belong to one
   engine. It is remembered per install so a comparison survives leaving the
   view.

## Consequences

- **Two backends exist for one surface.** They are kept in step by convention
  and by the wire test, not by a shared interface, and that is deliberate for
  as long as this is an experiment: an abstraction over both would have to be
  designed around whichever one is going to lose, and it is not yet known which.
  Whichever way that resolves, one of them gets deleted rather than adapted.
- **A pty session dies with the app.** No detach, no reattach after a restart,
  nothing to attach to from a real terminal. This is the trade the experiment
  exists to price, and the start panel says so rather than letting a user
  discover it after a crash.
- **The replay ring is not `capture-pane`.** tmux re-renders the visible grid
  from its own emulator, so a snapshot is always a coherent screen. A byte ring
  is the raw stream with its head cut off: a replay can begin inside an escape
  sequence, and it re-runs alternate-screen transitions the pane already made.
  It restores scrollback that `capture-pane` cannot, and it costs a resident
  buffer per window. A backend that wins on everything else still has to answer
  this before it can replace tmux.
- **Size stops being a negotiation.** One client renders these PTYs, so a resize
  is applied rather than voted on, and the size-constraint banner cannot fire.
  If the pty backend wins, ADR 0036's negotiation rules and the banner go with
  tmux.
- **What is not built.** Panes, session sharing, a detachable daemon, and
  Windows (ConPTY). None of them are needed to price the trade, and building
  them before it is priced would be building the losing side twice.
