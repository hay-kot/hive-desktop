import type { ITheme } from '@xterm/xterm'

// The app's palette lives in CSS custom properties that the theme switcher
// swaps on <html> (src/styles/main.css). xterm.js paints to a canvas and can
// not read them, so every colour it needs — chrome and the 16-colour ANSI
// set — is sampled here and re-applied whenever the theme changes.
const ANSI_VARS = {
  black: '--hv-term-black',
  red: '--hv-term-red',
  green: '--hv-term-green',
  yellow: '--hv-term-yellow',
  blue: '--hv-term-blue',
  magenta: '--hv-term-magenta',
  cyan: '--hv-term-cyan',
  white: '--hv-term-white',
  brightBlack: '--hv-term-bright-black',
  brightRed: '--hv-term-bright-red',
  brightGreen: '--hv-term-bright-green',
  brightYellow: '--hv-term-bright-yellow',
  brightBlue: '--hv-term-bright-blue',
  brightMagenta: '--hv-term-bright-magenta',
  brightCyan: '--hv-term-bright-cyan',
  brightWhite: '--hv-term-bright-white',
} as const

// The dark theme's values, for contexts with no styled document.
const FALLBACK: ITheme = {
  background: '#101318',
  foreground: '#e9edf4',
  cursor: '#f5b23f',
  cursorAccent: '#101318',
  selectionBackground: 'rgba(245, 178, 63, 0.35)',
  black: '#1b2029',
  red: '#ef7183',
  green: '#8fd67f',
  yellow: '#e6b45a',
  blue: '#6ba8f0',
  magenta: '#b98ef0',
  cyan: '#5fc8de',
  white: '#b8c1cf',
  brightBlack: '#55606f',
  brightRed: '#ff8b9b',
  brightGreen: '#a6e695',
  brightYellow: '#f5c973',
  brightBlue: '#86bcff',
  brightMagenta: '#cba6ff',
  brightCyan: '#7adcf0',
  brightWhite: '#e9edf4',
}

function cssColor(style: CSSStyleDeclaration | null, name: string, fallback: string): string {
  if (!style) return fallback
  return style.getPropertyValue(name).trim() || fallback
}

/** The xterm palette for the theme currently applied to the document. */
export function xtermTheme(): ITheme {
  const style =
    typeof getComputedStyle === 'function' && typeof document !== 'undefined'
      ? getComputedStyle(document.documentElement)
      : null
  const background = cssColor(style, '--hv-app', FALLBACK.background!)
  const theme: ITheme = {
    background,
    foreground: cssColor(style, '--hv-text', FALLBACK.foreground!),
    cursor: cssColor(style, '--hv-accent', FALLBACK.cursor!),
    cursorAccent: background,
    // --hv-selection is tuned for a 10% overlay on app chrome; a terminal
    // selection has to stay legible over arbitrary output, so it is stronger.
    selectionBackground: FALLBACK.selectionBackground,
  }
  for (const key of Object.keys(ANSI_VARS) as (keyof typeof ANSI_VARS)[]) {
    theme[key] = cssColor(style, ANSI_VARS[key], FALLBACK[key]!)
  }
  return theme
}
