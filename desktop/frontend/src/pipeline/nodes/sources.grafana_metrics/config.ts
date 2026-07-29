// sources.grafana_metrics is a source node (0 in / 1 out): it runs a PromQL
// query against a connected Grafana stack. The source runs on the backend
// (internal/app/sources/grafana) — the frontend never executes it, only
// consumes the one msg it appends per poll under topic
// "source:<flowId>/<nodeId>" — so there is no runtime.ts here (role: 'source'
// means "backend-run", exactly like sources.github).

import IconActivity from '~icons/lucide/activity'

export const type = 'sources.grafana_metrics'
export const role = 'source' as const
// The inbox item sourceKind this node's items carry — see sources.github's
// config.ts for why this lives next to `type`.
export const sourceKind = 'grafana'

export interface Config {
  /**
   * The connected stack this source fetches as, as "grafana/<account>". A ref
   * and never a token: flows/ is dotfiles-managed, so an embedded token would
   * be a token in a git repo.
   */
  credential: string
  /** The uid of the Prometheus-compatible datasource to query. */
  datasource_uid: string
  /** A PromQL expression, e.g. "up" or "sum(rate(http_requests_total[5m]))". */
  expr: string
  /** The feed item's title. Defaults to the query when empty. */
  title?: string
}

// ── App-registry metadata ───────────────────────────────────────────────────

export const label = 'Grafana metrics source'
export const category = 'Sources' as const
export const glyph = IconActivity
// Blue — source nodes share the sources.github cap color.
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
