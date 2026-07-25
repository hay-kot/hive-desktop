// feed is a terminal node (1 in / 0 out): the arriving message creates an
// unread inbox-item membership claim. The node *is* the feed — its identity is
// the flow-qualified node id "<flowId>/<nodeId>", the durable claim key the
// sidebar reads back. role/sink here are the engine's single source
// of truth for "how a feed node becomes a commit output" — runGraph imports
// `sink`/`unread` directly rather than re-encoding the mapping.

import IconRss from '~icons/lucide/rss'
import { isFeedIcon } from '../../../lib/feedIcons'
import * as notifyNode from '../notify/config'

export const type = 'feed'
export const role = 'output' as const

// A feed node's identity is still its node id; icon and description are purely
// cosmetic sidebar presentation. `icon` picks the glyph shown in the tree
// (one of the scoped keys in lib/feedIcons), and `description` is the hover
// tooltip context (especially useful for LLM-generated feeds). `notify` turns
// the feed into one that interrupts. All optional; mirrors Go's
// flow.FeedConfig.
export interface Config {
  icon?: string
  description?: string
  notify?: Notify
}

/**
 * A feed's notification config. Its presence is the on switch, so a quiet feed
 * carries no notify keys at all. It is the notify *node's* config verbatim —
 * both deliver through the same executor, so they share one shape, one
 * validator, and one severity vocabulary.
 */
export type Notify = notifyNode.Config

/** The template a newly enabled feed starts with — the item's own title. */
export const defaultNotifyTitle = '{{ .Payload.title }}'

/** Longest description the editor accepts — mirrors Go's feedDescriptionMaxLen. */
export const descriptionMaxLen = 500

// Template caps and the severity vocabulary come from the notify node: a
// feed's notifications are the same notifications.
export const notifyTitleMaxLen = notifyNode.titleMaxLen
export const notifyBodyMaxLen = notifyNode.bodyMaxLen
export const severities = notifyNode.severities

/**
 * The sink a notifying feed raises alongside its membership claim. It targets
 * the feed's own id: the feed is what asked to interrupt, so Commit resolves
 * this back to the same node's notify config.
 */

/** Newly observed inbox items land unread until the user reads them. */
export const unread = true

/** The feed's durable key is the flow-qualified node id. */

// ── App-registry metadata ───────────────────────────────────────────────────

export const label = 'Feed'
export const category = 'Destinations' as const
export const glyph = IconRss
// Green — the mockup doesn't show a Feed node explicitly; reuses the
// existing --color-feeds hue (already this app's "feed" meaning color).
export const accentToken = 'var(--color-node-green)'
export const tint = 'var(--color-node-green-tint)'
/** Terminal node — 0 outputs. */
export const outputs = 0

export const defaults: Config = {}

/** UX-only live validation, matching Go's FeedConfig.Validate. */
export function validate(config: Config): string[] {
  const errors: string[] = []
  if (config.icon && !isFeedIcon(config.icon)) {
    errors.push(`Icon "${config.icon}" is not a supported feed icon.`)
  }
  if ((config.description?.length ?? 0) > descriptionMaxLen) {
    errors.push(`Description must be at most ${descriptionMaxLen} characters.`)
  }
  // A notifying feed's block is validated exactly as a notify node's is.
  if (config.notify) errors.push(...notifyNode.validate(config.notify))
  return errors
}
