import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { themes } from '../composables/useTheme'

// The theme registry (useTheme.ts) and the palette blocks (main.css) can only
// drift apart silently: a registered id without a block renders as the :root
// palette, and a block without an id is dead weight. Parity is asserted here
// instead of discovered in the picker.

const css = readFileSync(join(process.cwd(), 'src/styles/main.css'), 'utf8')

const blocks = new Map<string, Set<string>>()
for (const match of css.matchAll(/\[data-theme="([a-z-]+)"\]\s*\{([^}]*)\}/g)) {
  const tokens = new Set([...match[2].matchAll(/--hv-[a-z0-9-]+/g)].map((m) => m[0]))
  blocks.set(match[1], tokens)
}

describe('theme CSS', () => {
  it('defines a block for every registered theme and registers every block', () => {
    expect([...blocks.keys()].sort()).toEqual([...themes].sort())
  })

  it('defines the full per-theme token set in every block', () => {
    // The light block is the canonical per-theme set: unlike dark it defines
    // nothing that themes inherit from :root (kind-*, severity-*, node hues).
    const required = [...blocks.get('light')!]
    expect(required.length).toBeGreaterThan(40)

    for (const [theme, tokens] of blocks) {
      // Midnight predates the per-theme ANSI palettes and shares :root's by
      // design (it is the same hue family as dark).
      const exempt = theme === 'midnight' ? /^--hv-term-/ : /^$/
      const missing = required.filter((token) => !tokens.has(token) && !exempt.test(token))
      expect(missing, `theme "${theme}"`).toEqual([])
    }
  })
})
