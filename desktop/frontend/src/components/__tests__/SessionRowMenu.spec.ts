import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SessionRowMenu from '../SessionRowMenu.vue'
import type { SessionSummary } from '../../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

const session: SessionSummary = { id: 's1', name: 'review 81', slug: 'review-81', repo: 'acme/site', state: 'active' }

function mountMenu(props: Partial<InstanceType<typeof SessionRowMenu>['$props']> = {}) {
  return mount(SessionRowMenu, { props: { session, ...props } })
}

describe('SessionRowMenu', () => {
  it('emits the operation each entry stands for and closes', async () => {
    const wrapper = mountMenu()

    for (const [testid, event] of [
      ['session-menu-detail', 'detail'],
      ['session-menu-rename', 'rename'],
      ['session-menu-recycle', 'recycle'],
      ['session-menu-delete', 'delete'],
    ] as const) {
      await wrapper.get(`[data-testid="${testid}"]`).trigger('click')
      expect(wrapper.emitted(event), testid).toHaveLength(1)
    }
    expect(wrapper.emitted('close')).toHaveLength(4)
  })

  it('offers recycle only for an active session', () => {
    expect(mountMenu().find('[data-testid="session-menu-recycle"]').exists()).toBe(true)

    // Hive rejects recycling a session that is not active, so it is not offered.
    const recycled = mountMenu({ session: { ...session, state: 'recycled' } })
    expect(recycled.find('[data-testid="session-menu-recycle"]').exists()).toBe(false)
    expect(recycled.find('[data-testid="session-menu-delete"]').exists()).toBe(true)
  })

  it('appends host entries in their own section and reports them by id', async () => {
    const wrapper = mountMenu({
      extra: [{ kind: 'action', id: 'new-tab:claude', label: 'New Claude tab', testid: 'extra-claude' }],
      extraLabel: 'New tab',
    })

    expect(wrapper.text()).toContain('New tab')
    await wrapper.get('[data-testid="extra-claude"]').trigger('click')
    expect(wrapper.emitted('extra')).toEqual([['new-tab:claude']])
  })
})
