// Runs on the backend (internal/app/sources/grafana); role 'source' means no runtime.ts here.

import IconBell from '~icons/lucide/bell'

export const type = 'sources.grafana_alerts'
export const role = 'source' as const
export const sourceKind = 'grafana'

export interface Config {
  /**
   * A ref and never a token: flows/ is dotfiles-managed, so an embedded token
   * would be a token in a git repo.
   */
  credential: string
  /** Alertmanager label matchers the stack filters on before responding. */
  matchers: string[]
}

export const label = 'Grafana alerts source'
export const category = 'Sources' as const
export const glyph = IconBell
// Source nodes share the sources.github cap color.
export const accentToken = 'var(--color-node-blue)'
export const tint = 'var(--color-node-blue-tint)'

export const defaults: Config = {
  credential: '',
  matchers: [],
}

/** Alertmanager's matcher operators, longest first so "!=" is recognized
 *  before the "=" inside it. */
const MATCHER_OPERATORS = ['!=', '=~', '!~', '=']

/** The leftmost operator is the one that separates the label name from the
 *  value — a value may contain an operator itself ("path=/a!=b"). Mirrors
 *  Go's validateMatcher. */
function matcherLabel(matcher: string): string | null {
  for (let i = 0; i < matcher.length; i++) {
    for (const op of MATCHER_OPERATORS) {
      if (matcher.startsWith(op, i)) return matcher.slice(0, i).trim()
    }
  }
  return null
}

/** UX-only — Go's SaveFlow validator is authoritative. */
export function validate(config: Config): string[] {
  const errors: string[] = []
  const credential = (config.credential ?? '').trim()
  if (!credential) {
    errors.push('a source needs a connected Grafana stack')
  } else if (!/^grafana\/[^/]+$/.test(credential)) {
    errors.push('credential must look like "grafana/<account>"')
  }
  for (const matcher of config.matchers ?? []) {
    const label = matcherLabel(matcher.trim())
    if (label === null) {
      errors.push(`matcher "${matcher}" needs one of =, !=, =~, !~`)
    } else if (!label) {
      errors.push(`matcher "${matcher}" has no label name`)
    }
  }
  return errors
}
