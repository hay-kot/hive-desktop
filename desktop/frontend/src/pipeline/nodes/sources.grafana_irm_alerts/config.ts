// Runs on the backend (internal/app/sources/grafana); role 'source' means no runtime.ts here.

import IconSiren from '~icons/lucide/siren'

export const type = 'sources.grafana_irm_alerts'
export const role = 'source' as const
export const sourceKind = 'grafana'

export interface Config {
  /**
   * A ref and never a token: flows/ is dotfiles-managed, so an embedded token
   * would be a token in a git repo.
   */
  credential: string
  /** An IRM integration id, the unit a squad's upstream routes deliver to. */
  integration: string
  /** An IRM team id. */
  team: string
}

export const label = 'Grafana IRM alerts source'
export const category = 'Sources' as const
export const glyph = IconSiren
// Source nodes share the sources.github cap color.
export const accentToken = 'var(--color-node-blue)'
export const tint = 'var(--color-node-blue-tint)'

export const defaults: Config = {
  credential: '',
  integration: '',
  team: '',
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
  return errors
}
