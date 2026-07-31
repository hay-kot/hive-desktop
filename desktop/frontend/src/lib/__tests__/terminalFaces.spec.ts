import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  loadTerminalFaces,
  resetTerminalFacesForTests,
  TERMINAL_FONT,
  terminalFontStack,
} from '../terminalFaces'

describe('terminalFontStack', () => {
  it('falls back to the bundled face when nothing is chosen', () => {
    expect(terminalFontStack('')).toBe(terminalFontStack(TERMINAL_FONT))
    expect(terminalFontStack('')).toContain(`'${TERMINAL_FONT}'`)
  })

  it('treats whitespace as nothing chosen', () => {
    expect(terminalFontStack('   ')).toBe(terminalFontStack(''))
  })

  // A family the OS reports but the webview cannot resolve — or one
  // uninstalled since it was picked — has to land on the shipped Nerd Font
  // rather than on whatever `monospace` happens to be, or a TUI's icons become
  // tofu.
  it('backs a chosen family with the bundled face, not just a generic', () => {
    const stack = terminalFontStack('Menlo')

    expect(stack.indexOf("'Menlo'")).toBeLessThan(stack.indexOf(`'${TERMINAL_FONT}'`))
    expect(stack).toContain('monospace')
  })

  it('does not repeat the bundled face when it is the chosen family', () => {
    expect(terminalFontStack(TERMINAL_FONT).match(new RegExp(TERMINAL_FONT, 'g'))).toHaveLength(1)
  })

  // The stack reaches xterm inside a canvas font shorthand, where an unquoted
  // family starting with a digit is a parse error — and a failed parse leaves
  // ctx.font at its previous value rather than throwing, so the terminal would
  // silently render in the wrong font.
  it('quotes family names that would not parse bare', () => {
    expect(terminalFontStack('3270Medium Nerd Font')).toContain("'3270Medium Nerd Font'")
    expect(terminalFontStack("Fred's Mono")).toContain('"Fred\'s Mono"')
  })
})

describe('loadTerminalFaces', () => {
  beforeEach(() => {
    resetTerminalFacesForTests()
    Object.defineProperty(document, 'fonts', {
      configurable: true,
      value: { load: vi.fn().mockResolvedValue([]) },
    })
  })

  // xterm measures its cell once on open and the atlas caches what was
  // resident, so a bold or italic face arriving late stays wrong. ADR 0038.
  it('loads both weights and the italic of each', async () => {
    await loadTerminalFaces('', 13, 300, 700)

    const stack = terminalFontStack('')
    expect((document.fonts.load as ReturnType<typeof vi.fn>).mock.calls.map(([spec]) => spec)).toEqual([
      `300 13px ${stack}`,
      `italic 300 13px ${stack}`,
      `700 13px ${stack}`,
      `italic 700 13px ${stack}`,
    ])
  })

  // document.fonts.load re-resolves on every call — tens of milliseconds even
  // for a resident face — and every pane awaits this before it may ask for a
  // terminal.
  it('resolves a repeat request from cache', async () => {
    await loadTerminalFaces('', 13, 300, 700)
    await loadTerminalFaces('', 13, 300, 700)

    expect(document.fonts.load).toHaveBeenCalledTimes(4)
  })

  it('reloads when the family, size, or a weight changes', async () => {
    await loadTerminalFaces('', 13, 300, 700)
    await loadTerminalFaces('Menlo', 13, 300, 700)
    await loadTerminalFaces('', 16, 300, 700)
    await loadTerminalFaces('', 13, 400, 700)

    expect(document.fonts.load).toHaveBeenCalledTimes(16)
  })

  // A font that never loads must not stop a pane from opening.
  it('resolves when a face fails to load', async () => {
    Object.defineProperty(document, 'fonts', {
      configurable: true,
      value: { load: vi.fn().mockRejectedValue(new Error('no such face')) },
    })

    await expect(loadTerminalFaces('Absent Mono', 13, 300, 700)).resolves.toBeUndefined()
  })
})
