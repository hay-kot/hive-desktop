import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import HiveSettingsView from '../HiveSettingsView.vue'
import { useCommandPalette } from '../../composables/useCommands'

const mocks = vi.hoisted(() => ({
  Info: vi.fn(),
  OpenHiveConfig: vi.fn(),
  OpenPath: vi.fn(),
  RevealPath: vi.fn(),
  OpenURL: vi.fn(),
  SetText: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice', () => ({
  Info: mocks.Info,
  OpenHiveConfig: mocks.OpenHiveConfig,
  OpenPath: mocks.OpenPath,
  RevealPath: mocks.RevealPath,
}))
vi.mock('@wailsio/runtime', () => ({
  Browser: { OpenURL: mocks.OpenURL },
  Clipboard: { SetText: mocks.SetText },
}))

const CONFIG = '/home/u/.config/hive/config.yaml'

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

beforeEach(() => {
  vi.clearAllMocks()
  mocks.OpenHiveConfig.mockResolvedValue(undefined)
  mocks.OpenPath.mockResolvedValue(undefined)
  mocks.RevealPath.mockResolvedValue(undefined)
})

describe('HiveSettingsView', () => {
  it('explains the included runtime, restart requirement, and default-agent precedence', async () => {
    mocks.Info.mockResolvedValue(info(true))
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    expect(wrapper.text()).toContain('does not require or invoke a separately installed Hive CLI')
    expect(wrapper.get('[data-testid="hive-restart-notice"]').text()).toContain('Restart Hive Desktop')
    expect(wrapper.text()).toContain('agents.default')
    expect(wrapper.text()).toContain('HIVE_DEFAULT_AGENT')

    await wrapper.get('[data-testid="hive-cli-docs"]').trigger('click')
    expect(mocks.OpenURL).toHaveBeenCalledWith('https://colonyops.github.io/hive/')
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
    const wrapper = mount(HiveSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="hive-config-missing"]').text()).toContain('using built-in defaults')
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
