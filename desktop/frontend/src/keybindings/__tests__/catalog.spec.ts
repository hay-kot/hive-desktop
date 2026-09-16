import { describe, expect, it } from 'vitest'
import { commandCatalog, defaultCombosFor } from '../catalog'
import { comboFromEvent } from '../../composables/useKeybindings'

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
  // character. The chord a user means by "bigger" has to match whichever
  // spelling their layout produces.
  it('binds every spelling of the increase chord on macOS', () => {
    const increase = commandCatalog.find((c) => c.id === 'terminal.text-size-increase')
    const combos = defaultCombosFor(increase!, true)
    const presses = [
      new KeyboardEvent('keydown', { key: '=', metaKey: true }),
      new KeyboardEvent('keydown', { key: '+', metaKey: true, shiftKey: true }),
      new KeyboardEvent('keydown', { key: '+', metaKey: true }),
    ]

    for (const press of presses) {
      expect(combos, `${press.key} (shift: ${press.shiftKey})`).toContain(comboFromEvent(press))
    }
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
