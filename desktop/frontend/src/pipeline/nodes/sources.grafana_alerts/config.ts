// sources.grafana_alerts is a source node (0 in / 1 out): it emits one item per
// firing Grafana-managed alert on a connected stack. The source runs on the
// backend (internal/app/sources/grafana) — the frontend never executes it, only
// consumes the msgs it appends per poll — so there is no runtime.ts here
// (role: 'source' means "backend-run").

import IconBell from '~icons/lucide/bell'

export const type = 'sources.grafana_alerts'
export const role = 'source' as const
// The inbox item sourceKind this node's items carry — shared with
// sources.grafana_metrics, since both fetch as the same provider.
export const sourceKind = 'grafana'

export interface Config {
  /**
   * The connected stack this source fetches as, as "grafana/<account>". A ref
   * and never a token. Alerts are not scoped to a datasource, so this is the
   * only field.
   */
  credential: string
}

// ── App-registry metadata ───────────────────────────────────────────────────

export const label = 'Grafana alerts source'
export const category = 'Sources' as const
export const glyph = IconBell
// Blue — source nodes share the sources.github cap color.
export const accentToken = 'var(--color-node-blue)'
export const tint = 'var(--color-node-blue-tint)'

export const defaults: Config = {
  credential: '',
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
