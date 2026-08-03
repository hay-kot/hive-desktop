// Runs on the backend (internal/app/sources/posthog); role 'source' means no runtime.ts here.

import IconBug from '~icons/lucide/bug'

export const type = 'sources.posthog_errors'
export const role = 'source' as const
export const sourceKind = 'posthog'

export const STATUSES = ['active', 'resolved', 'suppressed', 'all'] as const
export const ORDERINGS = ['last_seen', 'first_seen', 'occurrences', 'users', 'sessions'] as const

export type Status = (typeof STATUSES)[number]
export type Ordering = (typeof ORDERINGS)[number]

export const MAX_LIMIT = 100

export interface Config {
  /**
   * A ref and never a token: flows/ is dotfiles-managed, so an embedded token
   * would be a token in a git repo.
   */
  credential: string
  status?: Status
  order_by?: Ordering
  date_from?: string
  limit?: number
  include_test_accounts?: boolean
}

export const label = 'PostHog error tracking source'
export const category = 'Sources' as const
export const glyph = IconBug
// Source nodes share the sources.github cap color.
export const accentToken = 'var(--color-node-blue)'
export const tint = 'var(--color-node-blue-tint)'

export const defaults: Config = {
  credential: '',
  status: 'active',
  order_by: 'last_seen',
  date_from: '-7d',
  limit: 25,
  include_test_accounts: false,
}

/** UX-only — Go's SaveFlow validator is authoritative. */
export function validate(config: Config): string[] {
  const errors: string[] = []
  const credential = (config.credential ?? '').trim()
  if (!credential) {
    errors.push('a source needs a connected PostHog project')
  } else if (!/^posthog\/[^/]+$/.test(credential)) {
    errors.push('credential must look like "posthog/<account>"')
  }
  if (config.status && !STATUSES.includes(config.status)) {
    errors.push(`status must be one of ${STATUSES.join(', ')}`)
  }
  if (config.order_by && !ORDERINGS.includes(config.order_by)) {
    errors.push(`order by must be one of ${ORDERINGS.join(', ')}`)
  }
  const limit = config.limit ?? 0
  if (limit < 0) {
    errors.push('limit must not be negative')
  } else if (limit > MAX_LIMIT) {
    errors.push(`limit caps at ${MAX_LIMIT}`)
  }
  return errors
}
