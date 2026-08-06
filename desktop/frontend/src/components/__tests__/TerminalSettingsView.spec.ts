import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import TerminalSettingsView from '../TerminalSettingsView.vue'
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
import { resetInstalledFontsForTests } from '../../composables/useInstalledFonts'

const mocks = vi.hoisted(() => ({
  SetTerminalShowWindows: vi.fn(),
  SetTerminalPoolSize: vi.fn(),
  SetTerminalFontFamily: vi.fn(),
  SetTerminalFontWeights: vi.fn(),
  Fonts: vi.fn().mockResolvedValue({ all: ['Fira Code', 'Menlo'], monospace: ['Fira Code', 'Menlo'] }),
  ExperimentalSettings: vi.fn(),
  SetExperimentalTerminal: vi.fn(),
  SetExperimentalAgents: vi.fn(),
  TerminalModeEnabled: vi.fn(),
  AgentsModeEnabled: vi.fn(),
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  AppearanceSettings: vi.fn().mockResolvedValue({ theme: '', terminalFontSize: '', terminalFontFamily: '', terminalFontWeight: 0, terminalFontWeightBold: 0, terminalShowWindows: true, terminalPoolSize: 3 }),
  Fonts: mocks.Fonts,
  SetTheme: vi.fn(),
  SetTerminalFontSize: vi.fn(),
  SetTerminalFontFamily: mocks.SetTerminalFontFamily,
  SetTerminalFontWeights: mocks.SetTerminalFontWeights,
  SetTerminalShowWindows: mocks.SetTerminalShowWindows,
  SetTerminalPoolSize: mocks.SetTerminalPoolSize,
  ExperimentalSettings: mocks.ExperimentalSettings,
  SetExperimentalTerminal: mocks.SetExperimentalTerminal,
  SetExperimentalAgents: mocks.SetExperimentalAgents,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice', () => ({
  Enabled: mocks.TerminalModeEnabled,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice', () => ({
  Enabled: mocks.AgentsModeEnabled,
}))

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  // The scan is a module singleton that runs once, so without this the second
  // mount in this file would keep the first test's list.
  resetInstalledFontsForTests()
  mocks.Fonts.mockResolvedValue({ all: ['Fira Code', 'Menlo'], monospace: ['Fira Code', 'Menlo'] })
  mocks.ExperimentalSettings.mockResolvedValue({ terminal: false, agents: false })
  mocks.SetExperimentalTerminal.mockImplementation((enabled: boolean) => Promise.resolve({ terminal: enabled, agents: false }))
  mocks.TerminalModeEnabled.mockResolvedValue(false)
  mocks.AgentsModeEnabled.mockResolvedValue(false)
  document.body.innerHTML = ''
})

describe('TerminalSettingsView', () => {
  it('persists the terminal opt-in and flags that a relaunch is pending', async () => {
    const wrapper = mount(TerminalSettingsView)
    await flushPromises()

    // Persisted off, running off: nothing pending.
    expect(wrapper.find('[data-testid="terminal-mode-enabled-restart"]').exists()).toBe(false)

    await wrapper.find('[data-testid="terminal-mode-enabled"]').trigger('click')
    await flushPromises()

    expect(mocks.SetExperimentalTerminal).toHaveBeenCalledWith(true)
    expect(wrapper.find('[data-testid="terminal-mode-enabled-restart"]').exists()).toBe(true)
  })

  it('shows no restart hint when the persisted opt-in matches the running app', async () => {
    mocks.ExperimentalSettings.mockResolvedValue({ terminal: true, agents: false })
    mocks.TerminalModeEnabled.mockResolvedValue(true)
    const wrapper = mount(TerminalSettingsView)
    await flushPromises()

    expect(wrapper.find('[data-testid="terminal-mode-enabled-restart"]').exists()).toBe(false)

    // Turning it off is a change against the running app, so it is pending too.
    await wrapper.find('[data-testid="terminal-mode-enabled"]').trigger('click')
    await flushPromises()
    expect(mocks.SetExperimentalTerminal).toHaveBeenCalledWith(false)
    expect(wrapper.find('[data-testid="terminal-mode-enabled-restart"]').exists()).toBe(true)
  })

  it('reverts the terminal opt-in switch when the save fails', async () => {
    mocks.SetExperimentalTerminal.mockRejectedValue(new Error('disk is read-only'))
    const wrapper = mount(TerminalSettingsView)
    await flushPromises()

    await wrapper.find('[data-testid="terminal-mode-enabled"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="terminal-mode-enabled"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-testid="terminal-error"]').text()).toContain('disk is read-only')
  })

  it('reflects and changes the terminal font size preset', async () => {
    const wrapper = mount(TerminalSettingsView)

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
    const wrapper = mount(TerminalSettingsView)

    expect(wrapper.find(`[data-testid="settings-terminal-font-weight-${defaultTerminalFontWeight}"]`)
      .attributes('aria-selected')).toBe('true')

    await wrapper.find('[data-testid="settings-terminal-font-weight-400"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="settings-terminal-font-weight-400"]').attributes('aria-selected')).toBe('true')
    expect(mocks.SetTerminalFontWeights).toHaveBeenCalledWith(400, defaultTerminalFontWeightBold)

    setTerminalFontWeight(defaultTerminalFontWeight)
  })

  // Both weights go through one setter: written separately, a caller could land
  // a normal weight above the bold one.
  it('persists both weights when only the bold one changes', async () => {
    const wrapper = mount(TerminalSettingsView)
    mocks.SetTerminalFontWeights.mockClear()

    await wrapper.find('[data-testid="settings-terminal-font-weight-bold-600"]').trigger('click')
    await flushPromises()

    expect(mocks.SetTerminalFontWeights).toHaveBeenCalledWith(defaultTerminalFontWeight, 600)

    setTerminalFontWeightBold(defaultTerminalFontWeightBold)
  })

  // The scan is what the webview cannot do for itself, and the bundled face
  // leads the list whether or not it is also installed system-wide.
  it('offers the installed monospace families with the bundled face first', async () => {
    const wrapper = mount(TerminalSettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="settings-terminal-font-family-select"]').trigger('click')
    await flushPromises()

    // The families can only have come from the binding — the scan is what the
    // webview cannot do for itself, and its result is cached for the process.
    const labels = Array.from(document.querySelectorAll('[role="option"]'))
    expect(labels.map((el) => el.textContent?.trim())).toEqual([
      `${TERMINAL_FONT} · bundled`,
      'Fira Code',
      'Menlo',
    ])
    wrapper.unmount()
  })

  it('reflects and toggles the terminal window listing', async () => {
    const wrapper = mount(TerminalSettingsView)

    const toggle = wrapper.get('[data-testid="settings-terminal-show-windows"]')
    expect(toggle.attributes('aria-checked')).toBe('true')

    await toggle.trigger('click')
    await flushPromises()

    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(mocks.SetTerminalShowWindows).toHaveBeenCalledWith(false)

    // The setting is a module singleton; put the default back for later tests.
    setTerminalShowWindows(true)
  })

  it('reflects and changes the terminal warm-session count', async () => {
    const wrapper = mount(TerminalSettingsView)

    expect(wrapper.find('[data-testid="settings-terminal-pool-size-3"]').attributes('aria-selected')).toBe('true')

    await wrapper.find('[data-testid="settings-terminal-pool-size-5"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="settings-terminal-pool-size-5"]').attributes('aria-selected')).toBe('true')
    expect(mocks.SetTerminalPoolSize).toHaveBeenCalledWith(5)

    // The setting is a module singleton; put the default back for later tests.
    setTerminalPoolSize(3)
  })
})
