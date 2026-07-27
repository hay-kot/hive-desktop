import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ProfileRail from '../ProfileRail.vue'

const profiles = [{
  id: 'personal',
  letter: 'P',
  name: 'Personal',
  enabled: true,
  sourceSummary: 'GitHub · 2 sources',
  totalCount: 3,
  unreadCount: 1,
  feeds: [],
}]

describe('ProfileRail', () => {
  it('marks disabled profiles while keeping them selectable', async () => {
    const disabled = { ...profiles[0], enabled: false }
    const wrapper = mount(ProfileRail, { props: { profiles: [disabled], activeProfileId: 'personal' } })
    const tile = wrapper.get('[data-testid="profile-tile"]')

    expect(tile.attributes('data-enabled')).toBe('false')
    expect(tile.attributes('aria-label')).toContain('disabled')
    await tile.trigger('click')
    expect(wrapper.emitted('select')).toEqual([['personal']])
  })

  it('renders the avatar image when set, otherwise the letter chip', () => {
    const withImage = { ...profiles[0], image: 'data:image/png;base64,AAAA' }
    const wrapper = mount(ProfileRail, { props: { profiles: [withImage], activeProfileId: 'personal' } })
    const img = wrapper.get('[data-testid="profile-tile"] img')
    expect(img.attributes('src')).toBe('data:image/png;base64,AAAA')

    const plain = mount(ProfileRail, { props: { profiles, activeProfileId: 'personal' } })
    expect(plain.find('[data-testid="profile-tile"] img').exists()).toBe(false)
    expect(plain.get('[data-testid="profile-tile"]').text()).toContain('P')
  })

  it('shows one application settings action at the bottom', async () => {
    const wrapper = mount(ProfileRail, { props: { profiles, activeProfileId: 'personal' } })

    expect(wrapper.find('[data-testid="application-settings"]').attributes('aria-label')).toBe('Application settings')
    expect(wrapper.text()).not.toContain('hy')

    await wrapper.find('[data-testid="application-settings"]').trigger('click')
    expect(wrapper.emitted('open-settings')).toHaveLength(1)
  })
})
