import { describe, expect, it } from 'vitest'
import { itemContents } from '../itemClipboard'
import type { InboxItem } from '../../types/feed'

const baseItem: InboxItem = {
  id: 42, profileId: 'triage', sourceKind: 'github', sourceScope: 'colonyops/hive', externalId: 'pr-42', title: 'Add desktop shell', url: 'https://github.com/hay-kot/hive-desktop/pull/42',
  payload: { id: 'pr-42', kind: 'PR', repo: 'colonyops/hive', num: 42, author: 'hayden', branch: 'feat/desktop-ui-shell', body: 'First line\n\nSecond line' }, revision: 3, unread: true, lifecycle: 'active', firstSeenAt: 1, lastEventAt: 2,
}

describe('itemContents', () => {
  it('formats a self-contained header plus the raw markdown body', () => {
    expect(itemContents(baseItem)).toBe(
      'Add desktop shell\ncolonyops/hive #42 · https://github.com/hay-kot/hive-desktop/pull/42\n\nFirst line\n\nSecond line',
    )
  })

  it('drops the body block when the item has none', () => {
    const item = { ...baseItem, payload: { ...(baseItem.payload as object), body: '  ' } }
    expect(itemContents(item)).toBe('Add desktop shell\ncolonyops/hive #42 · https://github.com/hay-kot/hive-desktop/pull/42')
  })

  it('degrades to title and url for a payload without github metadata', () => {
    const item = { ...baseItem, payload: null }
    expect(itemContents(item)).toBe('Add desktop shell\nhttps://github.com/hay-kot/hive-desktop/pull/42')
  })
})
