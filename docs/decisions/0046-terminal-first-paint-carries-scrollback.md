# 0046 — First paint carries bounded scrollback and restores the cursor

- **Status:** accepted
- **Date:** 2026-07-30
- Supersedes decision 5 of [ADR 0036](0036-terminal-transport.md)

## Context

First paint captured a pane's visible screen (`capture-pane -pe -J`), trimmed
its trailing blank rows, and wrote that. A tab therefore opened on whatever
happened to be on screen: everything the session had done before the attach was
unreachable, even though tmux was still holding it. For the sessions this
product exists for — an agent that has been working for hours — that is most of
what there is to read, and there was no way to search it either.

Trimming had a second cost that only shows on the alternate screen. An emulator
pins its viewport to the last rows it was written, so a screen written short
seats the pane's row 0 partway down the viewport. Live output from a
cursor-addressed program then lands that many rows off for the rest of the
attach, and the further the trim, the worse the drift.

ADR 0036 deferred all three — full history, alternate-screen handling, cursor
restore — and made buffer overflow fatal partly because a partial resync would
need exactly them.

## Decision

1. **A first paint is three commands per pane, not one:** the cursor
   (`display-message -p "#{cursor_y} #{cursor_x}"`), then history
   (`capture-pane -pe -J -S -2000 -E -1`), then the visible screen
   (`capture-pane -pe -S 0`). They are replayed as history rows, the screen at
   exactly the window's height, and a cursor-position escape. Nothing homes or
   clears first: writing history-plus-a-full-screen scrolls the history out of
   the viewport by itself, which leaves the screen occupying the viewport
   exactly and the history above it as scrollback.

2. **The bound is 2000 lines, which is tmux's own default `history-limit`.** On
   an unconfigured tmux it is therefore the whole history rather than a bound
   anyone runs into, and it costs a few hundred KB per window against an 8 MiB
   broker. Unbounded replay was rejected: `history-limit` is user configuration
   with no ceiling, so it would make attach latency and broker pressure a
   property of someone's `.tmux.conf`. The frontend's xterm `scrollback` (5000)
   stays comfortably above it.

3. **The screen is written at exactly the window height, and `-J` is used on
   the history only.** The height is what keeps the emulator's viewport and the
   pane's grid the same rows, which is what makes the replay safe under an
   alternate-screen program. `-J` rejoins a wrapped scrollback row to the
   logical line it came from so searching and selecting read as one line, but a
   joined *screen* row would re-wrap into more rows than it was captured from
   and break that height.

4. **The emulator stays in its normal buffer even when the pane's program is on
   the alternate screen.** tmux keeps a pane's scrollback in the normal buffer
   while the alternate screen is up, so both are available and both are
   painted; putting the emulator into *its* alternate buffer would throw away
   the scrollback this whole decision exists to deliver.

5. **The cursor is read first, and after the paint gate's mark rather than
   before it.** Output tmux produces between the read and the captures is
   absent from the cursor but present in the replay that follows the snapshot,
   which redraws it and carries the cursor where it belongs. Read before the
   mark, that output would be discarded as already-snapshotted and nothing
   would ever correct the position.

6. **Search is `@xterm/addon-search`, one addon per window, one bar over the
   active tab.** The bar floats over the pane rather than sitting above it: a
   bar in the flex column would shrink the pane's box, and that box is what
   this client votes tmux's window size from — opening a find bar would reflow
   the session for every client attached to it. Switching tabs re-runs the
   query against the new buffer rather than carrying a count that was only true
   of the old one. It opens on Cmd+F (macOS) or Ctrl+Shift+F, intercepted at
   xterm's own key handler because a focused terminal owns every key; a bare
   Ctrl+F stays readline's forward-char and reaches the pane.

## Consequences

- **Attach costs one capture of tmux history per window** rather than one
  screen. Measured at roughly 100 KB per 2000 lines of coloured output, so a
  pooled attach of several sessions is single-digit MB through a broker bounded
  at 8 MiB per session, and a few tens of milliseconds of xterm parsing per
  window.
- **A fresh window now paints its prompt at the top of a full-height screen**
  instead of a trimmed one — what tmux itself shows, and no longer a screenful
  of blanks above the prompt, because the cursor is restored rather than left
  wherever the replay ended.
- **Overflow recovery is unblocked but not built.** ADR 0036's decision 4 still
  stands as written: overflow kills the client and a fresh attach is the
  resync. The scrollback and cursor restore a non-destructive partial resync
  would need now exist; using them is a separate decision.
- **The bound is a constant, not a setting.** A session whose `history-limit`
  is set far above 2000 replays only the last 2000 lines on attach. Making it
  configurable is a settings-surface decision nobody has asked for yet.
- Still deferred: pane splits. A split window's `pane_height` is smaller than
  the grid the emulator renders, so the alignment above holds for the unsplit
  windows v1 renders and no further.
