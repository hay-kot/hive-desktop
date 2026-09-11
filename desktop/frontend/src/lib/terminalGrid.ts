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

/** How many whole cells fit a box. */
export function proposeGrid(box: { width: number; height: number }, cell: CellSize): GridSize | null {
  if (!box.width || !box.height || !cell.width || !cell.height) return null
  const cols = Math.floor(box.width / cell.width)
  const rows = Math.floor(box.height / cell.height)
  if (cols < 1 || rows < 1) return null
  return { cols, rows }
}
