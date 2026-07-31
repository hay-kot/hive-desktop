import '../assets/fonts/caskaydia-mono-nerd.css'

// The face every terminal pane falls back to, and the name the OS font scan
// reports it as. Agent TUIs draw powerline and devicon glyphs no system font
// covers, so a pane that renders without this shows tofu where the icons are.
export const TERMINAL_FONT = 'CaskaydiaMono Nerd Font'

const FALLBACKS = "ui-monospace, 'SF Mono', Menlo, Consolas, monospace"

/**
 * The `fontFamily` xterm is given. A chosen system family leads and the bundled
 * face backs it, so a family the OS reports but the webview cannot resolve — or
 * one uninstalled since it was picked — degrades to the shipped font rather
 * than to whatever `monospace` happens to be.
 */
export function terminalFontStack(family: string): string {
  const chosen = family.trim()
  if (!chosen || chosen === TERMINAL_FONT) return `${quoted(TERMINAL_FONT)}, ${FALLBACKS}`
  return `${quoted(chosen)}, ${quoted(TERMINAL_FONT)}, ${FALLBACKS}`
}

// A family name reaches xterm inside a canvas font shorthand, and an unquoted
// name that starts with a digit or holds a stray token is a parse error there —
// which leaves ctx.font at its previous value rather than throwing.
function quoted(family: string): string {
  return family.includes("'") ? `"${family}"` : `'${family}'`
}

// Kept per (stack, size, weight pair) because document.fonts.load re-resolves
// on every call — 12ms to 47ms measured, even for a face already resident — and
// every pane awaits it before it may so much as ask for a terminal.
const faceLoads = new Map<string, Promise<void>>()

/**
 * Make every face a pane can draw with resident before it opens.
 *
 * xterm measures its cell when a Terminal opens and never re-measures when a
 * face arrives later, and an atlas renderer caches the glyphs it rasterised
 * from whatever was resident (ADR 0038) — so both weights belong here, not just
 * the normal one, and the italics of each with them.
 */
export function loadTerminalFaces(
  family: string,
  px: number,
  weight: number,
  weightBold: number,
): Promise<void> {
  const stack = terminalFontStack(family)
  const key = `${stack}|${px}|${weight}|${weightBold}`
  const loaded = faceLoads.get(key)
  if (loaded) return loaded

  const specs = [weight, weightBold].flatMap((value) => [
    `${value} ${px}px ${stack}`,
    `italic ${value} ${px}px ${stack}`,
  ])
  const pending = Promise.all(
    specs.map((spec) => document.fonts?.load(spec).catch(() => {})),
  ).then(() => {})
  faceLoads.set(key, pending)
  return pending
}

export function resetTerminalFacesForTests(): void {
  faceLoads.clear()
}
