import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createMemoryHistory } from 'vue-router'
import TerminalMode from '../TerminalMode.vue'
import { setTerminalShowWindows } from '../../composables/useTerminalShowWindows'
import { createAppRouter } from '../../router'

const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  ListSessions: vi.fn(),
  SessionDetail: vi.fn(),
  SessionRisk: vi.fn(),
  RenameSession: vi.fn(),
  DeleteSession: vi.fn(),
  RecycleSession: vi.fn(),
  PruneSessions: vi.fn(),
  getTerminalEndpoint: vi.fn(),
  createTerminalClient: vi.fn(),
  useTerminalWindows: vi.fn(),
  openBlank: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice', () => ({
  Available: mocks.Available,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  AppearanceSettings: vi.fn().mockResolvedValue({ theme: '', terminalFontSize: '', terminalShowWindows: true }),
  SetTerminalShowWindows: vi.fn(),
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({
  ListSessions: mocks.ListSessions,
  SessionDetail: mocks.SessionDetail,
  SessionRisk: mocks.SessionRisk,
  RenameSession: mocks.RenameSession,
  DeleteSession: mocks.DeleteSession,
  RecycleSession: mocks.RecycleSession,
  PruneSessions: mocks.PruneSessions,
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
      { uid: 1, windowId: '@1', name: 'agent', active: true, scrolledUp: false, term: {}, fit: {} },
      { uid: 2, windowId: '@2', name: 'shell', active: false, scrolledUp: false, term: {}, fit: {} },
    ]),
    activeWindowId: ref('@1'),
    status: ref<'connecting' | 'live' | 'ended'>('live'),
    endReason: ref<string | null>(null),
    error: ref<string | null>(null),
    actionError: ref<string | null>(null),
    sizeConstraint: ref<{ voted: { cols: number; rows: number }; granted: { cols: number; rows: number } } | null>(null),
    dismissSizeConstraint: vi.fn(),
    start: vi.fn().mockResolvedValue(undefined),
    reconnect: vi.fn().mockResolvedValue(undefined),
    select: vi.fn().mockResolvedValue(undefined),
    newWindow: vi.fn().mockResolvedValue(undefined),
    closeWindow: vi.fn().mockResolvedValue(undefined),
    rename: vi.fn().mockResolvedValue(undefined),
    attachTab: vi.fn(),
    disposeTab: vi.fn(),
    focusActive: vi.fn(),
    scrollToBottom: vi.fn(),
    dispose: vi.fn(),
  }
}

// TerminalMode only renders under the terminal route in App.vue, so every
// mount starts there; tests deep-link by passing the session in the path.
async function mountAt(path = '/terminal') {
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const wrapper = mount(TerminalMode, { global: { plugins: [router] } })
  await flushPromises()
  return { wrapper, router }
}

async function mountAvailable(session = fakeSession()) {
  mocks.useTerminalWindows.mockReturnValue(session)
  const { wrapper, router } = await mountAt()
  return { wrapper, router, session }
}

function storedRestore() {
  return JSON.parse(localStorage.getItem('hive.terminal.restore') ?? 'null')
}

describe('TerminalMode', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.getTerminalEndpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 't' })
    mocks.createTerminalClient.mockReturnValue({})
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'bump deps', slug: 'hive-bump-deps', repo: 'hay-kot/hive', state: 'active' },
    ])
    mocks.SessionRisk.mockResolvedValue({ uncommittedChanges: false, unpushedCommits: false, recycleDeletes: false })
  })

  it('renders the unavailable panel with the reason instead of gating the mode', async () => {
    mocks.Available.mockResolvedValue({ available: false, reason: 'tmux is not installed.' })

    const { wrapper } = await mountAt()

    const panel = wrapper.find('[data-testid="terminal-unavailable"]')
    expect(panel.exists()).toBe(true)
    expect(wrapper.get('[data-testid="terminal-unavailable-reason"]').text()).toBe('tmux is not installed.')
    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(false)
  })

  it('treats an endpoint failure as unavailable and can retry', async () => {
    mocks.getTerminalEndpoint.mockRejectedValueOnce(Object.assign(new Error('call failed'), {
      cause: { kind: 'unavailable', message: 'The local HTTP server is not running.' },
    }))

    const { wrapper } = await mountAt()
    expect(wrapper.get('[data-testid="terminal-unavailable-reason"]').text()).toBe('The local HTTP server is not running.')

    mocks.useTerminalWindows.mockReturnValue(fakeSession())
    await wrapper.get('[data-testid="terminal-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(true)
  })

  it('groups sessions by repo in the sidebar and attaches to the one picked', async () => {
    const session = fakeSession()
    mocks.useTerminalWindows.mockReturnValue(session)
    const { wrapper, router } = await mountAt()

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

    // The row navigates; the route is what attaches.
    expect(router.currentRoute.value.params.slug).toBe('hive-fix-parser')
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
    await flushPromises()
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(1)

    await wrapper.find('[data-testid="terminal-session-row"][data-attached="false"]').trigger('click')
    await flushPromises()
    expect(session.dispose).toHaveBeenCalledTimes(1)
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(2)
  })

  it('refocuses the terminal when the attached row is reselected', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    await wrapper.find('[data-testid="terminal-session-row"][data-attached="true"]').trigger('click')
    await flushPromises()

    expect(session.focusActive).toHaveBeenCalled()
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(1)
  })

  it('offers a way back to the live tail while the viewport is scrolled up', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="terminal-scroll-to-bottom"]').exists()).toBe(false)

    session.tabs.value[0].scrolledUp = true
    await flushPromises()
    await wrapper.get('[data-testid="terminal-scroll-to-bottom"]').trigger('click')
    expect(session.scrollToBottom).toHaveBeenCalled()

    // Only the active window's viewport matters — and the ended overlay
    // replaces the pane, so the pill goes with it.
    session.activeWindowId.value = '@2'
    await flushPromises()
    expect(wrapper.find('[data-testid="terminal-scroll-to-bottom"]').exists()).toBe(false)

    session.activeWindowId.value = '@1'
    session.status.value = 'ended'
    await flushPromises()
    expect(wrapper.find('[data-testid="terminal-scroll-to-bottom"]').exists()).toBe(false)
  })

  it('lists windows for unattached sessions out of the box', async () => {
    const listWindows = vi.fn(async (slug: string) => (slug === 'hive-bump-deps'
      ? { windows: [
          { windowId: '@7', name: 'agent', active: true, width: 0, height: 0 },
          { windowId: '@8', name: 'shell', active: false, width: 0, height: 0 },
        ] }
      : { windows: [] }))
    mocks.createTerminalClient.mockReturnValue({ listWindows })
    const session = fakeSession()
    session.tabs.value = [
      { uid: 7, windowId: '@7', name: 'agent', active: true, scrolledUp: false, term: {}, fit: {} },
      { uid: 8, windowId: '@8', name: 'shell', active: false, scrolledUp: false, term: {}, fit: {} },
    ]
    session.activeWindowId.value = '@7'
    const { wrapper, router } = await mountAvailable(session)

    // Settings ▸ Appearance ▸ Terminal ships the listing on, so the tree fills
    // in without touching anything.
    expect(listWindows).toHaveBeenCalledWith('hive-bump-deps')
    expect(listWindows).toHaveBeenCalledWith('hive-fix-parser')
    const rows = wrapper.findAll('[data-testid="terminal-listed-window-row"]')
    expect(rows.map((row) => row.text())).toEqual(['agent', 'shell'])

    // A listed window is a deep link: attach the session with that window
    // pinned, exactly like entering /terminal/:slug?window=….
    await rows[1].trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.params.slug).toBe('hive-bump-deps')
    expect(session.select).toHaveBeenCalledWith('@8')

    // Attached now, so its rows are the live tab set, not the listing.
    expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(0)
    expect(wrapper.findAll('[data-testid="terminal-window-row"]')).toHaveLength(2)
  })

  it('empties the tree of listed windows when the setting is turned off', async () => {
    const listWindows = vi.fn(async () => ({ windows: [
      { windowId: '@7', name: 'agent', active: true, width: 0, height: 0 },
    ] }))
    mocks.createTerminalClient.mockReturnValue({ listWindows })
    const { wrapper } = await mountAvailable()
    expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(2)

    setTerminalShowWindows(false)
    await flushPromises()
    expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(0)

    // The setting is a module singleton; put the default back for later tests.
    setTerminalShowWindows(true)
  })

  it('says when there are no sessions to attach to', async () => {
    mocks.ListSessions.mockResolvedValue([])
    const { wrapper } = await mountAt()

    expect(wrapper.find('[data-testid="terminal-sessions-empty"]').exists()).toBe(true)
  })

  it('resumes the last attached session and window when entering bare', async () => {
    localStorage.setItem('hive.terminal.restore', JSON.stringify({ slug: 'hive-bump-deps', window: '@2' }))
    const { wrapper, router, session } = await mountAvailable()
    await flushPromises()

    expect(router.currentRoute.value.params.slug).toBe('hive-bump-deps')
    expect(mocks.useTerminalWindows).toHaveBeenCalledWith('hive-bump-deps', expect.anything())
    expect(session.select).toHaveBeenCalledWith('@2')
    expect(wrapper.find('[data-testid="terminal-no-session"]').exists()).toBe(false)
  })

  it('forgets a remembered session that no longer exists and offers the picker', async () => {
    localStorage.setItem('hive.terminal.restore', JSON.stringify({ slug: 'hive-long-gone', window: '@1' }))
    const { wrapper, router } = await mountAt()

    expect(mocks.useTerminalWindows).not.toHaveBeenCalled()
    expect(router.currentRoute.value.params.slug ?? '').toBe('')
    expect(wrapper.find('[data-testid="terminal-no-session"]').exists()).toBe(true)
    expect(storedRestore()).toEqual({ slug: '', window: '' })
  })

  it('attaches straight from a session deep link', async () => {
    const session = fakeSession()
    mocks.useTerminalWindows.mockReturnValue(session)
    await mountAt('/terminal/hive-fix-parser')

    expect(mocks.useTerminalWindows).toHaveBeenCalledWith('hive-fix-parser', expect.anything())
    expect(session.start).toHaveBeenCalled()
  })

  it('falls back to tmux’s active window when the deep-linked one is gone', async () => {
    const session = fakeSession()
    mocks.useTerminalWindows.mockReturnValue(session)
    await mountAt('/terminal/hive-fix-parser?window=@9')

    expect(session.select).not.toHaveBeenCalled()
  })

  it('mirrors the active window into the URL and the resume snapshot', async () => {
    const session = fakeSession()
    mocks.useTerminalWindows.mockReturnValue(session)
    const { router } = await mountAt('/terminal/hive-fix-parser')
    await flushPromises()

    expect(router.currentRoute.value.query.window).toBe('@1')

    session.activeWindowId.value = '@2'
    await flushPromises()

    expect(router.currentRoute.value.query.window).toBe('@2')
    expect(storedRestore()).toEqual({ slug: 'hive-fix-parser', window: '@2' })
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

  // The grid not matching the pane is tmux's rule, not a fault, so it is named
  // in place rather than reported as an error.
  it('names the client that is deciding the size, and can be dismissed', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="terminal-size-constraint"]').exists()).toBe(false)

    session.sizeConstraint.value = { voted: { cols: 213, rows: 55 }, granted: { cols: 80, rows: 24 } }
    await flushPromises()

    const hint = wrapper.get('[data-testid="terminal-size-constraint"]').text()
    expect(hint).toContain('80×24')
    expect(hint).toContain('213×55')
    expect(hint).toContain('another attached')

    await wrapper.get('[data-testid="terminal-size-constraint-dismiss"]').trigger('click')
    expect(session.dismissSizeConstraint).toHaveBeenCalled()
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
    const { wrapper, router, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    session.status.value = 'ended'
    await flushPromises()
    await wrapper.get('[data-testid="terminal-close-session"]').trigger('click')
    await flushPromises()
    expect(session.dispose).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="terminal-no-session"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(true)
    // Closing on purpose forgets the session: the route drops the slug and
    // the next bare entry must not re-attach it.
    expect(router.currentRoute.value.params.slug ?? '').toBe('')
    expect(storedRestore()).toEqual({ slug: '', window: '' })

    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()
    wrapper.unmount()
    expect(session.dispose).toHaveBeenCalledTimes(2)
  })

  it('keeps sessions that cannot be attached out of the tree', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'old work', slug: 'hive-old-work', repo: 'hay-kot/hive', state: 'recycled' },
      { id: '3', name: 'broken', slug: 'hive-broken', repo: 'hay-kot/hive', state: 'corrupted' },
    ])
    const { wrapper } = await mountAvailable()

    // The tree is the attach surface, and only an active session has a tmux
    // session behind it. Prune is how the other two are dealt with.
    const rows = wrapper.findAll('[data-testid="terminal-session-row"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].attributes('data-slug')).toBe('hive-fix-parser')
    expect(wrapper.get('[data-testid="terminal-repo-group"]').text()).toContain('1')
  })

  it('reads a session on demand from its row menu', async () => {
    mocks.SessionDetail.mockResolvedValue({
      id: '2', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active',
      path: '/tmp/hive-fix-parser', cloneStrategy: 'full', worktreeBranch: '', tags: null,
      createdAt: '2026-07-01T00:00:00Z', updatedAt: '2026-07-02T00:00:00Z',
    })
    const { wrapper } = await mountAvailable()

    await wrapper.findAll('[data-testid="terminal-session-menu-toggle"]')[1].trigger('click')
    await wrapper.get('[data-testid="session-menu-detail"]').trigger('click')
    await flushPromises()

    expect(document.querySelector('[data-testid="session-detail-path"]')?.textContent).toBe('/tmp/hive-fix-parser')
    wrapper.unmount()
  })

  it('confirms a delete against the session\u2019s own risk before starting the job', async () => {
    mocks.SessionRisk.mockResolvedValue({ uncommittedChanges: true, unpushedCommits: true, recycleDeletes: false })
    const { wrapper } = await mountAvailable()

    await wrapper.findAll('[data-testid="terminal-session-menu-toggle"]')[0].trigger('click')
    await wrapper.get('[data-testid="session-menu-delete"]').trigger('click')
    await flushPromises()

    const dialog = document.querySelector('[data-testid="session-confirmation"]')
    expect(dialog?.textContent).toContain('uncommitted changes and unpushed commits')
    expect(mocks.DeleteSession).not.toHaveBeenCalled()

    document.querySelector<HTMLButtonElement>('[data-testid="session-confirmation-confirm"]')!.click()
    await flushPromises()
    expect(mocks.DeleteSession).toHaveBeenCalledWith('2')
    wrapper.unmount()
  })

  it('recycles from the row menu, behind the same confirmation', async () => {
    const { wrapper } = await mountAvailable()

    await wrapper.findAll('[data-testid="terminal-session-menu-toggle"]')[0].trigger('click')
    await wrapper.get('[data-testid="session-menu-recycle"]').trigger('click')
    await flushPromises()

    expect(document.querySelector('[data-testid="session-confirmation"]')?.textContent).toContain('resets its clone')
    document.querySelector<HTMLButtonElement>('[data-testid="session-confirmation-confirm"]')!.click()
    await flushPromises()
    expect(mocks.RecycleSession).toHaveBeenCalledWith('2')
    wrapper.unmount()
  })

  it('re-attaches under the new slug when the attached session is renamed', async () => {
    mocks.RenameSession.mockResolvedValue({ id: '1', name: 'parse it', slug: 'parse-it', repo: 'hay-kot/hive', state: 'active' })
    const { wrapper, router } = await mountAvailable()
    await wrapper.find('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
    await flushPromises()

    await wrapper.findAll('[data-testid="terminal-session-menu-toggle"]')[1].trigger('click')
    await wrapper.get('[data-testid="session-menu-rename"]').trigger('click')
    await flushPromises()

    const input = document.querySelector<HTMLInputElement>('[data-testid="session-rename-input"]')!
    input.value = 'parse it'
    input.dispatchEvent(new Event('input'))
    // The list still holds the old slug at this point, which is what lets the
    // rename recognise the renamed session as the attached one.
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'parse it', slug: 'parse-it', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'bump deps', slug: 'hive-bump-deps', repo: 'hay-kot/hive', state: 'active' },
    ])
    document.querySelector<HTMLButtonElement>('[data-testid="session-rename-save"]')!.click()
    await flushPromises()

    expect(mocks.RenameSession).toHaveBeenCalledWith('1', 'parse it')
    // The slug is the tmux target, so the attach has to follow it.
    expect(router.currentRoute.value.params.slug).toBe('parse-it')
    expect(mocks.useTerminalWindows).toHaveBeenLastCalledWith('parse-it', expect.anything())
    wrapper.unmount()
  })

  it('prunes from the sidebar header, naming how many sessions go', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'old work', slug: 'hive-old-work', repo: 'hay-kot/hive', state: 'recycled' },
      { id: '3', name: 'broken', slug: 'hive-broken', repo: 'hay-kot/hive', state: 'corrupted' },
    ])
    const { wrapper } = await mountAvailable()

    await wrapper.get('[data-testid="terminal-sessions-menu-toggle"]').trigger('click')
    const prune = wrapper.get('[data-testid="terminal-sessions-prune"]')
    expect(prune.text()).toContain('Prune 2 recycled')
    await prune.trigger('click')
    await flushPromises()

    expect(document.querySelector('[data-testid="session-confirmation"]')?.textContent).toContain('All 2 recycled and corrupted sessions')
    document.querySelector<HTMLButtonElement>('[data-testid="session-confirmation-confirm"]')!.click()
    await flushPromises()
    expect(mocks.PruneSessions).toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([
    ['deleted', [{ id: '2', name: 'bump deps', slug: 'hive-bump-deps', repo: 'hay-kot/hive', state: 'active' }]],
    ['recycled', [
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'recycled' },
      { id: '2', name: 'bump deps', slug: 'hive-bump-deps', repo: 'hay-kot/hive', state: 'active' },
    ]],
  ])('closes the attach when the attached session is %s', async (_case, listing) => {
    const { wrapper, router, session } = await mountAvailable()
    await wrapper.find('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
    await flushPromises()

    // Delete and recycle finish as jobs, so a jobs:updated reload is how the
    // frontend learns the session it was attached to can no longer be attached.
    mocks.ListSessions.mockResolvedValue(listing)
    await wrapper.get('[data-testid="terminal-sessions-refresh"]').trigger('click')
    await flushPromises()

    expect(session.dispose).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="terminal-no-session"]').exists()).toBe(true)
    expect(router.currentRoute.value.params.slug ?? '').toBe('')
    expect(storedRestore()).toEqual({ slug: '', window: '' })
  })
})
