import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import type { Event as ActivityEvent } from '../../../bindings/github.com/hay-kot/hive-desktop/internal/app/activity/models'

const markSeen = vi.fn()
const load = vi.fn()
const events = ref<ActivityEvent[]>([])

vi.mock('../../composables/useActivity', () => ({
  useActivity: () => ({
    events,
    loading: ref(false),
    error: ref<string | null>(null),
    load,
    markSeen,
  }),
}))

import ActivityView from '../ActivityView.vue'

function seed(): ActivityEvent[] {
  const now = Date.now()
  return [
    { id: 4, createdAt: now - 1000, category: 'refresh', severity: 'info', title: 'Refreshed github:hive/core', body: '12 items updated' },
    { id: 3, createdAt: now - 2000, category: 'refresh', severity: 'error', title: 'Refresh failed for rpc:Sentry', body: 'exit 1' },
    { id: 2, createdAt: now - 3000, category: 'session', severity: 'success', title: 'Created session review-pr-1', body: 'sonnet' },
    { id: 1, createdAt: now - 4000, category: 'auto_action', severity: 'auto', title: 'Auto-action · Triage', body: 'rule triage.default' },
  ]
}

function rows(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('[data-testid="activity-row"]')
}

describe('ActivityView', () => {
  it('clears the unseen marker on open and shows every event under All', () => {
    events.value = seed()
    const wrapper = mount(ActivityView)
    expect(markSeen).toHaveBeenCalled()
    expect(rows(wrapper)).toHaveLength(4)
  })

  it('filters to errors by severity, not category', async () => {
    events.value = seed()
    const wrapper = mount(ActivityView)
    await wrapper.find('[data-testid="activity-filter-error"]').trigger('click')
    const r = rows(wrapper)
    expect(r).toHaveLength(1)
    expect(r[0].text()).toContain('Refresh failed for rpc:Sentry')
  })

  it('filters by category for the session/auto/refresh pills', async () => {
    events.value = seed()
    const wrapper = mount(ActivityView)
    await wrapper.find('[data-testid="activity-filter-session"]').trigger('click')
    expect(rows(wrapper)).toHaveLength(1)
    expect(rows(wrapper)[0].text()).toContain('Created session review-pr-1')
  })

  it('filters by the search box across title and body', async () => {
    events.value = seed()
    const wrapper = mount(ActivityView)
    await wrapper.find('[data-testid="activity-search"]').setValue('triage')
    expect(rows(wrapper)).toHaveLength(1)
    expect(rows(wrapper)[0].text()).toContain('Auto-action')
  })

  it('shows the empty state when nothing matches', async () => {
    events.value = seed()
    const wrapper = mount(ActivityView)
    await wrapper.find('[data-testid="activity-search"]').setValue('nothing-matches-this')
    expect(rows(wrapper)).toHaveLength(0)
    expect(wrapper.find('[data-testid="activity-empty"]').exists()).toBe(true)
  })

  // #439: hue in the ledger means severity and nothing else. The seed covers a
  // failure, an auto-action and two ordinary categories, so a category hue
  // creeping back in shows up here.
  it('paints only failures and auto-actions, leaving every other row neutral', () => {
    events.value = seed()
    const wrapper = mount(ActivityView)
    const dots = wrapper.findAll('[data-testid="activity-row"] span.rounded-full')

    const dotClass = (i: number) => dots[i].attributes('class') ?? ''
    // Rows are newest-first: refresh, failed refresh, session, auto-action.
    expect(dotClass(0)).toContain('bg-text-4')
    expect(dotClass(1)).toContain('bg-severity-error')
    expect(dotClass(2)).toContain('bg-text-4')
    expect(dotClass(3)).toContain('bg-accent')

    const html = wrapper.html()
    for (const banned of ['bg-severity-success', 'bg-node-purple', 'bg-severity-info']) {
      expect(html).not.toContain(banned)
    }
  })

  // Only the two rows a reader scans for carry a rail; the rest lift on hover.
  it('rails the failure and the auto-action only', () => {
    events.value = seed()
    const wrapper = mount(ActivityView)
    const r = rows(wrapper)
    expect(r[0].attributes('class')).toContain('hover:bg-row-hover')
    expect(r[1].attributes('class')).toContain('bg-severity-error-tint')
    expect(r[2].attributes('class')).toContain('hover:bg-row-hover')
    expect(r[3].attributes('class')).toContain('bg-accent-tint')
  })

  it('emits close on Escape', async () => {
    events.value = seed()
    const wrapper = mount(ActivityView)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
