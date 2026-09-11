---
icon: lucide/square-terminal
description: View and control Hive tmux sessions in the Code area.
---

# Terminal mode

Open **Code** with <kbd>g</kbd> then <kbd>c</kbd>. Select a session to attach to its tmux windows.

## Requirements

Hive needs tmux 3.2 or newer for Code, Chats, and quick terminals.

```sh
brew install tmux
```

Hive searches your login shell's `PATH` and common package manager locations. Set `paths.tmux` in [Settings](../configuration/settings.md#advanced-configuration) if the binary is elsewhere.

The local HTTP server must also be enabled. It is on by default.

## Sessions

Code lists active Hive sessions and their tmux windows. Closing Hive leaves those sessions running. You can attach to the same session from another terminal with `tmux attach`.

If a session has no running terminal, select **Start session**. Hive creates its configured windows and starts the agent. This starts a new agent process and does not resume the previous terminal process.

**Kill terminal** stops the tmux session and its processes while keeping the checkout and Hive session record. **Recycle** and **Delete** also change or remove the checkout.

## Scratch terminals

The **Terminals** section holds a scratch tmux session for shells that are not tied to a repository. Use its `+` button or <kbd>⌘T</kbd> to add tabs.

Scratch tabs can be selected, renamed, reordered, and closed. The session keeps running when Hive closes. **Kill terminal** stops every scratch tab.

## Working in Code

- Select a window from the sidebar to attach to it.
- Use <kbd>⌘1</kbd> through <kbd>⌘9</kbd> to switch windows.
- Use <kbd>⌘←</kbd> to focus the sidebar and <kbd>⌘→</kbd> to return to the terminal.
- Press <kbd>/</kbd> while the sidebar is focused to filter sessions.
- Press <kbd>⌘F</kbd> in a terminal to search its recent scrollback.
- Drag windows in the sidebar to reorder them.

Press <kbd>?</kbd> for the current shortcut list. Shortcuts can be changed under **Settings ▸ Keyboard**.

Actions can add commands to session and window menus. Quick terminals can open tools such as `lazygit` in the active checkout. See [Actions](../inbox/actions.md).

## Panes

A window shows every tmux pane where tmux lays it out, including panes split from another tmux client. The pane with the keyboard is tmux's active pane, so a click in a pane also selects it in tmux.

- <kbd>⌘D</kbd> splits the active pane to the right and <kbd>⌘⇧D</kbd> splits it downward. The new pane opens in the active pane's directory.
- <kbd>⌘⌥←</kbd>, <kbd>⌘⌥→</kbd>, <kbd>⌘⌥↑</kbd>, and <kbd>⌘⌥↓</kbd> move between panes.
- Drag the border between two panes to resize them.
- <kbd>⌘⇧↩</kbd> zooms the active pane to fill the window, and again to restore the layout. A `zoomed` badge shows while a pane is zoomed.
- <kbd>⌘⇧W</kbd> closes the active pane. Closing the last pane closes the window. Hive asks first when the pane is running something.

On Linux, use Control+Shift in place of Command, and Control+Alt with the arrow keys.

## Appearance

**Settings ▸ Terminal** controls the terminal font, size, weight, line height, letter spacing, visible windows, and status bar.

## Shared tmux sizing

All clients attached to a tmux session share one grid size. If the terminal does not fill the available pane, another attached client may be setting the size.

Detach or resize the other client, or add this to `tmux.conf`:

```text
set -g window-size largest
```

## Current limits

- Hive loads up to 2,000 lines of scrollback.
- A large output burst can skip intermediate lines while the view resynchronizes.
