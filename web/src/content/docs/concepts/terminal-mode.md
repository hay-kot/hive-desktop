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

## First paint is the screen, not the history

Attaching draws each window's **visible screen**. Scrollback is not replayed, so
the app starts with an empty buffer even when the session has hours of history
behind it; read it in tmux's own copy mode (`prefix + [`) for now.

## Known rough edges

It is off by default for a reason:

- **Splits are not rendered separately.** A window shows its active pane, so a
  split you made elsewhere is only half visible.
- **A large burst of output can outrun the app's buffer**, which ends the stream
  and offers **Reconnect**. Reconnecting re-attaches and repaints from the
  current screen.
