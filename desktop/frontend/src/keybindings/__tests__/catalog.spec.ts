import { describe, expect, it } from 'vitest'
import { commandCatalog, defaultCombosFor } from '../catalog'
import { comboFromEvent, terminalEscapeCombo } from '../../composables/useKeybindings'

// Where `mod` is Ctrl, Ctrl+Shift is the pane escape and terminalEscapeCombo
// drops the Shift before resolving, so a shifted default on an escapesPane
// command is unreachable from a pane there and lands on whichever command owns
// the unshifted spelling: Ctrl+Shift+W on `mod+shift+w` would close the window.
describe('commandCatalog defaults', () => {
  it('gives every escapesPane command unshifted defaults where mod is Ctrl', () => {
    for (const command of commandCatalog.filter((c) => c.escapesPane)) {
      for (const combo of defaultCombosFor(command, false)) {
        expect(combo.split('+'), `${command.id}: ${combo}`).not.toContain('shift')
      }
    }
  })

  // ⌘+ is Shift+= on most layouts, and `plus` is a named key, so the event
  // spells itself `mod+shift+plus` rather than folding the Shift into the
  // character.
  it.each([
    ['=', false],
    ['+', true],
    ['+', false],
  ])('binds the %s spelling (shift: %s) of the increase chord on macOS', (key, shiftKey) => {
    const increase = commandCatalog.find((c) => c.id === 'terminal.text-size-increase')!
    const press = new KeyboardEvent('keydown', { key, shiftKey, metaKey: true })

    expect(defaultCombosFor(increase, true)).toContain(comboFromEvent(press))
  })

  // `mod+_` and `mod+)` look like typos and are not: the pane escape drops the
  // Shift token and keeps the shifted character.
  it.each([
    ['terminal.text-size-increase', '+'],
    ['terminal.text-size-decrease', '_'],
    ['terminal.text-size-reset', ')'],
  ])('reaches %s from a pane where mod is Ctrl', (id, key) => {
    const command = commandCatalog.find((c) => c.id === id)!
    const press = new KeyboardEvent('keydown', { key, ctrlKey: true, shiftKey: true })

    expect(defaultCombosFor(command, false)).toContain(terminalEscapeCombo(press))
  })

  it.each([
    ['macOS', true],
    ['a Ctrl platform', false],
  ])('binds each default to one command on %s', (_, mac) => {
    const owners = new Map<string, string>()
    for (const command of commandCatalog) {
      for (const combo of defaultCombosFor(command, mac)) {
        expect(owners.get(combo), `${combo} already belongs to ${owners.get(combo)}`).toBeUndefined()
        owners.set(combo, command.id)
      }
    }
  })
})
