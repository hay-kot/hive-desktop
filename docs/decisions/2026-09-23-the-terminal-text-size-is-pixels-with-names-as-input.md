# The terminal text size is pixels, with names as input

- **Status:** accepted
- **Date:** 2026-09-23

## Context

ADR the-zoom-chords-step-the-terminal-text-size-instead-of-magnifying-the-webview
put <kbd>⌘+</kbd>/<kbd>⌘-</kbd> on the text size and had them walk the five
presets, on the reasoning that each preset has known-good cell metrics and that
a wider range is more presets.

That ladder is four steps wide and stops at 18px. A chord whose point is
repetition stops answering after the third press, which is the complaint the
chords were bound to answer.

Nothing in the renderer needs the size to be one of five values: xterm measures
the cell from the resident face at whatever size it is given, and the atlas
rasterises per size (ADR terminal-atlas-renderer). But the names are still the
readable way to write this by hand, and `settings.yaml` is a file people edit.

## Decision

**The size the app runs on is a pixel count** — stepped by two, clamped to
8–64, where the bounds are legibility at the bottom and a usable grid at the
top. The frontend deals only in pixels.

**`appearance.terminal_font_size` and its `HIVE_DESKTOP_*` override accept
either spelling, permanently.** `internal/app/settings/terminalfontsize.go`
holds the one name table — small 12, medium 13, large 14, xl 16, xxl 18 —
resolves a name or a number to pixels, and clamps. This is not a migration and
there is no rewriting pass: a name is valid input for good.

**A file keeps the spelling it already uses.** `SetTerminalFontSize` takes
pixels and asks `TerminalFontSizeValue` what to write: a file spelled with a
name keeps names while the size has one, and goes numeric when the ladder steps
off them. It then stays numeric, because a size with no name is what the user
asked for and drifting back to a name would rewrite a field they did not touch.

**Settings ▸ Terminal ▸ Font size is a stepper** (`SettingsStepper.vue`), since
a segmented control would need a button per rung.

## Consequences

- **The names are input, not state.** Nothing in the UI offers them and the
  frontend cannot spell them, which is what keeps one table in one language.
- **The bounds are stated twice** — `settings.MinTerminalFontSizePx` /
  `MaxTerminalFontSizePx` govern the file, and the frontend constants hold the
  ladder and the stepper. The Go pair is the one that decides what persists.
- **An unreadable size reads as the default** rather than failing the load. One
  mistyped appearance field is not worth refusing to start, and the default is
  visibly wrong to whoever mistyped it.
- Canvas reader typography keeps its own preset names in the frontend
  (`useCanvasTypography.ts`). The terminal size diverges because a chord steps
  it; a picker-only setting has no reason to leave its names behind.
- The step is not proportional, so a press moves less at 60px than at 10px. If
  that reads as sluggish the answer is a proportional step, not named rungs.
