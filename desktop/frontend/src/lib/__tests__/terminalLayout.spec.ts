import { describe, expect, it } from 'vitest'
import type { PaneLayout } from '../terminalClient'
import {
  dividerTouches,
  draggedExtent,
  layoutLeaves,
  paneDividers,
  paneGrids,
  visiblePaneRects,
  windowPanes,
} from '../terminalLayout'

// Recorded from tmux 3.7b: split-window -h, then -v on the right pane.
//   95e4,120x40,0,0{60x40,0,0,0,59x40,61,0[59x20,61,0,1,59x19,61,21,2]}
const nested: PaneLayout = {
  split: 'leftright', x: 0, y: 0, width: 120, height: 40,
  cells: [
    { paneId: '%0', x: 0, y: 0, width: 60, height: 40 },
    {
      split: 'topbottom', x: 61, y: 0, width: 59, height: 40,
      cells: [
        { paneId: '%1', x: 61, y: 0, width: 59, height: 20 },
        { paneId: '%2', x: 61, y: 21, width: 59, height: 19 },
      ],
    },
  ],
}

const single: PaneLayout = { paneId: '%7', x: 0, y: 0, width: 120, height: 40 }

describe('terminalLayout', () => {
  it('lists leaves left to right, then top to bottom', () => {
    expect(layoutLeaves(nested).map((leaf) => leaf.paneId)).toEqual(['%0', '%1', '%2'])
    expect(layoutLeaves(null)).toEqual([])
  })

  it('names the active pane alone when tmux has reported no layout yet', () => {
    expect(windowPanes({ layout: null, activePane: '%3' })).toEqual(['%3'])
    expect(windowPanes({ layout: null, activePane: '' })).toEqual([])
    expect(windowPanes({ layout: nested, activePane: '%1' })).toEqual(['%0', '%1', '%2'])
  })

  it('gives each pane its cell, and a zoomed pane the whole window', () => {
    const unzoomed = paneGrids({ layout: nested, activePane: '%1', zoomed: false, width: 120, height: 40 })
    expect(unzoomed.get('%0')).toEqual({ cols: 60, rows: 40 })
    expect(unzoomed.get('%2')).toEqual({ cols: 59, rows: 19 })

    const zoomed = paneGrids({ layout: nested, activePane: '%1', zoomed: true, width: 120, height: 40 })
    expect(zoomed.get('%1')).toEqual({ cols: 120, rows: 40 })
    // The panes zoom is hiding keep the size tmux keeps their screens at.
    expect(zoomed.get('%2')).toEqual({ cols: 59, rows: 19 })

    const unreported = paneGrids({ layout: null, activePane: '%3', zoomed: false, width: 80, height: 24 })
    expect(unreported.get('%3')).toEqual({ cols: 80, rows: 24 })
  })

  it('places every pane unzoomed and only the active one zoomed', () => {
    expect(visiblePaneRects({ layout: nested, activePane: '%1', zoomed: false, width: 120, height: 40 })).toEqual([
      { paneId: '%0', x: 0, y: 0, width: 60, height: 40 },
      { paneId: '%1', x: 61, y: 0, width: 59, height: 20 },
      { paneId: '%2', x: 61, y: 21, width: 59, height: 19 },
    ])
    expect(visiblePaneRects({ layout: nested, activePane: '%2', zoomed: true, width: 120, height: 40 })).toEqual([
      { paneId: '%2', x: 0, y: 0, width: 120, height: 40 },
    ])
    expect(visiblePaneRects({ layout: single, activePane: '%7', zoomed: false, width: 120, height: 40 })).toEqual([
      { paneId: '%7', x: 0, y: 0, width: 120, height: 40 },
    ])
  })

  it('finds the border between every pair of siblings, addressed to the cell before it', () => {
    const dividers = paneDividers({ layout: nested, zoomed: false })
    expect(dividers).toEqual([
      // The outer split's border is column 60, the full height of the window;
      // %0 is the cell before it and may grow to 118 before %1's cell is gone.
      { axis: 'x', at: 60, from: 0, to: 40, before: '%0', after: '%1', origin: 0, extent: 60, limit: 118 },
      // The inner split's border is row 20, spanning the right column only.
      { axis: 'y', at: 20, from: 61, to: 120, before: '%1', after: '%2', origin: 0, extent: 20, limit: 38 },
    ])
    expect(paneDividers({ layout: nested, zoomed: true })).toEqual([])
    expect(paneDividers({ layout: single, zoomed: false })).toEqual([])
  })

  it('clamps a dragged divider to one cell either side', () => {
    const [outer] = paneDividers({ layout: nested, zoomed: false })
    expect(draggedExtent(outer, 30.4)).toBe(30)
    expect(draggedExtent(outer, 0)).toBe(1)
    expect(draggedExtent(outer, 500)).toBe(118)
  })

  it('knows which borders the active pane touches', () => {
    const [outer, inner] = paneDividers({ layout: nested, zoomed: false })
    expect(dividerTouches(outer, nested, '%0')).toBe(true)
    expect(dividerTouches(outer, nested, '%2')).toBe(true)
    expect(dividerTouches(inner, nested, '%0')).toBe(false)
    expect(dividerTouches(inner, nested, '%2')).toBe(true)
    expect(dividerTouches(inner, nested, '')).toBe(false)
  })
})
