import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import GrafanaIntegrationDrawer from '../GrafanaIntegrationDrawer.vue'

const mocks = vi.hoisted(() => ({ Connect: vi.fn(), Disconnect: vi.fn(), List: vi.fn(), On: vi.fn() }))

vi.mock('../../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/grafanaservice', () => ({
  Connect: mocks.Connect,
  Disconnect: mocks.Disconnect,
}))
vi.mock('../../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/integrationsservice', () => ({
  List: mocks.List,
}))
vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.On },
}))

function connectedStacks(...accounts: string[]) {
  mocks.List.mockResolvedValue([
    { type: 'sources.grafana_metrics', title: 'Grafana metrics source', stability: 'experimental', mode: 'pull', provider: 'grafana', accounts, envOverride: false },
  ])
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.On.mockReturnValue(() => {})
  connectedStacks()
})

function mountDrawer() {
  return mount(GrafanaIntegrationDrawer, { global: { stubs: { Teleport: true } } })
}

describe('GrafanaIntegrationDrawer', () => {
  it('shows no connected stacks initially', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    expect(wrapper.find('[data-testid="grafana-connected-empty"]').exists()).toBe(true)
  })

  it('connects with a pasted URL and token, then clears the inputs', async () => {
    mocks.Connect.mockResolvedValue({ account: 'grafana.example.com-1', url: 'https://grafana.example.com', orgID: 1, orgName: 'Main' })
    const wrapper = mountDrawer()
    await flushPromises()

    await wrapper.find('[data-testid="grafana-connect-url"]').setValue('https://grafana.example.com')
    await wrapper.find('[data-testid="grafana-connect-token"]').setValue('glsa_token')
    await wrapper.find('[data-testid="grafana-connect-submit"]').trigger('click')
    await flushPromises()

    expect(mocks.Connect).toHaveBeenCalledWith('https://grafana.example.com', 'glsa_token')
    expect(wrapper.get<HTMLInputElement>('[data-testid="grafana-connect-url"]').element.value).toBe('')
    expect(wrapper.get<HTMLInputElement>('[data-testid="grafana-connect-token"]').element.value).toBe('')
  })

  it('surfaces a rejected connection and keeps the inputs', async () => {
    mocks.Connect.mockRejectedValue(new Error('grafana rejected the token'))
    const wrapper = mountDrawer()
    await flushPromises()

    await wrapper.find('[data-testid="grafana-connect-url"]').setValue('https://grafana.example.com')
    await wrapper.find('[data-testid="grafana-connect-token"]').setValue('bad')
    await wrapper.find('[data-testid="grafana-connect-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="grafana-connect-error"]').text()).toContain('rejected')
    expect(wrapper.get<HTMLInputElement>('[data-testid="grafana-connect-url"]').element.value).toBe('https://grafana.example.com')
  })

  it('lists a connected stack and disconnects it', async () => {
    connectedStacks('grafana.example.com-1')
    mocks.Disconnect.mockResolvedValue(undefined)
    const wrapper = mountDrawer()
    await flushPromises()

    expect(wrapper.find('[data-testid="grafana-connected-grafana.example.com-1"]').exists()).toBe(true)
    await wrapper.find('[data-testid="grafana-disconnect-grafana.example.com-1"]').trigger('click')
    await flushPromises()

    expect(mocks.Disconnect).toHaveBeenCalledWith('grafana.example.com-1')
  })

  it('closes from the footer', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    await wrapper.find('[data-testid="grafana-settings-close"]').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})
