import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AboutSettingsView from '../AboutSettingsView.vue'

const mocks = vi.hoisted(() => ({
  Build: vi.fn(),
  Status: vi.fn(),
  SetEnabled: vi.fn(),
  CheckNow: vi.fn(),
  OpenURL: vi.fn(),
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice', () => ({
  Build: mocks.Build,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/updaterservice', () => ({
  Status: mocks.Status,
  SetEnabled: mocks.SetEnabled,
  CheckNow: mocks.CheckNow,
}))
vi.mock('@wailsio/runtime', () => ({
  Browser: { OpenURL: mocks.OpenURL },
}))

function updateInfo(overrides: Record<string, unknown> = {}) {
  return {
    enabled: true,
    available: false,
    currentVersion: '1.4.0',
    latestVersion: '',
    notes: '',
    releaseUrl: '',
    ...overrides,
  }
}

function buildInfo(overrides: Record<string, unknown> = {}) {
  return {
    version: '1.4.0',
    commit: 'abc1234',
    date: '2026-07-01T12:00:00Z',
    repoUrl: 'https://github.com/hay-kot/hive-desktop',
    releaseUrl: 'https://github.com/hay-kot/hive-desktop/releases/tag/desktop-v1.4.0',
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.Build.mockResolvedValue(buildInfo())
  mocks.Status.mockResolvedValue(updateInfo())
  mocks.SetEnabled.mockResolvedValue(undefined)
  mocks.CheckNow.mockResolvedValue(updateInfo())
})

describe('AboutSettingsView', () => {
  it('renders the build info and links to the repo and GitHub release', async () => {
    mocks.OpenURL.mockResolvedValue(undefined)
    const wrapper = mount(AboutSettingsView)
    await flushPromises()

    expect(wrapper.find('[data-testid="about-build-version"]').text()).toBe('1.4.0')
    expect(wrapper.find('[data-testid="about-build-commit"]').text()).toBe('abc1234')
    expect(wrapper.find('[data-testid="about-build-date"]').text()).toBe('2026-07-01T12:00:00Z')

    await wrapper.find('[data-testid="about-build-repo"]').trigger('click')
    expect(mocks.OpenURL).toHaveBeenCalledWith('https://github.com/hay-kot/hive-desktop')

    await wrapper.find('[data-testid="about-build-release"]').trigger('click')
    expect(mocks.OpenURL).toHaveBeenCalledWith('https://github.com/hay-kot/hive-desktop/releases/tag/desktop-v1.4.0')
  })

  it('keeps the repo link but hides the release link for dev builds', async () => {
    mocks.Build.mockResolvedValue(buildInfo({ version: 'dev', releaseUrl: '' }))
    const wrapper = mount(AboutSettingsView)
    await flushPromises()

    expect(wrapper.find('[data-testid="about-build-version"]').text()).toBe('dev')
    expect(wrapper.find('[data-testid="about-build-repo"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="about-build-release"]').exists()).toBe(false)
  })

  it('toggles automatic updates through the service', async () => {
    mocks.Status.mockResolvedValue(updateInfo({ enabled: true }))
    const wrapper = mount(AboutSettingsView)
    await flushPromises()

    await wrapper.find('[data-testid="about-auto-update"]').trigger('click')
    expect(mocks.SetEnabled).toHaveBeenCalledWith(false)
  })

  it('checks for updates and shows an available result inline', async () => {
    mocks.CheckNow.mockResolvedValue(updateInfo({ available: true, latestVersion: '1.5.0' }))
    const wrapper = mount(AboutSettingsView)
    await flushPromises()

    await wrapper.find('[data-testid="about-check-update"]').trigger('click')
    await flushPromises()
    expect(mocks.CheckNow).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="about-update-available"]').text()).toContain('1.5.0')
  })

  it('shows up to date after a check finds nothing', async () => {
    mocks.CheckNow.mockResolvedValue(updateInfo({ available: false }))
    const wrapper = mount(AboutSettingsView)
    await flushPromises()

    await wrapper.find('[data-testid="about-check-update"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="about-update-uptodate"]').exists()).toBe(true)
  })

  it('restores the switch when persisting automatic updates fails', async () => {
    mocks.SetEnabled.mockRejectedValue(new Error('disk is read-only'))
    const wrapper = mount(AboutSettingsView)
    await flushPromises()

    await wrapper.find('[data-testid="about-auto-update"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="about-auto-update"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[data-testid="about-error"]').text()).toContain('disk is read-only')
  })
})
