import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import SettingsView from '../SettingsView.vue'
import { setTheme } from '../../composables/useTheme'
import {
  defaultTerminalFontWeight,
  defaultTerminalFontWeightBold,
  setTerminalFontSize,
  setTerminalFontWeight,
  setTerminalFontWeightBold,
} from '../../composables/useTerminalFont'
import { TERMINAL_FONT } from '../../lib/terminalFaces'
import { setTerminalShowWindows } from '../../composables/useTerminalShowWindows'
import { setTerminalPoolSize } from '../../composables/useTerminalPoolSize'
import { resetWebhookSettingsForTests } from '../../composables/useWebhookSettings'

const setTerminalShowWindowsBinding = vi.hoisted(() => vi.fn())
const setTerminalPoolSizeBinding = vi.hoisted(() => vi.fn())
const setTerminalFontFamilyBinding = vi.hoisted(() => vi.fn())
const setTerminalFontWeightsBinding = vi.hoisted(() => vi.fn())
const monospaceFontsBinding = vi.hoisted(() => vi.fn().mockResolvedValue(['Fira Code', 'Menlo']))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  GithubSettings: vi.fn().mockResolvedValue({ pollIntervalSeconds: 60, minPollIntervalSeconds: 60 }),
  SetGithubSettings: vi.fn(),
  NotificationSettings: vi.fn().mockResolvedValue({ notificationsEnabled: true, systemNotificationsEnabled: true, notificationSound: true }),
  SetNotificationSettings: vi.fn(),
  AppearanceSettings: vi.fn().mockResolvedValue({ theme: '', terminalFontSize: '', terminalFontFamily: '', terminalFontWeight: 0, terminalFontWeightBold: 0, terminalShowWindows: true, terminalPoolSize: 3 }),
  MonospaceFonts: monospaceFontsBinding,
  SetTheme: vi.fn(),
  SetTerminalFontSize: vi.fn(),
  SetTerminalFontFamily: setTerminalFontFamilyBinding,
  SetTerminalFontWeights: setTerminalFontWeightsBinding,
  SetTerminalShowWindows: setTerminalShowWindowsBinding,
  SetTerminalPoolSize: setTerminalPoolSizeBinding,
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
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/grafanaservice', () => ({
  Connect: vi.fn(),
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
    { key: 'github', title: 'GitHub source', stability: 'stable', provider: 'github', types: ['sources.github'], accounts: ['octocat'], envOverride: false },
    { key: 'sources.webhook', title: 'Webhook source', stability: 'stable', provider: '', types: ['sources.webhook'], accounts: [], envOverride: false },
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

  it('reflects and changes the terminal font size preset', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    expect(wrapper.find('[data-testid="settings-terminal-font-size-medium"]').attributes('aria-selected')).toBe('true')

    await wrapper.find('[data-testid="settings-terminal-font-size-xl"]').trigger('click')

    expect(wrapper.find('[data-testid="settings-terminal-font-size-xl"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="settings-terminal-font-size-medium"]').attributes('aria-selected')).toBe('false')

    // The size is a module singleton; put the default back for later tests.
    setTerminalFontSize('medium')
  })

  // #181: the terminal shipped with no weight control at all, so normal cells
  // rendered at whatever the atlas drew.
  it('reflects and changes the terminal font weight', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    expect(wrapper.find(`[data-testid="settings-terminal-font-weight-${defaultTerminalFontWeight}"]`)
      .attributes('aria-selected')).toBe('true')

    await wrapper.find('[data-testid="settings-terminal-font-weight-400"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="settings-terminal-font-weight-400"]').attributes('aria-selected')).toBe('true')
    expect(setTerminalFontWeightsBinding).toHaveBeenCalledWith(400, defaultTerminalFontWeightBold)

    setTerminalFontWeight(defaultTerminalFontWeight)
  })

  // Both weights go through one setter: written separately, a caller could land
  // a normal weight above the bold one.
  it('persists both weights when only the bold one changes', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })
    setTerminalFontWeightsBinding.mockClear()

    await wrapper.find('[data-testid="settings-terminal-font-weight-bold-600"]').trigger('click')
    await flushPromises()

    expect(setTerminalFontWeightsBinding).toHaveBeenCalledWith(defaultTerminalFontWeight, 600)

    setTerminalFontWeightBold(defaultTerminalFontWeightBold)
  })

  // The scan is what the webview cannot do for itself, and the bundled face
  // leads the list whether or not it is also installed system-wide.
  it('offers the installed monospace families with the bundled face first', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })
    await flushPromises()

    await wrapper.get('[data-testid="settings-terminal-font-family-select"]').trigger('click')
    await flushPromises()

    const labels = Array.from(document.querySelectorAll('[role="option"]'))
    expect(monospaceFontsBinding).toHaveBeenCalled()
    expect(labels.map((el) => el.textContent?.trim())).toEqual([
      `${TERMINAL_FONT} · bundled`,
      'Fira Code',
      'Menlo',
    ])
  })

  it('reflects and toggles the terminal window listing', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    const toggle = wrapper.get('[data-testid="settings-terminal-show-windows"]')
    expect(toggle.attributes('aria-checked')).toBe('true')

    await toggle.trigger('click')
    await flushPromises()

    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(setTerminalShowWindowsBinding).toHaveBeenCalledWith(false)

    // The setting is a module singleton; put the default back for later tests.
    setTerminalShowWindows(true)
  })

  it('reflects and changes the terminal warm-session count', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    expect(wrapper.find('[data-testid="settings-terminal-pool-size-3"]').attributes('aria-selected')).toBe('true')

    await wrapper.find('[data-testid="settings-terminal-pool-size-5"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="settings-terminal-pool-size-5"]').attributes('aria-selected')).toBe('true')
    expect(setTerminalPoolSizeBinding).toHaveBeenCalledWith(5)

    // The setting is a module singleton; put the default back for later tests.
    setTerminalPoolSize(3)
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

  // The card list's presentation/drawer maps (in SettingsView.vue) are keyed
  // by connector type and documented as incomplete by design: a type the
  // registry reports but the maps have not met yet still renders — generic
  // icon, no blurb, no configure gear — rather than being dropped from the
  // list. A connector added in Go before its presentation entry lands must
  // not silently disappear from Settings.
  it('renders a card for a connector type its presentation maps do not know', async () => {
    listIntegrations.mockResolvedValue([
      { key: 'posthog', title: 'PostHog', stability: 'experimental', provider: 'posthog', types: ['sources.posthog'], accounts: [], envOverride: false },
    ])
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    const card = wrapper.find('[data-testid="integration-posthog"]')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('PostHog')
    // No presentation entry means no blurb, not a crash or a missing card.
    expect(wrapper.find('[data-testid="integration-posthog-status"]').text()).toBe('Not connected')
    // No drawer entry means no configure gear, rather than a dead button.
    expect(wrapper.find('[data-testid="integration-posthog-configure"]').exists()).toBe(false)
  })

  it('reports a connector connected by an environment override', async () => {
    listIntegrations.mockResolvedValue([
      { key: 'github', title: 'GitHub source', stability: 'stable', provider: 'github', types: ['sources.github'], accounts: [], envOverride: true },
    ])
    const wrapper = mount(SettingsView, { props: { activeCategory: 'integrations' } })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-github-status"]').text()).toBe('Connected')
    expect(wrapper.find('[data-testid="integration-github"]').text()).toContain('HIVE_GITHUB_TOKEN')
  })

  it('reports a connector with no credential as not connected', async () => {
    listIntegrations.mockResolvedValue([
      { key: 'github', title: 'GitHub source', stability: 'stable', provider: 'github', types: ['sources.github'], accounts: [], envOverride: false },
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

  it('opens Grafana integration settings from the cog', async () => {
    listIntegrations.mockResolvedValue([
      { key: 'grafana', title: 'Grafana', stability: 'experimental', provider: 'grafana', types: ['sources.grafana_alerts', 'sources.grafana_metrics'], accounts: [], envOverride: false },
    ])
    const wrapper = mount(SettingsView, {
      props: { activeCategory: 'integrations' },
      global: { stubs: { Teleport: true } },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="integration-grafana-configure"]').exists()).toBe(true)
    await wrapper.find('[data-testid="integration-grafana-configure"]').trigger('click')
    expect(wrapper.find('[data-testid="grafana-integration-drawer"]').exists()).toBe(true)
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

  it('exposes a skills section that renders the skill installer', () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'skills' } })

    expect(wrapper.find('[data-testid="settings-category-skills"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="skill-settings"]').exists()).toBe(true)
  })

  it('closes from the header action and Escape', async () => {
    const wrapper = mount(SettingsView, { props: { activeCategory: 'appearance' } })

    await wrapper.find('[data-testid="settings-close"]').trigger('click')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))

    expect(wrapper.emitted('close')).toHaveLength(2)
  })
})
