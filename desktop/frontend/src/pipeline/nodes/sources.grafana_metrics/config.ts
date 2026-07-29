// Runs on the backend (internal/app/sources/grafana); role 'source' means no runtime.ts here.

import IconActivity from '~icons/lucide/activity'

export const type = 'sources.grafana_metrics'
export const role = 'source' as const
export const sourceKind = 'grafana'

export interface Config {
  /**
   * A ref and never a token: flows/ is dotfiles-managed, so an embedded token
   * would be a token in a git repo.
   */
  credential: string
  datasource_uid: string
  expr: string
  title?: string
}

export const label = 'Grafana metrics source'
export const category = 'Sources' as const
export const glyph = IconActivity
// Source nodes share the sources.github cap color.
export const accentToken = 'var(--color-node-blue)'
export const tint = 'var(--color-node-blue-tint)'

export const defaults: Config = {
  credential: '',
  datasource_uid: '',
  expr: '',
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
  if (!(config.datasource_uid ?? '').trim()) errors.push('a datasource uid is required')
  if (!(config.expr ?? '').trim()) errors.push('a PromQL query is required')
  return errors
}
