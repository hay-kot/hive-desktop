import type { Component } from 'vue'

// Entry model for AppMenu, the shared dropdown-menu primitive. Menus are
// described as data (actions, separators, group labels) so every surface that
// hosts one — feed rows, the detail pane, future toolbars — renders the same
// chrome and only decides what the entries mean.
export interface MenuActionEntry {
  kind: 'action'
  id: string
  label: string
  /** Leading glyph as a resolved component (unplugin-icons import). */
  icon?: Component
  /** Leading glyph by AppIcon registry name — for icon names that arrive as data. */
  iconName?: string
  /** Identity hue for the glyph (e.g. configured-action type colors). */
  iconColor?: string
  /** Right-aligned shortcut hint, already formatted for display. */
  kbd?: string
  /**
   * Inert: the operation exists but has nowhere to go right now (a text size
   * already at the end of its ladder). It stays listed rather than
   * disappearing, so the menu's shape does not shift under the pointer.
   */
  disabled?: boolean
  testid?: string
}

export type MenuEntry =
  | MenuActionEntry
  | { kind: 'separator' }
  | { kind: 'label'; text: string }
