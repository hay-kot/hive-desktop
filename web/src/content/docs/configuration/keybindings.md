---
title: Keyboard shortcuts
description: The default bindings, the command ids behind them, and how to rebind a command or a launcher in settings.yaml.
group: Configuration
order: 1
---

Every shortcut is a **command** with a stable id. **Settings ▸ Keyboard** lists
them all with their current bindings, reports collisions, and writes the
`keybindings:` key in `settings.yaml`. Press <kbd>?</kbd> anywhere to see the
same list, and <kbd>⌘K</kbd> to open the command palette, where every command
is reachable by name whether or not it has a key.

## Rebinding

```yaml
# settings.yaml
keybindings:
  feed.next: [j, arrowdown]        # replace a command's bindings
  feed.mark-workspace-read: [X]    # bind one that ships unbound
  launcher.lazygit: [alt+g]        # a launcher from actions.yml
  tasks.toggle: ["g t"]            # a sequence: press g, then t
  palette.toggle: []               # unbind
```

The map is sparse: a command you do not name keeps its defaults. Each value
is a list of combos, and each combo is a canonical string:

- keys are lower-case: `j`, `enter`, `arrowdown`, `escape`, `/`;
- modifiers are `mod`, `shift`, `alt`, and `ctrl`, joined with `+`, as in
  `mod+shift+t`. **`mod` is Command on macOS and Control everywhere else**,
  which is why the defaults below say `mod`;
- a **sequence** is two or more combos separated by a space, pressed in
  order: `g i`. A sequence never fires over a focused terminal pane, inside a
  text field, or under an overlay.

Two commands cannot share a combo. The editor reports the collision, and a
binding that conflicts is not applied.

## What a terminal pane keeps

In Code, a focused terminal pane keeps every key it can use, so bare
<kbd>j</kbd> reaches the shell and `ctrl+k` stays readline's. Only Command
chords (and Control+Shift on platforms without Command) escape the pane, plus
a few commands that must work from inside it: the window jumps, closing the
window, the pop-up terminal, and the way back to the session tree. On Linux,
where `mod` is Control, check that a chord you add is not something you want
the pane to see.

## Defaults

The **context** column says where a bare binding fires. `global` is
everywhere; `feed` needs the inbox with a feed focused; `terminal` needs Code
with a session attached; `agents` is the Chats area.

### General

| Command | Default | Context |
| --- | --- | --- |
| `palette.toggle` | `mod+k` | global |
| `palette.keys` | `?` | global |
| `view.focus-search` | `/`, `mod+f` | global |
| `settings.open` | `mod+,`, `g s` | global |
| `session.new` | `mod+n` | global |
| `terminal.popup.toggle` | `` mod+` `` | global |
| `history.back` | `mod+[` | global |
| `history.forward` | `mod+]` | global |
| `report.open` | `mod+shift+b` | global |
| `window.hide` | unbound | global |

### View

| Command | Default | Context |
| --- | --- | --- |
| `view.go-inbox` | `g i` | global |
| `view.go-code` | `g c` | global |
| `view.go-chats` | `g a` | global |
| `tasks.toggle` | `mod+shift+t`, `g t` | global |

### Feeds

| Command | Default | Context |
| --- | --- | --- |
| `feed.next` | `j`, `arrowdown` | feed |
| `feed.prev` | `k`, `arrowup` | feed |
| `feed.open-in-browser` | `o`, `enter` | feed |
| `feed.toggle-archive` | `e` | feed |
| `feed.mark-unread` | `shift+u` | feed |
| `feed.toggle-unread` | `u` | feed |
| `feed.toggle-preview` | `p` | feed |
| `feed.refresh` | `r` | feed |
| `feed.mark-all-read` | `shift+a` | feed |
| `feed.mark-workspace-read` | unbound | feed |

`feed.mark-workspace-read` marks every feed in the workspace read and has no
undo, so it ships unbound on purpose.

### Code

| Command | Default | Context |
| --- | --- | --- |
| `terminal.focus-sidebar` | `mod+arrowleft` | terminal |
| `terminal.focus-pane` | `mod+arrowright` | terminal |
| `terminal.new-window` | `mod+t` | terminal |
| `terminal.close-window` | `mod+w` | terminal |
| `terminal.next-window` | `mod+}` | terminal |
| `terminal.prev-window` | `mod+{` | terminal |
| `terminal.select-window-1` … `-9` | `mod+1` … `mod+9` | terminal |

The numbered jumps go to the nth window of the attached session as listed in
the sidebar, whatever tmux numbered it. A tenth is deliberately unreachable.

### Chats

| Command | Default | Context |
| --- | --- | --- |
| `agents.focus-sidebar` | `mod+shift+arrowleft` | agents |
| `agents.focus-pane` | `mod+shift+arrowright` | agents |

### Launchers

Each entry under `launchers:` in `actions.yml` is a command named
`launcher.<id>`, unbound until you bind it. A launcher without a `cwd` fires
only while a terminal session is open; see
[Actions](/docs/concepts/actions#launchers).

## Keys that are not commands

A few keys belong to the widget that owns focus and are not rebindable: the
arrows and <kbd>j</kbd>/<kbd>k</kbd> inside the Code session tree, the arrows
inside the command palette, <kbd>Esc</kbd> to leave a filter field, and
<kbd>⌘F</kbd> inside a terminal window, which searches that window's
scrollback.
