import { afterEach, describe, expect, it } from 'vitest'
import { xtermTheme } from '../terminalTheme'

describe('xtermTheme', () => {
  afterEach(() => {
    document.documentElement.removeAttribute('style')
  })

  it('samples the palette applied to the document', () => {
    const root = document.documentElement.style
    root.setProperty('--hv-app', '#101010')
    root.setProperty('--hv-text', '#eeeeee')
    root.setProperty('--hv-accent', '#ff8800')
    root.setProperty('--hv-term-red', '#cc241d')
    root.setProperty('--hv-term-bright-white', '#ebdbb2')

    const theme = xtermTheme()
    expect(theme.background).toBe('#101010')
    expect(theme.foreground).toBe('#eeeeee')
    expect(theme.cursor).toBe('#ff8800')
    expect(theme.cursorAccent).toBe('#101010')
    expect(theme.red).toBe('#cc241d')
    expect(theme.brightWhite).toBe('#ebdbb2')
  })

  it('falls back to the dark palette for variables the document does not define', () => {
    const theme = xtermTheme()
    expect(theme.background).toBe('#101318')
    expect(theme.foreground).toBe('#e9edf4')
    expect(theme.blue).toBe('#6ba8f0')
    expect(theme.brightBlack).toBe('#55606f')
  })
})
