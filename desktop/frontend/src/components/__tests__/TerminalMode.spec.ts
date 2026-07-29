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
  openBlank: vi.fn(),
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
vi.mock('../../composables/useNewSession', () => ({
  useNewSession: () => ({ openBlank: mocks.openBlank, prefetch: vi.fn() }),
}))
vi.mock('@wailsio/runtime', () => ({
  Events: { On: vi.fn().mockReturnValue(() => {}) },
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
    localStorage.clear()
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
    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(false)
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

    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(true)
  })

  it('groups sessions by repo in the sidebar and attaches to the one picked', async () => {
    const session = fakeSession()
    mocks.useTerminalWindows.mockReturnValue(session)
    const wrapper = mount(TerminalMode)
    await flushPromises()

    expect(wrapper.find('[data-testid="terminal-no-session"]').exists()).toBe(true)
    const groups = wrapper.findAll('[data-testid="terminal-repo-group"]')
    expect(groups).toHaveLength(1)
    expect(groups[0].text()).toContain('hay-kot/hive')

    const rows = wrapper.findAll('[data-testid="terminal-session-row"]')
    expect(rows).toHaveLength(2)
    // Alphabetical within a group, like the TUI tree.
    expect(rows[0].text()).toContain('bump deps')

    await rows[1].trigger('click')
    await flushPromises()

    expect(mocks.useTerminalWindows).toHaveBeenCalledWith('hive-fix-parser', expect.anything())
    expect(session.start).toHaveBeenCalled()
    // The sidebar stays; the picked row is marked attached.
    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="terminal-no-session"]').exists()).toBe(false)
    expect(rows[1].attributes('data-attached')).toBe('true')
    expect(wrapper.findAll('[data-testid="terminal-tab"]').map((tab) => tab.text())).toEqual(['agent', 'shell'])
    expect(wrapper.findAll('[data-testid="terminal-pane"]')).toHaveLength(2)
  })

  it('opens the new-session dialog from the sidebar header', async () => {
    const { wrapper } = await mountAvailable()
    await wrapper.get('[data-testid="terminal-new-session"]').trigger('click')
    expect(mocks.openBlank).toHaveBeenCalledOnce()
  })

  it('collapses a repo group without losing the attached session', async () => {
    const { wrapper } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="terminal-repo-group"]').trigger('click')
    expect(wrapper.findAll('[data-testid="terminal-session-row"]')).toHaveLength(0)
    expect(wrapper.findAll('[data-testid="terminal-pane"]')).toHaveLength(2)

    await wrapper.get('[data-testid="terminal-repo-group"]').trigger('click')
    expect(wrapper.findAll('[data-testid="terminal-session-row"]')).toHaveLength(2)
  })

  it('nests the attached session’s windows in the tree and selects from them', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    const windows = wrapper.findAll('[data-testid="terminal-window-row"]')
    expect(windows).toHaveLength(2)
    expect(windows[0].attributes('data-active')).toBe('true')

    await windows[1].trigger('click')
    expect(session.select).toHaveBeenCalledWith('@2')
  })

  it('switches sessions by disposing the old attach before the new one', async () => {
    const { wrapper, session } = await mountAvailable()
    const rows = wrapper.findAll('[data-testid="terminal-session-row"]')
    await rows[0].trigger('click')
    await flushPromises()

    // Re-clicking the attached row must not re-attach.
    await wrapper.find('[data-testid="terminal-session-row"][data-attached="true"]').trigger('click')
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(1)

    await wrapper.find('[data-testid="terminal-session-row"][data-attached="false"]').trigger('click')
    await flushPromises()
    expect(session.dispose).toHaveBeenCalledTimes(1)
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(2)
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

  it('re-attaches from the sidebar row after the session ended', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    session.status.value = 'ended'
    await flushPromises()

    await wrapper.find('[data-testid="terminal-session-row"][data-attached="true"]').trigger('click')
    await flushPromises()
    expect(session.dispose).toHaveBeenCalledTimes(1)
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(2)
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

  it('detaches when the ended overlay closes the session and on unmount', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    session.status.value = 'ended'
    await flushPromises()
    await wrapper.get('[data-testid="terminal-close-session"]').trigger('click')
    expect(session.dispose).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="terminal-no-session"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(true)

    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()
    wrapper.unmount()
    expect(session.dispose).toHaveBeenCalledTimes(2)
  })
})
