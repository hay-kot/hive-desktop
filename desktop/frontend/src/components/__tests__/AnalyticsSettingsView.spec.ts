import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AnalyticsSettingsView from '../AnalyticsSettingsView.vue'

const mocks = vi.hoisted(() => ({
  AnalyticsSettings: vi.fn(),
  SetAnalyticsEnabled: vi.fn(),
  OpenURL: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  AnalyticsSettings: mocks.AnalyticsSettings,
  SetAnalyticsEnabled: mocks.SetAnalyticsEnabled,
}))

vi.mock('@wailsio/runtime', () => ({ Browser: { OpenURL: mocks.OpenURL } }))

beforeEach(() => {
  vi.clearAllMocks()
  mocks.AnalyticsSettings.mockResolvedValue({ enabled: true, configured: true, overridden: false })
  mocks.SetAnalyticsEnabled.mockImplementation(async (enabled: boolean) => ({ enabled, configured: true, overridden: false }))
})

describe('AnalyticsSettingsView', () => {
  it('loads the current preference and opts out immediately', async () => {
    const wrapper = mount(AnalyticsSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="analytics-enabled"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[data-testid="analytics-enabled"]').trigger('click')
    await flushPromises()

    expect(mocks.SetAnalyticsEnabled).toHaveBeenCalledWith(false)
    expect(wrapper.get('[data-testid="analytics-enabled"]').attributes('aria-checked')).toBe('false')
  })

  it('restores the preference when saving fails', async () => {
    mocks.SetAnalyticsEnabled.mockRejectedValue(new Error('settings are read-only'))
    const wrapper = mount(AnalyticsSettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="analytics-enabled"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="analytics-enabled"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="analytics-error"]').text()).toContain('settings are read-only')
  })

  it('explains when the running build has no analytics destination', async () => {
    mocks.AnalyticsSettings.mockResolvedValue({ enabled: true, configured: false, overridden: false })
    const wrapper = mount(AnalyticsSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="analytics-unconfigured"]').text()).toBe('Not configured')
  })

  it('explains and disables an environment override', async () => {
    mocks.AnalyticsSettings.mockResolvedValue({ enabled: false, configured: true, overridden: true })
    const wrapper = mount(AnalyticsSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="analytics-overridden"]').text()).toBe('Environment override')
    expect(wrapper.get('[data-testid="analytics-enabled"]').attributes('disabled')).toBeDefined()
  })

  it('opens the privacy details', async () => {
    const wrapper = mount(AnalyticsSettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="analytics-privacy"]').trigger('click')
    expect(mocks.OpenURL).toHaveBeenCalledWith('https://hivedesktop.com/configuration/privacy/')
  })
})
