---
title: Terminal mode
description: Attach to a session's tmux windows inside the app — what it does, and which of its rough edges are tmux's rules rather than bugs.
group: Concepts
order: 1
---

Terminal mode attaches to the **tmux session** behind a Hive session and renders
its windows as tabs, so the agent you launched from the feed is readable without
leaving the app. It is **experimental** and ships off.

## Turning it on

Settings ▸ System ▸ Experimental ▸ **Terminal mode**, then **relaunch Hive** —
the flag is read once at startup. The same edit in `settings.yaml`:

```yaml
experimental:
  terminal: true
```

Two things have to be true for it to work, and the mode says which one is
missing when it isn't:

- **tmux 3.2 or newer on your `PATH`.** Hive drives tmux's control mode, which
  is where the protocol it needs landed.
- **The local HTTP server is on** — `http: {enabled: true}`, the default. The
  terminal's transport rides that server; with no server there is no terminal.

Once it's on, the title bar carries a **Hub | Terminal** switch. The sidebar
lists your sessions, and picking one attaches to it.

## Starting a session that has no terminal yet

The tmux session behind a Hive session does not survive a reboot or a
`tmux kill-server`, and a session created by an action may never have had one —
but the session itself outlives all of that. Picking one of those in the sidebar
shows **Session not started** where the terminal would be, with a **Start
session** button. The same action lives in the row's ⋯ menu.

Starting builds the session from Hive's own `windows:` spawn configuration for
that repository — same windows, same working directory, same agent command as
`hive` would use — and then attaches.

It waits for that click on purpose: starting **runs your agent again** from a
clean slate, nothing is resumed from where it left off, and opening the app
re-attaches your last session on its own. Only **active** sessions can be
started; a recycled one has no checkout left to open a terminal in, which is why
the sidebar does not list it.

If a repository's rule uses the older command-based `spawn:` (launching a
terminal app of your own) rather than `windows:`, Hive cannot build the session
for you — start it with `hive` and it will be there to attach to.

## Killing a terminal without touching the session

The same ⋯ menu carries **Kill terminal…**. It kills the tmux session and
nothing else: the agent and anything else running in it stop, while the
checkout, your uncommitted work, and the session itself stay exactly as they
are. What is left is the **Session not started** panel, so starting it again is
one click away.

That is what separates it from the two below it in the menu — **Recycle** resets
the checkout, and **Delete** removes the session and its directory.

## Your own commands on a session or a window

Anything you would otherwise do by finding the checkout yourself — open it in
your editor, reveal it in Finder, run the test task, send a key to the agent's
window — can be an entry in that ⋯ menu. They are ordinary entries in
`actions.yml`, the same file the feed's actions live in, and an action says
where it is offered with `targets`:

```yaml
- id: open-in-zed
  label: Open in Zed
  type: shell
  targets: [session]
  command_template: 'zed {{ .Session.Path | shq }}'

- id: run-tests
  label: Run tests
  type: shell
  targets: [session]
  timeout: "10m"
  command_template: 'mise run test'

- id: interrupt
  label: Interrupt agent
  type: shell
  targets: [window]
  command_template: 'tmux send-keys -t {{ printf "%s:%s" .Session.Slug .Window.ID | shq }} C-c'
```

`targets: [session]` puts the entry in a session row's ⋯ menu;
`targets: [window]` gives a window row a menu of its own — one appears only once
something targets a window. Omitting `targets` means the feed, which is what
every action you already have means.

The session you clicked is what the templates render over:

| | |
| --- | --- |
| `{{ .Session.Path }}` | the checkout on disk |
| `{{ .Session.Slug }}` | the session's slug, which is also its tmux session name |
| `{{ .Session.Name }}`, `{{ .Session.Repo }}`, `{{ .Session.Branch }}` | its name, remote, and worktree branch |
| `{{ .Window.ID }}` | the tmux window id — on a `window` action only |

A **shell** action started this way runs in the session's checkout unless you
set `cwd`, so `mise run test` needs nothing else. It runs as a background job:
the jobs list is where it reports finishing or failing, and a failure carries
the end of its error output. A **clipboard** action is copied on the click
instead — `text_template: '{{ .Session.Path }}'` is a one-line "copy this
session's path".

The one type that cannot target a session or window is **launch-session**: it
creates a *new* session, which is what the feed and the New Session form are
for.

## It is a real attach, not a copy

Hive attaches as another tmux client, exactly as `tmux attach` in a terminal
does. Two things follow:

- **Closing Hive, or leaving the mode, leaves the tmux session running.** Your
  agent keeps working and nothing is killed on the way out — the same session is
  still there for `tmux attach -t <name>`.
- **You can be attached in both places at once**, and both show the same
  windows.

## Why the grid is sometimes not the size of the pane

This is the one that looks like a bug and isn't.

**Every client attached to a tmux session renders the same grid for a window.**
There is one size, and tmux picks whose it is — that's the `window-size` option:

| `window-size` | Who decides the size |
| --- | --- |
| `latest` (tmux's default) | the client used most recently |
| `smallest` | the smallest attached client |
| `largest` | the largest attached client |

So if the session is also open in a terminal, the two of you share one size.
When the other client's size wins, Hive draws that grid inside a pane that would
fit a different one, and says so in a note above the terminal. The ways out:

- detach the other client — `prefix + d` in that terminal, or
  `tmux detach-client -t <client>` with a name from `tmux list-clients`;
- resize the other client to match;
- `set -g window-size largest` in your `tmux.conf`, so the bigger client wins
  instead of the most recent one.

Two related effects come from the same rule:

- **Attaching does not squeeze your session.** Hive votes only sizes it has
  actually measured, so opening the app never drops a session — or a terminal
  you have open elsewhere — to a placeholder.
- **A window can reflow the first time you switch to it.** With tmux's
  `aggressive-resize on`, a window is sized by the clients actually viewing it,
  so a window you haven't opened yet learns its size at the moment you select
  it.

## Scrollback and finding things in it

Attaching replays each window's **scrollback**, up to 2000 lines, and then its
visible screen — so a session that has been working for hours opens with what it
did before you got there, not just the last screenful. Scroll up as you would in
any terminal; the **Scroll to bottom** pill takes you back to the live tail.

2000 lines is tmux's own default `history-limit`, so unless you have raised that
in your `tmux.conf` this is the entire history tmux is keeping. If you have
raised it, the app replays the most recent 2000 lines and the rest stays
reachable in tmux's copy mode (`prefix + [`).

**Find in a window** with `⌘F` (`Ctrl+Shift+F` on Linux and Windows), or the
magnifier in the tab strip. It searches the window you are looking at, scrollback
included — `Enter` and `Shift+Enter` step through the matches, `Esc` closes the
bar. A plain `Ctrl+F` is left alone on purpose: it is readline's forward-char and
belongs to whatever is running in the pane.

Switching tabs re-runs the search against that window, because a match count only
ever describes one window's buffer.

## Putting the windows in the order you want

Drag a tab along the strip, or a window along its session in the sidebar, and it
lands in the gap the pointer is nearest. Both are the same order — a window
moved in one shows up moved in the other — because the order is tmux's own, not
a per-view arrangement. Every other client attached to that session sees the
move too, and tmux's window indices are renumbered afterwards so they stay
contiguous.

Only a session you are attached to can be reordered: moving a window is
something the attach does, so the windows listed under sessions you have not
opened are there to click, not to drag.

## Changing the text size

The **⋯** menu at the end of the tab strip carries **Decrease**, **Increase**,
and **Reset** for the terminal's text size, so a pane that is too small to read
is fixed where you are looking at it. It steps through the same five presets as
Settings ▸ Appearance ▸ Terminal — 12px to 18px — and the menu stays open, so
walking to the size you want is a run of clicks rather than a run of trips.

Both controls write the same setting, `appearance.terminal_font_size`, so a
nudge here applies to every open terminal at once and is still there next
launch. **Reset** goes back to Medium, the default.

New cell metrics mean a different number of cells fit the pane, so a change
re-votes the window size — with the same rule as above about who wins that
vote.

## Known rough edges

It is off by default for a reason:

- **Splits are not rendered separately.** A window shows its active pane, so a
  split you made elsewhere is only half visible.
- **A large burst of output can outrun the app's buffer**, which ends the stream
  and offers **Reconnect**. Reconnecting re-attaches and repaints from the
  current screen.
