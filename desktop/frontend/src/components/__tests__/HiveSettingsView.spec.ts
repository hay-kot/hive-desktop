import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import HiveSettingsView from '../HiveSettingsView.vue'
import { useCommandPalette } from '../../composables/useCommands'

const mocks = vi.hoisted(() => ({
  Info: vi.fn(),
  OpenHiveConfig: vi.fn(),
  OpenPath: vi.fn(),
  RevealPath: vi.fn(),
  ChooseDirectory: vi.fn(),
  OpenURL: vi.fn(),
  SetText: vi.fn(),
  Setup: vi.fn(),
  Save: vi.fn(),
  InspectWorkspace: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice', () => ({
  Info: mocks.Info,
  OpenHiveConfig: mocks.OpenHiveConfig,
  OpenPath: mocks.OpenPath,
  RevealPath: mocks.RevealPath,
  ChooseDirectory: mocks.ChooseDirectory,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/hiveconfigservice', () => ({
  Setup: mocks.Setup,
  Save: mocks.Save,
  InspectWorkspace: mocks.InspectWorkspace,
}))
vi.mock('@wailsio/runtime', () => ({
  Browser: { OpenURL: mocks.OpenURL },
  Clipboard: { SetText: mocks.SetText },
}))

const CONFIG = '/home/u/.config/hive/config.yaml'
const WORKSPACE = '/home/u/code'

function info(exists: boolean, overridden = false) {
  return {
    dataDir: { path: '/home/u/.local/share/hive', exists: true, overridden: false },
    configDir: { path: '/home/u/.config/hive/desktop', exists: true, overridden: false },
    logFile: { path: '/home/u/.local/share/hive/desktop/desktop.log', exists: true, overridden: false },
    database: { path: '/home/u/.local/share/hive/desktop/desktop-pipeline.db', exists: true, overridden: false },
    agentWorkspaces: { path: '/home/u/.config/hive/desktop/workspaces', exists: true, overridden: false },
    hiveConfig: { path: CONFIG, exists, overridden },
  }
}

function setup(over: Partial<{
  exists: boolean
  usable: boolean
  unreadable: string
  defaultAgent: string
  profiles: Array<{ name: string, command: string, flags: string[] | null }>
  workspaces: Array<{ path: string, exists: boolean, repos: number }>
  defaultAgentOverride: string
}> = {}) {
  return {
    config: {
      path: CONFIG,
      exists: over.exists ?? true,
      environmentOverride: false,
      usable: over.usable ?? true,
      unreadable: over.unreadable ?? '',
      defaultAgent: over.defaultAgent ?? 'claude',
      profiles: over.profiles ?? [{ name: 'claude', command: 'claude', flags: [] }],
      workspaces: over.workspaces ?? [{ path: WORKSPACE, exists: true, repos: 12 }],
    },
    agents: [
      { name: 'claude', label: 'Claude Code', skipPermissionFlags: ['--dangerously-skip-permissions'], installed: true },
      { name: 'opencode', label: 'OpenCode', skipPermissionFlags: ['--agent', 'free-permissions-runner'], installed: false },
      { name: 'copilot', label: 'GitHub Copilot', skipPermissionFlags: [], installed: false },
    ],
    defaultAgentOverride: over.defaultAgentOverride ?? '',
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.OpenHiveConfig.mockResolvedValue(undefined)
  mocks.OpenPath.mockResolvedValue(undefined)
  mocks.RevealPath.mockResolvedValue(undefined)
  mocks.Setup.mockResolvedValue(setup())
  mocks.Save.mockImplementation(async () => setup())
  mocks.InspectWorkspace.mockResolvedValue({ path: WORKSPACE, exists: true, repos: 12 })
  mocks.ChooseDirectory.mockResolvedValue(WORKSPACE)
})

describe('HiveSettingsView', () => {
  it('explains the included runtime and that edits here need no restart', async () => {
    mocks.Info.mockResolvedValue(info(true))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    expect(wrapper.text()).toContain('does not require or invoke a separately installed Hive CLI')
    // The restart caveat narrowed rather than disappeared: this pane writes
    // the file and reloads the runtime, so only a hand edit still needs one.
    const notice = wrapper.get('[data-testid="hive-restart-notice"]').text()
    expect(notice).toContain('apply straight away')
    expect(notice).toContain('by hand needs a restart')

    await wrapper.get('[data-testid="hive-cli-docs"]').trigger('click')
    expect(mocks.OpenURL).toHaveBeenCalledWith('https://colonyops.github.io/hive/')
  })

  it('shows what the config declares and marks installed agents', async () => {
    mocks.Info.mockResolvedValue(info(true))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-agent-claude"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="hive-agent-opencode"]').attributes('aria-pressed')).toBe('false')
    expect(wrapper.find('[data-testid="hive-agent-installed-claude"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="hive-agent-installed-opencode"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="hive-workspace-list"]').text()).toContain(WORKSPACE)
    expect(wrapper.get('[data-testid="hive-workspace-list"]').text()).toContain('12 repositories')
  })

  it('saves an edited agent and folder set', async () => {
    mocks.Info.mockResolvedValue(info(true))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    // Nothing has changed yet, so there is nothing to save.
    expect(wrapper.get('[data-testid="hive-config-save"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="hive-agent-opencode"]').trigger('click')
    await wrapper.get('[data-testid="hive-skip-permissions"]').setValue(true)
    await flushPromises()
    expect(wrapper.get('[data-testid="hive-config-save"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-testid="hive-config-save"]').trigger('click')
    await flushPromises()

    expect(mocks.Save).toHaveBeenCalledWith({
      defaultAgent: 'claude',
      profiles: [
        { name: 'claude', command: 'claude', flags: ['--dangerously-skip-permissions'] },
        { name: 'opencode', command: 'opencode', flags: ['--agent', 'free-permissions-runner'] },
      ],
      workspaces: [WORKSPACE],
    })
    expect(wrapper.get('[data-testid="hive-config-saved"]').text()).toContain('Saved')
  })

  it('adds a folder through the native picker and counts what is in it', async () => {
    mocks.Info.mockResolvedValue(info(true))
    mocks.Setup.mockResolvedValue(setup({ workspaces: [], usable: false }))
    mocks.ChooseDirectory.mockResolvedValue('/home/u/work')
    mocks.InspectWorkspace.mockResolvedValue({ path: '/home/u/work', exists: true, repos: 3 })
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="hive-add-workspace"]').trigger('click')
    await flushPromises()

    expect(mocks.InspectWorkspace).toHaveBeenCalledWith('/home/u/work')
    expect(wrapper.get('[data-testid="hive-workspace-list"]').text()).toContain('3 repositories')
  })

  it('reports a rejected folder without adding it', async () => {
    mocks.Info.mockResolvedValue(info(true))
    mocks.ChooseDirectory.mockResolvedValue('/home/u/code/one-repo')
    mocks.InspectWorkspace.mockRejectedValue(new Error('that is a repository, not the folder that holds your repositories'))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="hive-add-workspace"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-config-error"]').text()).toContain('not the folder that holds your repositories')
    expect(wrapper.get('[data-testid="hive-workspace-list"]').text()).not.toContain('one-repo')
  })

  // HIVE_DEFAULT_AGENT wins over agents.default at load, so a pane that let
  // someone pick an agent without saying so would be lying to them.
  it('warns when the environment is overriding the chosen agent', async () => {
    mocks.Info.mockResolvedValue(info(true))
    mocks.Setup.mockResolvedValue(setup({ defaultAgentOverride: 'opencode' }))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    const warning = wrapper.get('[data-testid="hive-default-agent-override"]').text()
    expect(warning).toContain('HIVE_DEFAULT_AGENT')
    expect(warning).toContain('opencode')
  })

  it('refuses to rewrite a config it could not parse', async () => {
    mocks.Info.mockResolvedValue(info(true))
    mocks.Setup.mockResolvedValue(setup({ unreadable: 'yaml: line 4: mapping values are not allowed', usable: false }))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-unreadable"]').text()).toContain('line 4')
    expect(wrapper.find('[data-testid="hive-config-save"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hive-setup-agents"]').exists()).toBe(false)
  })

  it('lists profiles it has no control for so a save is not a surprise', async () => {
    mocks.Info.mockResolvedValue(info(true))
    mocks.Setup.mockResolvedValue(setup({
      defaultAgent: 'fable',
      profiles: [{ name: 'fable', command: 'claude --model fable', flags: [] }],
    }))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-custom-profiles"]').text()).toContain('fable')
    expect(wrapper.get('[data-testid="hive-custom-profiles"]').text()).toContain('kept as written')
  })

  it('opens and reveals an existing config', async () => {
    mocks.Info.mockResolvedValue(info(true, true))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-config-path"]').text()).toBe(CONFIG)
    expect(wrapper.get('[data-testid="hive-config-overridden"]').text()).toBe('HIVE_CONFIG')

    const palette = useCommandPalette()
    palette.query.value = ''
    palette.scope.value = 'actions'
    const actionIds = palette.results.value.map((row) => row.id)
    expect(actionIds).toContain('hive:config:copy')
    expect(actionIds).toContain('hive:config:open')
    expect(actionIds).toContain('hive:config:reveal')
    expect(actionIds).not.toContain('hive:config:create')

    await wrapper.get('[data-testid="hive-config-open"]').trigger('click')
    await wrapper.get('[data-testid="hive-config-reveal"]').trigger('click')

    expect(mocks.OpenPath).toHaveBeenCalledWith(CONFIG)
    expect(mocks.RevealPath).toHaveBeenCalledWith(CONFIG)
  })

  it('creates a missing config, opens it, and refreshes its state', async () => {
    mocks.Info.mockResolvedValueOnce(info(false)).mockResolvedValueOnce(info(true))
    mocks.Setup.mockResolvedValue(setup({ exists: false, usable: false, workspaces: [], profiles: [] }))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-config-missing"]').text()).toContain('built-in defaults')
    expect(wrapper.find('[data-testid="hive-config-open"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hive-config-reveal"]').exists()).toBe(false)

    const palette = useCommandPalette()
    palette.query.value = ''
    palette.scope.value = 'actions'
    expect(palette.results.value.map((row) => row.id)).toEqual(expect.arrayContaining([
      'hive:config:copy',
      'hive:config:create',
    ]))
    expect(palette.results.value.map((row) => row.id)).not.toContain('hive:config:open')

    await wrapper.get('[data-testid="hive-config-create"]').trigger('click')
    await flushPromises()

    expect(mocks.OpenHiveConfig).toHaveBeenCalledOnce()
    expect(mocks.Info).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="hive-config-missing"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hive-config-open"]').exists()).toBe(true)
    expect(palette.results.value.map((row) => row.id)).toContain('hive:config:open')
    expect(palette.results.value.map((row) => row.id)).not.toContain('hive:config:create')
  })

  it('refreshes file state while preserving an open failure', async () => {
    mocks.Info.mockResolvedValueOnce(info(false)).mockResolvedValueOnce(info(true))
    mocks.Setup.mockResolvedValue(setup({ exists: false, usable: false, workspaces: [], profiles: [] }))
    mocks.OpenHiveConfig.mockRejectedValue(new Error('the file was created but no editor opened'))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="hive-config-create"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-settings-error"]').text()).toContain('no editor opened')
    expect(wrapper.find('[data-testid="hive-config-missing"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="hive-config-open"]').exists()).toBe(true)
  })
})
