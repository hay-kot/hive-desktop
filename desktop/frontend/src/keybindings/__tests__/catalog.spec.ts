import { describe, expect, it } from 'vitest'
import { commandCatalog, defaultCombosFor } from '../catalog'

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
