import '../assets/fonts/jetbrains-mono-nerd.css'

// The face every terminal pane measures its cell from, and the stack it falls
// back through. Agent TUIs draw powerline and devicon glyphs no system font
// covers, so a pane that renders without this shows tofu where the icons are.
export const TERMINAL_FONT = "'JetBrainsMono Nerd Font'"
export const TERMINAL_FONT_STACK = `${TERMINAL_FONT}, 'IBM Plex Mono', ui-monospace, monospace`

// Kept per size because document.fonts.load re-resolves on every call — 12ms
// to 47ms measured, even for a face already resident — and every pane awaits
// it before it may so much as ask for a terminal.
const faceLoads = new Map<number, Promise<void>>()

// xterm measures its cell when a Terminal opens and never re-measures when a
// face arrives later, and an atlas renderer caches the glyphs it rasterised
// from whatever was resident — so bold has to be here too, not just regular.
export function loadTerminalFaces(px: number): Promise<void> {
  const loaded = faceLoads.get(px)
  if (loaded) return loaded
  const pending = Promise.all([`${px}px`, `bold ${px}px`].map(
    (font) => document.fonts?.load(`${font} ${TERMINAL_FONT}`).catch(() => {}),
  )).then(() => {})
  faceLoads.set(px, pending)
  return pending
}

export function resetTerminalFacesForTests(): void {
  faceLoads.clear()
}
