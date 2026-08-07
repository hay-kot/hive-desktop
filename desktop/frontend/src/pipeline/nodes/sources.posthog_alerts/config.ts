// Runs on the backend (internal/app/sources/posthog); role 'source' means no runtime.ts here.

import IconBellRing from '~icons/lucide/bell-ring'

export const type = 'sources.posthog_alerts'
export const role = 'source' as const
export const sourceKind = 'posthog'

export interface Config {
  /**
   * A ref and never a token: flows/ is dotfiles-managed, so an embedded token
   * would be a token in a git repo.
   */
  credential: string
  firing_only?: boolean
}

export const label = 'PostHog insight alerts source'
export const category = 'Sources' as const
export const glyph = IconBellRing
// Source nodes share the sources.github cap color.
export const accentToken = 'var(--color-node-blue)'
export const tint = 'var(--color-node-blue-tint)'

export const defaults: Config = {
  credential: '',
  firing_only: false,
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
  return errors
}
