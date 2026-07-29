import type { ITheme } from '@xterm/xterm'

// The app's palette lives in CSS custom properties that the theme switcher
// swaps on <html> (src/styles/main.css). xterm.js paints to a canvas and can
// not read them, so the few colours it needs are sampled here and re-applied
// whenever the theme changes. ANSI colours keep xterm's defaults — the app has
// no opinion on them.
const FALLBACK: ITheme = {
  background: '#181a1f',
  foreground: '#fafafa',
  cursor: '#f59e0b',
  cursorAccent: '#181a1f',
  selectionBackground: 'rgba(245, 158, 11, 0.35)',
}

function cssColor(name: string, fallback: string): string {
  if (typeof getComputedStyle !== 'function' || typeof document === 'undefined') return fallback
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

/** The xterm palette for the theme currently applied to the document. */
export function xtermTheme(): ITheme {
  const background = cssColor('--hv-app', FALLBACK.background!)
  return {
    background,
    foreground: cssColor('--hv-text', FALLBACK.foreground!),
    cursor: cssColor('--hv-accent', FALLBACK.cursor!),
    cursorAccent: background,
    // --hv-selection is tuned for a 10% overlay on app chrome; a terminal
    // selection has to stay legible over arbitrary output, so it is stronger.
    selectionBackground: FALLBACK.selectionBackground,
  }
}
