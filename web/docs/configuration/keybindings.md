---
icon: lucide/keyboard
description: Find, use, and change keyboard shortcuts in Hive Desktop.
---

# Keyboard shortcuts

Press <kbd>?</kbd> to see the current shortcuts. **Settings ▸ Keyboard** shows the same list, reports conflicts, and lets you change each binding.

The command palette at <kbd>⌘K</kbd> can run commands with or without a shortcut.

## Common shortcuts

| Action | Shortcut |
| --- | --- |
| Open command palette | <kbd>⌘K</kbd> |
| Open shortcut list | <kbd>?</kbd> |
| Search the current view | <kbd>/</kbd> or <kbd>⌘F</kbd> |
| Open Settings | <kbd>⌘,</kbd> or <kbd>g</kbd> then <kbd>s</kbd> |
| Go to Inbox | <kbd>g</kbd> then <kbd>i</kbd> |
| Go to Code | <kbd>g</kbd> then <kbd>c</kbd> |
| Go to Chats | <kbd>g</kbd> then <kbd>a</kbd> |
| Refresh a feed | <kbd>r</kbd> |
| Move through feed items | <kbd>j</kbd>/<kbd>k</kbd> |
| Open an item | <kbd>o</kbd> or <kbd>Enter</kbd> |
| Archive an item | <kbd>e</kbd> |

Use <kbd>?</kbd> for the full list because it always matches the installed version.

## Rebinding

You can change shortcuts in the app or in `settings.yaml`:

```yaml
keybindings:
  feed.next: [j, arrowdown]
  launcher.lazygit: [alt+g]
  tasks.toggle: ["g t"]
  palette.toggle: []
```

The map only needs entries you want to change. An empty list removes a binding.

Use lower-case key names and join modifiers with `+`, such as `mod+shift+t`. `mod` means Command on macOS and Control on other platforms. Separate keys with a space for a sequence such as `g i`.

## Terminal shortcuts

A focused terminal sends most keys to the running program. Global app shortcuts use Command on macOS or Control+Shift on other platforms so they can still leave the terminal pane.

Check **Settings ▸ Keyboard** before assigning a Control shortcut that your shell or terminal program already uses.
