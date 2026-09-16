# Cmd+ and Cmd- step the terminal font size instead of magnifying the webview

- **Status:** accepted
- **Date:** 2026-09-16

## Context

Wails installs a platform application menu when the app sets none, and its View
menu spends <kbd>⌘+</kbd>/<kbd>⌘-</kbd>/<kbd>⌘0</kbd> on the webview's own zoom.
That zoom is `setMagnification:` on macOS: it scales the rendered webview rather
than re-laying-out the page, so the window keeps its CSS width and starts
scrolling. Zooming into a terminal therefore produced a horizontal scrollbar and
resampled glyphs instead of a reflowed grid — the one thing a terminal can
actually do in response to bigger text, which is re-measure its cell, re-fit and
let tmux rewrap, never happened.

A menu key equivalent is consumed by the platform before the webview sees the
keydown, so the frontend keymap could not claim those chords while the roles
held their accelerators.

Text size was five named presets (small…xxl) because the only way to change it
was a picker, and a picker is better with named options than with a spinner.

## Decision

**The app installs the platform's default menu with the three zoom accelerators
removed**, and binds those chords to the terminal's text size instead
(`terminal.text-size-increase` / `-decrease` / `-reset`). The items stay in the
View menu, clickable: magnification remains the only way to scale the chrome
outside a terminal.

**The size is whole pixels, stepped by two, clamped to 8–40** — a ladder, not a
preset list, because a chord that stops after five rungs is not a zoom. The
`appearance.terminal_font_size` setting carries the number as digits and still
reads the five preset names as the sizes they meant, so an existing
settings.yaml loads unchanged.

The commands run in a `terminals` context: terminal mode, the Agents area, or
the pop-up panel over either. What they act on is the emulator, so where one is
drawn is what gates them, not which view is on screen.

## Consequences

- **Nothing outside a terminal responds to <kbd>⌘+</kbd> any more.** The Inbox,
  Flows and Settings have no text-size setting of their own, so scaling them
  means the View menu. If that becomes a real complaint the answer is a UI
  scale setting, not the accelerator back.
- **Settings ▸ Terminal ▸ Font size is a stepper** (`SettingsStepper.vue`), and
  the preset names are gone from the UI, the docs and the settings template.
- This supersedes the fourth point of ADR the-sidebar-tree-is-the-only-window-list:
  text size is no longer Settings-only. It still has no pane chrome — the ladder
  is a keybinding, which is what that point was protecting the pane from.
