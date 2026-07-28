import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DetailPane from '../DetailPane.vue'
import type { ActionView } from '../../types/action'
import type { InboxItem } from '../../types/feed'

const item: InboxItem = {
  id: 42, profileId: 'triage', sourceKind: 'github', sourceScope: 'colonyops/hive', externalId: 'pr-42', title: 'Add desktop shell', url: 'https://github.com/hay-kot/hive-desktop/pull/42',
  payload: { id: 'pr-42', kind: 'PR', repo: 'colonyops/hive', num: 42, author: 'octocat', branch: 'feat/desktop-ui-shell', body: 'Body' }, revision: 1, unread: true, lifecycle: 'active', firstSeenAt: 1, lastEventAt: Date.now(),
}
const actions: ActionView[] = [{ id: 'summarize', label: 'Summarize', type: 'launch-session', showInDetail: true, requiresSessionInput: false }]
const payload = (patch: Record<string, unknown>) => ({ ...(item.payload as Record<string, unknown>), ...patch })

describe('DetailPane', () => {
  it('renders source, GitHub context, action cards, and branch metadata from an inbox item', () => {
    const wrapper = mount(DetailPane, { props: { item, actions } })
    expect(wrapper.get('[data-testid="source-badge"]').attributes('data-source')).toBe('github')
    expect(wrapper.text()).toContain('colonyops/hive #42')
    expect(wrapper.findAll('[data-testid="action-card"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="action-footer-branch"]').text()).toBe('feat/desktop-ui-shell')
  })

  it('renders GitHub-flavored markdown and routes body links through open-url', async () => {
    const wrapper = mount(DetailPane, { props: { item: { ...item, payload: payload({ body: '## Steps\n\n- [ ] first\n\n[docs](https://example.com)' }) }, actions } })
    expect(wrapper.get('[data-testid="detail-body"]').find('h2').exists()).toBe(true)
    await wrapper.get('[data-testid="detail-body"] a').trigger('click')
    expect(wrapper.emitted('open-url')).toEqual([['https://example.com']])
  })

  it('formats a current event as now, never now ago', () => {
    const wrapper = mount(DetailPane, { props: { item, actions } })
    expect(wrapper.text()).toContain('· now')
    expect(wrapper.text()).not.toContain('now ago')
  })

  it('emits action and browser intents and has an empty state', async () => {
    const wrapper = mount(DetailPane, { props: { item, actions } })
    await wrapper.get('[data-testid="action-card"]').trigger('click')
    await wrapper.get('[data-testid="item-actions-toggle"]').trigger('click')
    await wrapper.get('[data-testid="menu-open-browser"]').trigger('click')
    expect(wrapper.emitted('run-action')).toEqual([['summarize']])
    expect(wrapper.emitted('open-browser')).toHaveLength(1)
    expect(mount(DetailPane, { props: { item: null, actions: [] } }).text()).toContain('Select an item')
  })

  it('persists resize changes', async () => {
    const wrapper = mount(DetailPane, { props: { item, actions } })
    const handle = wrapper.get('[data-testid="resize-handle-detailpane"]')
    await handle.trigger('pointerdown', { clientX: 500, pointerId: 1 })
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 450, pointerId: 1 }))
    await wrapper.vm.$nextTick()
    expect(wrapper.get('aside').element.style.width).toBe('516px')
    window.dispatchEvent(new PointerEvent('pointerup', { clientX: 450, pointerId: 1 }))
  })

  it('keeps the type pill non-wrapping and renders each configured action', () => {
    const wrapper = mount(DetailPane, { props: { item, actions: [...actions, { id: 'draft', label: 'Draft reply', type: 'shell', showInDetail: true, requiresSessionInput: false }] } })
    expect(wrapper.get('.kind-pill').classes()).toEqual(expect.arrayContaining(['shrink-0', 'whitespace-nowrap']))
    expect(wrapper.findAll('[data-testid="action-card"]')).toHaveLength(2)
  })

  it('does not render an empty body container and keeps long branch metadata in its label/value stack', () => {
    const wrapper = mount(DetailPane, { props: { item: { ...item, payload: payload({ body: '', branch: 'feat/a-very-long-branch-name-that-must-wrap' }) }, actions } })
    expect(wrapper.find('[data-testid="detail-body"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="action-footer-meta"]').text()).toContain('Runs headless (batch) on')
    expect(wrapper.get('[data-testid="action-footer-branch"]').text()).toContain('a-very-long-branch')
  })

  it('offers triage, open/copy, and configured actions from the shared item menu', async () => {
    const wrapper = mount(DetailPane, { props: { item, actions } })
    await wrapper.get('[data-testid="item-actions-toggle"]').trigger('click')
    const menu = wrapper.get('[data-testid="item-actions-menu"]')
    expect(menu.findAll('button').map((entry) => entry.get('span.flex-1').text())).toEqual([
      'Mark as read', 'Archive', 'Ignore', 'Open in browser', 'Copy link', 'Copy contents', 'Create session…', 'Summarize',
    ])
    await menu.get('[data-testid="menu-toggle-ignored"]').trigger('click')
    expect(wrapper.emitted('toggle-ignored')).toHaveLength(1)
    expect(wrapper.find('[data-testid="item-actions-menu"]').exists()).toBe(false)
  })

  it('relays copy and configured-action intents from the item menu', async () => {
    const wrapper = mount(DetailPane, { props: { item, actions } })
    await wrapper.get('[data-testid="item-actions-toggle"]').trigger('click')
    await wrapper.get('[data-testid="menu-copy-contents"]').trigger('click')
    await wrapper.get('[data-testid="item-actions-toggle"]').trigger('click')
    await wrapper.get('[data-testid="menu-action-summarize"]').trigger('click')
    expect(wrapper.emitted('copy-contents')).toHaveLength(1)
    expect(wrapper.emitted('run-action')).toEqual([['summarize']])
  })

  it('labels non-GitHub items with the neutral kind pill instead of Issue', () => {
    const webhookItem: InboxItem = { ...item, sourceKind: 'webhook', sourceScope: 'sources.webhook-1', url: '', payload: { id: 'run-1', status: 'failure' } }
    const wrapper = mount(DetailPane, { props: { item: webhookItem, actions: [] } })
    expect(wrapper.get('[data-testid="kind-pill"]').text()).toBe('Item')
    expect(wrapper.get('[data-testid="kind-pill"]').classes()).toContain('kind-pill-neutral')
    expect(wrapper.get('[data-testid="source-badge"]').attributes('data-source')).toBe('webhook')
    expect(wrapper.text()).not.toContain('#42')
  })

  it('omits the open-in-browser menu entry for webhook items without a URL, and the ACTIONS block when it has no applicable actions', async () => {
    const webhookItem: InboxItem = { ...item, sourceKind: 'webhook', sourceScope: 'sources.webhook-1', url: '', payload: { id: 'run-1' } }
    const wrapper = mount(DetailPane, { props: { item: webhookItem, actions: [] } })
    await wrapper.get('[data-testid="item-actions-toggle"]').trigger('click')
    expect(wrapper.find('[data-testid="menu-open-browser"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('ACTIONS')
    expect(wrapper.find('[data-testid="action-footer-meta"]').exists()).toBe(false)
  })

  it('shows the ACTIONS block for a webhook item with applicable actions, without a branch footer', () => {
    const webhookItem: InboxItem = { ...item, sourceKind: 'webhook', sourceScope: 'sources.webhook-1', url: '', payload: { id: 'run-1', kind: 'deploy' } }
    const wrapper = mount(DetailPane, { props: { item: webhookItem, actions } })
    expect(wrapper.text()).toContain('ACTIONS')
    expect(wrapper.findAll('[data-testid="action-card"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="action-footer-meta"]').exists()).toBe(false)
  })

  it('hides the ACTIONS block for a GitHub item with no applicable actions', () => {
    const wrapper = mount(DetailPane, { props: { item, actions: [] } })
    expect(wrapper.text()).not.toContain('ACTIONS')
    expect(wrapper.find('[data-testid="action-footer-meta"]').exists()).toBe(false)
  })

  it('offers the open-in-browser menu entry for webhook items that carry a URL', async () => {
    const webhookItem: InboxItem = { ...item, sourceKind: 'webhook', sourceScope: 'sources.webhook-1', payload: { id: 'run-1', url: 'https://ci.example.com/run/1' } }
    const wrapper = mount(DetailPane, { props: { item: webhookItem, actions: [] } })
    await wrapper.get('[data-testid="item-actions-toggle"]').trigger('click')
    expect(wrapper.find('[data-testid="menu-open-browser"]').exists()).toBe(true)
  })

  it('renders the Observed activity timeline in supplied chronological order', () => {
    const wrapper = mount(DetailPane, { props: { item, actions, events: [
      { id: 1, itemId: 42, kind: 'created', transition: 'created', attention: 'activity', summary: 'first observation', createdAt: 1 },
      { id: 2, itemId: 42, kind: 'updated', transition: 'updated', attention: 'activity', summary: 'second observation', createdAt: 2 },
    ] } })
    const timeline = wrapper.get('[data-testid="observed-activity"]')
    expect(timeline.text()).toContain('OBSERVED ACTIVITY')
    expect(timeline.findAll('li')).toHaveLength(2)
    expect(timeline.findAll('li')[0]!.text()).toContain('first observation')
    expect(timeline.findAll('li')[1]!.text()).toContain('second observation')
  })

})
