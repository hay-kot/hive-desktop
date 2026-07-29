import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import TerminalMode from '../TerminalMode.vue'

const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  ListSessions: vi.fn(),
  getTerminalEndpoint: vi.fn(),
  createTerminalClient: vi.fn(),
  useTerminalWindows: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice', () => ({
  Available: mocks.Available,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({
  ListSessions: mocks.ListSessions,
}))
vi.mock('../../lib/terminalClient', () => ({
  getTerminalEndpoint: mocks.getTerminalEndpoint,
  createTerminalClient: mocks.createTerminalClient,
}))
vi.mock('../../composables/useTerminalWindows', () => ({
  useTerminalWindows: mocks.useTerminalWindows,
}))

function fakeSession() {
  return {
    tabs: ref([
      { uid: 1, windowId: '@1', name: 'agent', active: true, term: {}, fit: {} },
      { uid: 2, windowId: '@2', name: 'shell', active: false, term: {}, fit: {} },
    ]),
    activeWindowId: ref('@1'),
    status: ref<'connecting' | 'live' | 'ended'>('live'),
    endReason: ref<string | null>(null),
    error: ref<string | null>(null),
    actionError: ref<string | null>(null),
    start: vi.fn().mockResolvedValue(undefined),
    reconnect: vi.fn().mockResolvedValue(undefined),
    select: vi.fn().mockResolvedValue(undefined),
    newWindow: vi.fn().mockResolvedValue(undefined),
    closeWindow: vi.fn().mockResolvedValue(undefined),
    rename: vi.fn().mockResolvedValue(undefined),
    attachTab: vi.fn(),
    disposeTab: vi.fn(),
    dispose: vi.fn(),
  }
}

async function mountAvailable(session = fakeSession()) {
  mocks.useTerminalWindows.mockReturnValue(session)
  const wrapper = mount(TerminalMode)
  await flushPromises()
  return { wrapper, session }
}

describe('TerminalMode', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.getTerminalEndpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 't', streamPath: '/s' })
    mocks.createTerminalClient.mockReturnValue({})
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'bump deps', slug: 'hive-bump-deps', repo: 'hay-kot/hive', state: 'active' },
    ])
  })

  it('renders the unavailable panel with the reason instead of gating the mode', async () => {
    mocks.Available.mockResolvedValue({ available: false, reason: 'tmux is not installed.' })

    const wrapper = mount(TerminalMode)
    await flushPromises()

    const panel = wrapper.find('[data-testid="terminal-unavailable"]')
    expect(panel.exists()).toBe(true)
    expect(wrapper.get('[data-testid="terminal-unavailable-reason"]').text()).toBe('tmux is not installed.')
    expect(wrapper.find('[data-testid="terminal-session-picker"]').exists()).toBe(false)
  })

  it('treats an endpoint failure as unavailable and can retry', async () => {
    mocks.getTerminalEndpoint.mockRejectedValueOnce(Object.assign(new Error('call failed'), {
      cause: { kind: 'unavailable', message: 'The local HTTP server is not running.' },
    }))

    const wrapper = mount(TerminalMode)
    await flushPromises()
    expect(wrapper.get('[data-testid="terminal-unavailable-reason"]').text()).toBe('The local HTTP server is not running.')

    mocks.useTerminalWindows.mockReturnValue(fakeSession())
    await wrapper.get('[data-testid="terminal-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="terminal-session-picker"]').exists()).toBe(true)
  })

  it('lists the active sessions and attaches to the one picked', async () => {
    const session = fakeSession()
    mocks.useTerminalWindows.mockReturnValue(session)
    const wrapper = mount(TerminalMode)
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="terminal-session-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('fix the parser')

    await rows[1].trigger('click')
    await flushPromises()

    expect(mocks.useTerminalWindows).toHaveBeenCalledWith('hive-bump-deps', expect.anything())
    expect(session.start).toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="terminal-tab"]').map((tab) => tab.text())).toEqual(['agent', 'shell'])
    expect(wrapper.findAll('[data-testid="terminal-pane"]')).toHaveLength(2)
  })

  it('says when there are no sessions to attach to', async () => {
    mocks.ListSessions.mockResolvedValue([])
    const wrapper = mount(TerminalMode)
    await flushPromises()

    expect(wrapper.find('[data-testid="terminal-sessions-empty"]').exists()).toBe(true)
  })

  it('offers reconnect when the stream drops, and again when tmux exits', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="terminal-session-ended"]').exists()).toBe(false)

    session.status.value = 'ended'
    session.endReason.value = 'disconnected'
    session.error.value = 'The terminal stream disconnected.'
    await flushPromises()

    expect(wrapper.get('[data-testid="terminal-session-ended"]').text()).toContain('Terminal stream lost')
    expect(wrapper.get('[data-testid="terminal-session-ended-reason"]').text()).toBe('The terminal stream disconnected.')

    session.endReason.value = 'exited'
    session.error.value = 'overflow'
    await flushPromises()
    expect(wrapper.get('[data-testid="terminal-session-ended"]').text()).toContain('Session ended')

    await wrapper.get('[data-testid="terminal-reconnect"]').trigger('click')
    expect(session.reconnect).toHaveBeenCalled()
  })

  it('drives the window toolbar and hands panes to the composable', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    expect(session.attachTab).toHaveBeenCalledTimes(2)
    expect(session.attachTab.mock.calls[0][0]).toBe('@1')

    await wrapper.get('[data-testid="terminal-new-window"]').trigger('click')
    expect(session.newWindow).toHaveBeenCalled()

    await wrapper.findAll('[data-testid="terminal-close-window"]')[1].trigger('click')
    expect(session.closeWindow).toHaveBeenCalledWith('@2')

    const tabs = wrapper.findAll('[data-testid="terminal-tab"]')
    await tabs[1].find('button').trigger('click')
    expect(session.select).toHaveBeenCalledWith('@2')

    await tabs[1].find('button').trigger('dblclick')
    const input = wrapper.get('[data-testid="terminal-rename-input"]')
    await input.setValue('build')
    await input.trigger('keydown.enter')
    expect(session.rename).toHaveBeenCalledWith('@2', 'build')
  })

  it('scopes every pane so the terminal owns its keys', async () => {
    const { wrapper } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    for (const pane of wrapper.findAll('[data-testid="terminal-pane"]')) {
      expect(pane.attributes('data-terminal-input-scope')).toBeDefined()
    }
  })

  it('detaches on the way back to the picker and on unmount', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="terminal-back"]').trigger('click')
    expect(session.dispose).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="terminal-session-picker"]').exists()).toBe(true)

    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()
    wrapper.unmount()
    expect(session.dispose).toHaveBeenCalledTimes(2)
  })
})
