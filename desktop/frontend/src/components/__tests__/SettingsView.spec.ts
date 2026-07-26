import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import SettingsView from '../SettingsView.vue'
import { setTheme } from '../../composables/useTheme'
import { resetWebhookSettingsForTests } from '../../composables/useWebhookSettings'

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  GithubSettings: vi.fn().mockResolvedValue({ pollIntervalSeconds: 60, minPollIntervalSeconds: 60 }),
  SetGithubSettings: vi.fn(),
  NotificationSettings: vi.fn().mockResolvedValue({ notificationsEnabled: true, systemNotificationsEnabled: true, notificationSound: true }),
  SetNotificationSettings: vi.fn(),
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/notificationservice', () => ({
  PermissionStatus: vi.fn().mockResolvedValue('not-requested'),
  RequestNotificationPermission: vi.fn(),
}))
const listIntegrations = vi.hoisted(() => vi.fn())
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/integrationsservice', () => ({
  List: listIntegrations,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/githubservice', () => ({
  Status: vi.fn().mockResolvedValue({ state: 'connected', login: 'octocat', name: 'Octocat', avatarUrl: '', message: '' }),
  StartDeviceFlow: vi.fn(),
  CancelDeviceFlow: vi.fn(),
  SetToken: vi.fn(),
  Disconnect: vi.fn(),
}))
vi.mock('@wailsio/runtime', () => ({
  Events: { On: vi.fn().mockReturnValue(() => {}) },
  Browser: { OpenURL: vi.fn() },
}))

const webhookSettings = vi.hoisted(() => vi.fn())
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/webhookservice', () => ({
  Settings: webhookSettings,
  SetSettings: vi.fn(),
  GeneratePort: vi.fn(),
}))

beforeEach(() => {
  localStorage.clear()
  setTheme('dark')
  resetWebhookSettingsForTests()
  listIntegrations.mockResolvedValue([
    { type: 'sources.github', title: 'GitHub source', stability: 'stable', mode: 'pull', provider: 'github', accounts: ['octocat'], envOverride: false },
    { type: 'sources.webhook', title: 'Webhook source', stability: 'stable', mode: 'push', provider: '', accounts: [], envOverride: false },
  ])
  webhookSettings.mockResolvedValue({
    enabled: true,
    port: 24831,
    portMin: 20000,
    portMax: 32767,
    portOverridden: false,
    running: true,
    boundHost: '127.0.0.1',
    boundPort: 24831,
    baseUrl: 'http://127.0.0.1:24831/hooks/',
    startError: '',
    restartRequired: false,
  })
})

afterEach(() => {
  delete document.documentElement.dataset.theme
})

describe('SettingsView', () => {
  it('only exposes settings backed by application behavior', () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    expect(wrapper.find('[data-testid="settings-category-appearance"]').attributes('aria-current')).toBe('true')
    expect(wrapper.find('[data-testid="settings-theme-toggle-dark"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="settings-category-general"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="settings-category-integrations"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="settings-category-advanced"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="settings-display-name"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="settings-font-size"]').exists()).toBe(false)
  })

  it('reflects and changes the real application theme', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    expect(wrapper.find('[data-testid="settings-theme-toggle-dark"]').attributes('aria-selected')).toBe('true')

    await wrapper.find('[data-testid="settings-theme-toggle-gruvbox"]').trigger('click')

    expect(wrapper.find('[data-testid="settings-theme-toggle-gruvbox"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="settings-theme-toggle-dark"]').attributes('aria-selected')).toBe('false')
    expect(document.documentElement.dataset.theme).toBe('gruvbox')
    await nextTick()
    expect(localStorage.getItem('hive.theme')).toBe('gruvbox')
  })

  it('shows the connected GitHub source', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    await wrapper.find('[data-testid="settings-category-integrations"]').trigger('click')
    expect(wrapper.emitted('select-category')).toEqual([['integrations']])
    await wrapper.setProps({ activeCategory: 'integrations' })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-github-status"]').text()).toBe('Connected')
    expect(wrapper.find('[data-testid="integration-github"]').text()).toContain('Connected as octocat')
  })

  // The card list is a projection of the Go connector registry. A hardcoded
  // "coming soon" list is what this replaced: it drifted from the registry in
  // both directions — promising connectors that did not exist, and silently
  // omitting ones that did.
  it('renders one card per registered connector and nothing else', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    expect(wrapper.findAll('[data-testid^="integration-"][data-testid$="-status"]')).toHaveLength(2)
    for (const id of ['grafana', 'posthog', 'slack']) {
      expect(wrapper.find(`[data-testid="integration-${id}"]`).exists()).toBe(false)
    }
  })

  it('reports a connector connected by an environment override', async () => {
    listIntegrations.mockResolvedValue([
      { type: 'sources.github', title: 'GitHub source', stability: 'stable', mode: 'pull', provider: 'github', accounts: [], envOverride: true },
    ])
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-github-status"]').text()).toBe('Connected')
    expect(wrapper.find('[data-testid="integration-github"]').text()).toContain('HIVE_GITHUB_TOKEN')
  })

  it('reports a connector with no credential as not connected', async () => {
    listIntegrations.mockResolvedValue([
      { type: 'sources.github', title: 'GitHub source', stability: 'stable', mode: 'pull', provider: 'github', accounts: [], envOverride: false },
    ])
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-github-status"]').text()).toBe('Not connected')
  })

  it('opens GitHub integration settings from the cog', async () => {
    const wrapper = mount(SettingsView, {
      props: { activeCategory: 'integrations' },
      global: { stubs: { Teleport: true } },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-github-configure"]').exists()).toBe(true)
    await wrapper.find('[data-testid="integration-github-configure"]').trigger('click')
    expect(wrapper.find('[data-testid="github-integration-drawer"]').exists()).toBe(true)
  })

  it('shows the local webhook listener alongside the other integrations', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-webhook-status"]').text()).toBe('Running')
    expect(wrapper.find('[data-testid="integration-webhook"]').text()).toContain('http://127.0.0.1:24831/hooks/')
  })

  it('reports a disabled listener on the webhook card', async () => {
    webhookSettings.mockResolvedValue({
      enabled: false,
      port: 24831,
      portMin: 20000,
      portMax: 32767,
      portOverridden: false,
      running: false,
      boundHost: '127.0.0.1',
      boundPort: 0,
      baseUrl: 'http://127.0.0.1:24831/hooks/',
      startError: '',
      restartRequired: false,
    })
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-webhook-status"]').text()).toBe('Disabled')
  })

  it('a saved change the listener has not applied outranks what it is doing', async () => {
    // Disabled in settings but still bound: this session is running on
    // borrowed time, and the card must say so rather than "Running".
    webhookSettings.mockResolvedValue({
      enabled: false,
      port: 24831,
      portMin: 20000,
      portMax: 32767,
      portOverridden: false,
      running: true,
      boundHost: '127.0.0.1',
      boundPort: 24831,
      baseUrl: 'http://127.0.0.1:24831/hooks/',
      startError: '',
      restartRequired: true,
    })
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-webhook-status"]').text()).toBe('Restart needed')
  })

  it('reports a bind failure on the webhook card', async () => {
    webhookSettings.mockResolvedValue({
      enabled: true,
      port: 24831,
      portMin: 20000,
      portMax: 32767,
      portOverridden: false,
      running: false,
      boundHost: '127.0.0.1',
      boundPort: 0,
      baseUrl: 'http://127.0.0.1:24831/hooks/',
      startError: 'webhook listener: address already in use',
      restartRequired: true,
    })
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-webhook-status"]').text()).toBe('Port in use')
  })

  it('opens webhook settings from the cog', async () => {
    const wrapper = mount(SettingsView, {
      props: { activeCategory: 'integrations' },
      global: { stubs: { Teleport: true } },
    })
    await flushPromises()

    await wrapper.find('[data-testid="integration-webhook-configure"]').trigger('click')
    expect(wrapper.find('[data-testid="webhook-integration-drawer"]').exists()).toBe(true)
  })

  it('exposes a notifications category that renders the notification settings', () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'notifications' } })

    expect(wrapper.find('[data-testid="settings-category-notifications"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="notification-settings"]').exists()).toBe(true)
  })

  it('exposes a keybindings section that renders the editor', () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'keybindings' } })

    expect(wrapper.find('[data-testid="settings-category-keybindings"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="settings-keybindings"]').exists()).toBe(true)
  })

  it('exposes an LLM prompts section that renders the prompt catalog', () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'prompts' } })

    expect(wrapper.find('[data-testid="settings-category-prompts"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="prompt-settings"]').exists()).toBe(true)
  })

  it('closes from the header action and Escape', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    await wrapper.find('[data-testid="settings-close"]').trigger('click')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))

    expect(wrapper.emitted('close')).toHaveLength(2)
  })
})
