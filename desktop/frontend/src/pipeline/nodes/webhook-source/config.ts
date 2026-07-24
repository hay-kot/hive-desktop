// webhook-source is a source node (0 in / 1 out): the push-driven counterpart
// to github-source. The node declares a path on the desktop's local webhook
// listener; JSON POSTed to http://127.0.0.1:<port>/hooks/<path> is ingested by
// Go and appended to the event log under topic "source:<flowId>/<nodeId>" —
// the frontend never executes the source, it only consumes the msgs the
// backend already appended (role: 'source', no runtime.ts), exactly like
// github-source. See internal/desktop/pipeline/webhook_source.go.

import IconWebhook from '~icons/lucide/webhook'

import { isFeedIcon } from '../../../lib/feedIcons'

export const type = 'webhook-source'
export const role = 'source' as const
// The inbox item sourceKind this node's items carry — see github-source's
// config.ts for why this lives next to `type`.
export const sourceKind = 'webhook'

export interface Config {
  /**
   * Endpoint path under /hooks/: one or more slug segments separated by "/"
   * (lowercase letters, digits, "-", "_"), e.g. "ci-alerts" or "ci/deploys".
   */
  path: string
  /**
   * Optional shared secret; when set, senders must present the same value in
   * the X-Hive-Secret request header.
   */
  secret?: string
  /**
   * Optional glyph feed rows render for this node's items, from the shared
   * feed icon set. Empty means the default webhook glyph.
   */
  icon?: string
}

// ── App-registry metadata ───────────────────────────────────────────────────

export const label = 'Webhook source'
export const category = 'Sources' as const
export const glyph = IconWebhook
// Blue — source nodes share the github-source cap color.
export const accentToken = 'var(--color-node-blue)'
export const tint = 'var(--color-node-blue-tint)'

export const defaults: Config = {
  path: '',
}

const SEGMENT = /^[a-z0-9][a-z0-9_-]*$/

/** UX-only — Go's SaveFlow validator is authoritative. */
export function validate(config: Config): string[] {
  const errors: string[] = []
  const path = config.path ?? ''
  if (!path.trim()) {
    errors.push('a webhook source requires a path')
  } else if (path.length > 128) {
    errors.push('path caps at 128 characters')
  } else if (!path.split('/').every((segment) => SEGMENT.test(segment))) {
    errors.push('path must be lowercase slug segments, e.g. "ci-alerts" or "ci/deploys"')
  }
  const secret = config.secret ?? ''
  if (secret.length > 128) errors.push('secret caps at 128 characters')
  else if (secret && !/^[!-~]+$/.test(secret)) errors.push('secret must be printable ASCII without spaces')
  if (config.icon && !isFeedIcon(config.icon)) errors.push(`"${config.icon}" is not a supported feed icon`)
  return errors
}
