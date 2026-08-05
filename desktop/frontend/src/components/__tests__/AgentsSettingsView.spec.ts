import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AgentsSettingsView from '../AgentsSettingsView.vue'

const WORKSPACES = '/home/u/.config/hive/desktop/workspaces'

const mocks = vi.hoisted(() => ({
  Info: vi.fn(),
  OpenPath: vi.fn(),
  RevealPath: vi.fn(),
  ExperimentalSettings: vi.fn(),
  SetExperimentalTerminal: vi.fn(),
  SetExperimentalAgents: vi.fn(),
  TerminalModeEnabled: vi.fn(),
  AgentsModeEnabled: vi.fn(),
  SetText: vi.fn(),
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice', () => ({
  Info: mocks.Info,
  OpenPath: mocks.OpenPath,
  RevealPath: mocks.RevealPath,
  ChooseDirectory: vi.fn(),
  SetDataDir: vi.fn(),
  SetConfigDir: vi.fn(),
  ClearDataDir: vi.fn(),
  ClearConfigDir: vi.fn(),
  Quit: vi.fn(),
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
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
vi.mock('@wailsio/runtime', () => ({
  Clipboard: { SetText: mocks.SetText },
}))

beforeEach(() => {
  vi.clearAllMocks()
  mocks.Info.mockResolvedValue({
    dataDir: { path: '/home/u/.local/share/hive', exists: true, overridden: false },
    configDir: { path: '/home/u/.config/hive/desktop', exists: true, overridden: false },
    logFile: { path: '/home/u/.local/share/hive/desktop/desktop.log', exists: true, overridden: false },
    database: { path: '/home/u/.local/share/hive/desktop/desktop-pipeline.db', exists: true, overridden: false },
    agentWorkspaces: { path: WORKSPACES, exists: true, overridden: false },
  })
  mocks.ExperimentalSettings.mockResolvedValue({ terminal: false, agents: false })
  mocks.SetExperimentalAgents.mockImplementation((enabled: boolean) => Promise.resolve({ terminal: false, agents: enabled }))
  mocks.TerminalModeEnabled.mockResolvedValue(false)
  mocks.AgentsModeEnabled.mockResolvedValue(false)
  document.body.innerHTML = ''
})

describe('AgentsSettingsView', () => {
  it('persists the agents opt-in and flags that a relaunch is pending', async () => {
    const wrapper = mount(AgentsSettingsView)
    await flushPromises()

    expect(wrapper.find('[data-testid="agents-mode-enabled-restart"]').exists()).toBe(false)

    await wrapper.find('[data-testid="agents-mode-enabled"]').trigger('click')
    await flushPromises()

    expect(mocks.SetExperimentalAgents).toHaveBeenCalledWith(true)
    expect(wrapper.find('[data-testid="agents-mode-enabled-restart"]').exists()).toBe(true)
    // The terminal opt-in is a separate flag and is untouched by this one.
    expect(mocks.SetExperimentalTerminal).not.toHaveBeenCalled()
  })

  it('reverts the agents opt-in switch when the save fails', async () => {
    mocks.SetExperimentalAgents.mockRejectedValue(new Error('disk is read-only'))
    const wrapper = mount(AgentsSettingsView)
    await flushPromises()

    await wrapper.find('[data-testid="agents-mode-enabled"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="agents-mode-enabled"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-testid="agents-error"]').text()).toContain('disk is read-only')
  })

  // The sidebar's root-unavailable message points here, so the resolved root
  // has to be readable from this pane.
  it('shows the resolved workspace root and opens it', async () => {
    const wrapper = mount(AgentsSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="agents-workspace-root-path"]').text()).toBe(WORKSPACES)

    await wrapper.get('[data-testid="agents-workspace-root-open"]').trigger('click')
    expect(mocks.OpenPath).toHaveBeenCalledWith(WORKSPACES)

    await wrapper.get('[data-testid="agents-workspace-root-reveal"]').trigger('click')
    expect(mocks.RevealPath).toHaveBeenCalledWith(WORKSPACES)
  })
})
