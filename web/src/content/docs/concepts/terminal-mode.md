---
title: Terminal mode
description: Attach to a session's tmux windows inside the app — what it does, and which of its rough edges are tmux's rules rather than bugs.
group: Concepts
order: 1
---

Terminal mode attaches to the **tmux session** behind a Hive session and lists
its windows in the sidebar, so the agent you launched from the feed is readable
without leaving the app.

## What it needs

Two things have to be true for it to work, and the mode says which one is
missing when it isn't:

- **tmux 3.2 or newer on your `PATH`.** Hive drives tmux's control mode, which
  is where the protocol it needs landed.
- **The local HTTP server is on** — `http: {enabled: true}`, the default. The
  terminal's transport rides that server; with no server there is no terminal.

The title bar carries an **Inbox | Code** switch. The sidebar lists your
sessions, and picking one attaches to it.

## The scratch terminal

The first section of the sidebar, above your repositories, is **Terminals**, and
your own tabs are listed in it the way a repository lists its sessions. Behind it
is a tmux session like any other, except that it belongs to no Hive session, no
repository and no agent — it is there to poke at something.

The `+` on that heading is how you use it: with nothing running it opens a shell
in your home directory, and every tab after that opens there too. Tabs are the
only multiplicity there is — there is one scratch terminal, and it keeps as many
tabs as you open. Click a tab to go to it, drag it to reorder, double-click to
rename it, and fold the whole section away from its heading.

It outlives the app the way every tmux session does — close Hive, come back, and
the tabs are still there, still running whatever you left. tmux calls the session
`Scratch`, so `tmux attach -t Scratch` reaches it from a terminal.

Its ⋯ menu carries **Start terminal** and **Kill terminal…** and nothing else:
rename, recycle, delete and session details all act on a Hive session, and there
isn't one behind this section. Killing it closes every tab and stops what is running
in them; starting it again opens an empty one. **Prune** never touches it.

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

**Find in a window** with `⌘F` (`Ctrl+Shift+F` on Linux and Windows). It
searches the window you are looking at, scrollback included — `Enter` and `Shift+Enter` step through the matches, `Esc` closes the
bar. A plain `Ctrl+F` is left alone on purpose: it is readline's forward-char and
belongs to whatever is running in the pane.

Switching windows re-runs the search against that window, because a match count
only ever describes one window's buffer.

## Driving the sidebar from the keyboard

The session tree is a keyboard surface. `↑` and `↓` — or `j` and `k` — walk it,
and landing on a row does exactly what clicking it does: a session attaches, a
window is selected. There is nothing to confirm.

A running session **is** its windows, so the arrows walk those and skip its own
row — stopping there first would have changed nothing, since it is already
attached. What is left is a simple rule: you stop on a session row exactly when
there is no terminal behind it yet, and `Enter` on that row starts one. (A
session whose windows are not listed keeps its row either way, so nothing
becomes unreachable.)

What arrowing deliberately does *not* do is put the cursor in the terminal.
Walking past a session is not the same as sitting down to work in one, and a
pane that took focus on the way past would send your next `↓` to tmux instead of
the sidebar. `Enter` is how you go in — as is clicking, which keeps the mouse
behaving the way it always has.

`/` puts the cursor in the filter at the top of the sidebar and narrows the tree
to what matches: a session name, its slug, or a repository — a repository match
keeps everything under it. Collapsed repositories open for as long as a filter
is on, so nothing hides behind one. `Esc` clears the filter, and a second `Esc`
leaves the field; `↓` and `Enter` leave it without clearing, which is how you
narrow the list and then walk what is left. Inside a pane `/` is just a slash,
so reach the tree first.

Filtering only changes what the sidebar draws. The session you are attached to
stays attached and on screen even when the filter hides its row, and clearing
the filter puts the row back where it was.

`⌘←` takes you back to the tree from inside a pane, and `⌘→` puts you back in
the terminal. `⌘←` is one of the chords terminal mode takes away from tmux: it
has to work while a pane holds every other key, so it is the only way back out
that does not need the mouse. If the sidebar is collapsed, `⌘←` reopens it.

`⌘1` through `⌘9` go straight to a window of the session you are attached to,
counting down its list in the sidebar — so `⌘3` is the third window under it,
whatever tmux numbered that window. These work from inside a pane too, which is
the point: the window you are leaving is the one holding the keyboard. A session
with fewer windows than the digit ignores the chord.

All of these are rebindable in Settings ▸ Keyboard, which is also where to
change them on Linux and Windows — there `mod` is Control, so `Ctrl+←` is
readline's backward-word and `Ctrl+2` through `Ctrl+7` are control characters
you may want back in the pane. The arrow keys inside the tree are the tree's own
and are not rebindable.

The bar under the session list carries the essentials, and its last hint follows
your focus — it offers the way into the terminal while you are in the tree, and
the way back out once you are in a pane.

## Putting the windows in the order you want

Drag a window along its session in the sidebar and it lands in the gap the
pointer is nearest. The order is tmux's own, not a per-view arrangement, so
every other client attached to that session sees the move too, and tmux's window
indices are renumbered afterwards so they stay contiguous.

Only a session you are attached to can be reordered: moving a window is
something the attach does, so the windows listed under sessions you have not
opened are there to click, not to drag.

## Changing the text size

Settings ▸ Terminal steps the terminal's text through five presets,
12px to 18px. It writes `appearance.terminal_font_size`, so a change applies to
every open terminal at once and is still there next launch.

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
