// The cell an xterm renders at, read the way @xterm/addon-fit reads it. The
// fit addon measures its own terminal's host, which is one pane's box; the
// window's size vote needs the same cell against the whole window's box, and
// pane placement needs it to turn layout cells into pixels.

import type { Terminal } from '@xterm/xterm'

export interface CellSize {
  width: number
  height: number
}

export interface GridSize {
  cols: number
  rows: number
}

interface TerminalCore {
  _renderService?: { dimensions?: { css?: { cell?: { width: number; height: number } } } }
  viewport?: { scrollBarWidth?: number }
}

function core(term: Terminal): TerminalCore | undefined {
  return (term as unknown as { _core?: TerminalCore })._core
}

/** The rendered cell of an opened terminal, or null before it has measured one. */
export function terminalCellSize(term: Terminal): CellSize | null {
  const cell = core(term)?._renderService?.dimensions?.css?.cell
  if (!cell?.width || !cell.height) return null
  return { width: cell.width, height: cell.height }
}

/** The width the terminal's viewport scrollbar takes from its box; 0 with no scrollback or an overlay scrollbar. */
export function terminalScrollbarWidth(term: Terminal): number {
  if (term.options.scrollback === 0) return 0
  return core(term)?.viewport?.scrollBarWidth || 0
}

/** How many cells fit a box, the arithmetic the fit addon does over its own host. */
export function proposeGrid(box: { width: number; height: number }, cell: CellSize, scrollbar: number): GridSize | null {
  if (!box.width || !box.height || !cell.width || !cell.height) return null
  const cols = Math.floor((box.width - scrollbar) / cell.width)
  const rows = Math.floor(box.height / cell.height)
  if (cols < 1 || rows < 1) return null
  return { cols, rows }
}
