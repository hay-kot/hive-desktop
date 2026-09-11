// Geometry over a tmux window layout: where each pane's emulator goes inside
// the window's grid, and where the dividers between them are. Everything here
// is in cells; the renderer multiplies by the cell it measured.

import type { PaneLayout, WindowState } from './terminalClient'

export interface PaneRect {
  paneId: string
  x: number
  y: number
  width: number
  height: number
}

/**
 * A border between two sibling cells. `at` is the border's column (axis x)
 * or row (axis y), `from`/`to` its extent along the other axis, and `before`
 * names a pane inside the cell ahead of it — the pane a resize is addressed
 * to, since tmux moves the divider on the far side of the cell it is told to
 * resize. `origin` and `extent` are that cell's start and size along the axis,
 * `limit` the most it may grow to before the cell after it disappears.
 */
export interface PaneDivider {
  axis: 'x' | 'y'
  at: number
  from: number
  to: number
  before: string
  after: string
  origin: number
  extent: number
  limit: number
}

/** The pane the keyboard goes to in a window: tmux's active pane, or its only one. */
export function activePaneOf<T extends { paneId: string }>(tab: { activePane: string; panes: T[] }): T | undefined {
  return tab.panes.find((pane) => pane.paneId === tab.activePane) ?? tab.panes[0]
}

/** Every pane cell, left to right and top to bottom. */
export function layoutLeaves(layout: PaneLayout | null | undefined): PaneLayout[] {
  if (!layout) return []
  if (layout.paneId) return [layout]
  return (layout.cells ?? []).flatMap(layoutLeaves)
}

/** The pane ids of a window, in layout order; the active pane alone when tmux has reported no layout yet. */
export function windowPanes(state: Pick<WindowState, 'layout' | 'activePane'>): string[] {
  const leaves = layoutLeaves(state.layout).map((leaf) => leaf.paneId as string)
  if (leaves.length) return leaves
  return state.activePane ? [state.activePane] : []
}

/**
 * The grid each pane renders at: its cell in the layout, or the whole window
 * for a zoomed active pane. A pane zoom is hiding keeps its layout size, which
 * is what tmux keeps its screen at. A window with no layout gives its active
 * pane the whole grid.
 */
export function paneGrids(state: Pick<WindowState, 'layout' | 'activePane' | 'zoomed' | 'width' | 'height'>): Map<string, { cols: number; rows: number }> {
  const grids = new Map<string, { cols: number; rows: number }>()
  const leaves = layoutLeaves(state.layout)
  if (!leaves.length) {
    if (state.activePane) grids.set(state.activePane, { cols: state.width, rows: state.height })
    return grids
  }
  for (const leaf of leaves) {
    const zoomedIn = state.zoomed && leaf.paneId === state.activePane
    grids.set(leaf.paneId as string, zoomedIn
      ? { cols: state.width, rows: state.height }
      : { cols: leaf.width, rows: leaf.height })
  }
  return grids
}

/**
 * Where each pane on screen is drawn, in cells from the window's top left.
 * Under zoom only the active pane is on screen, over the whole window.
 */
export function visiblePaneRects(state: Pick<WindowState, 'layout' | 'activePane' | 'zoomed' | 'width' | 'height'>): PaneRect[] {
  const leaves = layoutLeaves(state.layout)
  if (!leaves.length) {
    return state.activePane ? [{ paneId: state.activePane, x: 0, y: 0, width: state.width, height: state.height }] : []
  }
  if (state.zoomed) {
    return leaves.some((leaf) => leaf.paneId === state.activePane)
      ? [{ paneId: state.activePane, x: 0, y: 0, width: state.width, height: state.height }]
      : []
  }
  return leaves.map((leaf) => ({ paneId: leaf.paneId as string, x: leaf.x, y: leaf.y, width: leaf.width, height: leaf.height }))
}

/** The dividers of an unzoomed layout; a zoomed window has none on screen. */
export function paneDividers(state: Pick<WindowState, 'layout' | 'zoomed'>): PaneDivider[] {
  if (!state.layout || state.zoomed) return []
  const dividers: PaneDivider[] = []
  collectDividers(state.layout, dividers)
  return dividers
}

function collectDividers(node: PaneLayout, out: PaneDivider[]): void {
  const cells = node.cells ?? []
  if (!node.split || cells.length < 2) return
  const horizontal = node.split === 'leftright'
  for (let i = 0; i < cells.length - 1; i++) {
    const before = cells[i]
    const after = cells[i + 1]
    const extent = horizontal ? before.width : before.height
    const next = horizontal ? after.width : after.height
    out.push({
      axis: horizontal ? 'x' : 'y',
      at: horizontal ? before.x + before.width : before.y + before.height,
      from: horizontal ? node.y : node.x,
      to: horizontal ? node.y + node.height : node.x + node.width,
      before: firstPane(before),
      after: firstPane(after),
      origin: horizontal ? before.x : before.y,
      extent,
      limit: extent + next - 1,
    })
  }
  for (const cell of cells) collectDividers(cell, out)
}

function firstPane(node: PaneLayout): string {
  return layoutLeaves(node)[0]?.paneId ?? ''
}

/**
 * The size the cell before a divider ends up at when the divider is dragged to
 * `position` (a column or row): at least one cell, and never so far that the
 * cell after it vanishes.
 */
export function draggedExtent(divider: PaneDivider, position: number): number {
  const wanted = Math.round(position) - divider.origin
  return Math.min(Math.max(wanted, 1), divider.limit)
}

/** Whether a divider borders the pane, on either side. */
export function dividerTouches(divider: PaneDivider, layout: PaneLayout | null, paneId: string): boolean {
  if (!layout || !paneId) return false
  const owner = cellOwning(layout, divider)
  if (!owner) return false
  const cells = owner.cells ?? []
  const index = cells.findIndex((cell) => firstPane(cell) === divider.before)
  if (index < 0) return false
  return [cells[index], cells[index + 1]].some((cell) => cell && layoutLeaves(cell).some((leaf) => leaf.paneId === paneId))
}

function cellOwning(node: PaneLayout, divider: PaneDivider): PaneLayout | null {
  const cells = node.cells ?? []
  if (node.split && cells.length >= 2) {
    const horizontal = node.split === 'leftright'
    if ((horizontal ? 'x' : 'y') === divider.axis && cells.some((cell) => firstPane(cell) === divider.before)) return node
  }
  for (const cell of cells) {
    const found = cellOwning(cell, divider)
    if (found) return found
  }
  return null
}
