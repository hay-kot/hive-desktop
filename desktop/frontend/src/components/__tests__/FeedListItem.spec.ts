import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import FeedListItem from '../FeedListItem.vue'
import type { InboxItem } from '../../types/feed'

const baseItem: InboxItem = {
  id: 42, profileId: 'triage', sourceKind: 'github', sourceScope: 'colonyops/hive', externalId: 'pr-42', title: 'Add desktop shell', url: 'https://github.com/hay-kot/hive-desktop/pull/42',
  payload: { id: 'pr-42', kind: 'PR', repo: 'colonyops/hive', num: 42, author: 'hayden', branch: 'feat/desktop-ui-shell', body: 'Body' }, revision: 3, unread: true, lifecycle: 'active', firstSeenAt: 1, lastEventAt: Date.now(),
}
function mountItem(overrides: Partial<InboxItem> = {}, selected = false) { return mount(FeedListItem, { props: { item: { ...baseItem, ...overrides }, selected } }) }

describe('FeedListItem', () => {
  it('decodes GitHub payload type and metadata through the presentation adapter', () => {
    const wrapper = mountItem()
    expect(wrapper.find('[data-testid="source-badge"]').attributes('data-source')).toBe('github')
    expect(wrapper.find('[data-testid="type-pill"]').classes()).toContain('type-pill-pr')
    expect(wrapper.find('[data-testid="type-pill"]').text()).toBe('Pull Request')
    expect(wrapper.find('[data-testid="item-snippet"]').text()).toContain('hayden — Body')
  })

  it('renders issue styling and an unread indicator only when inbox state is unread', () => {
    const issue = mountItem({ unread: false, payload: { ...baseItem.payload as object, kind: 'Issue' } })
    expect(issue.find('[data-testid="type-pill"]').classes()).toContain('type-pill-issue')
    expect(issue.find('[data-testid="unread-dot"]').exists()).toBe(false)
  })

  it('uses archive reason only in archived presentation and keeps selection styling', () => {
    const wrapper = mount(FeedListItem, { props: { item: { ...baseItem, archivedReason: 'manual' }, archived: true, selected: true } })
    expect(wrapper.get('[data-testid="archive-reason"]').text()).toBe('manual')
    expect(wrapper.get('[data-testid="feed-item"]').classes()).toContain('selected')
  })

  it('emits selection intent on click and keyboard activation', async () => {
    const wrapper = mountItem()
    await wrapper.get('[data-testid="feed-item"]').trigger('click')
    await wrapper.get('[data-testid="feed-item"]').trigger('keydown.enter')
    expect(wrapper.emitted('select')).toHaveLength(2)
  })

  it('offers archive and open-in-browser from the hover pill without selecting the row', async () => {
    const wrapper = mountItem()
    await wrapper.get('[data-testid="row-archive"]').trigger('click')
    await wrapper.get('[data-testid="row-open"]').trigger('click')
    expect(wrapper.emitted('toggle-archive')).toHaveLength(1)
    expect(wrapper.emitted('open-browser')).toHaveLength(1)
    expect(wrapper.emitted('select')).toBeUndefined()
  })

  it('swaps the archive slot for stop-ignoring in trash presentation', () => {
    const wrapper = mount(FeedListItem, { props: { item: { ...baseItem, ignoredAt: 5 }, trash: true, selected: false } })
    expect(wrapper.find('[data-testid="row-archive"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="row-restore"]').attributes('aria-label')).toBe('Stop ignoring')
  })

  it('opens the item menu from the kebab and from right-click and relays its intents', async () => {
    const wrapper = mountItem()
    await wrapper.get('[data-testid="row-menu-toggle"]').trigger('click')
    await wrapper.get('[data-testid="menu-copy-link"]').trigger('click')
    expect(wrapper.emitted('copy-link')).toHaveLength(1)
    expect(wrapper.find('[data-testid="row-menu"]').exists()).toBe(false)

    await wrapper.get('[data-testid="feed-item"]').trigger('contextmenu')
    expect(wrapper.find('[data-testid="row-menu"]').exists()).toBe(true)
    await wrapper.get('[data-testid="menu-toggle-read"]').trigger('click')
    expect(wrapper.emitted('set-unread')).toEqual([[false]])
    expect(wrapper.emitted('select')).toBeUndefined()
  })
})
