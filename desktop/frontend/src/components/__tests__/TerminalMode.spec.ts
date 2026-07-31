import { flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createMemoryHistory } from 'vue-router'
import TerminalMode from '../TerminalMode.vue'
import { resetTerminalAvailabilityForTests } from '../../composables/useTerminalAvailability'
import { resetTerminalFontForTests } from '../../composables/useTerminalFont'
import { resetTerminalSessionsForTests } from '../../composables/useTerminalSessions'
import { resetSessionStatusesForTests } from '../../composables/useSessionStatuses'
import { setTerminalShowWindows } from '../../composables/useTerminalShowWindows'
import { resetTerminalWindowListingsForTests } from '../../composables/useTerminalWindowListings'
import { createAppRouter } from '../../router'

const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  ListSessions: vi.fn(),
  SessionStatuses: vi.fn(),
  SessionDetail: vi.fn(),
  SessionRisk: vi.fn(),
  RenameSession: vi.fn(),
  DeleteSession: vi.fn(),
  RecycleSession: vi.fn(),
  PruneSessions: vi.fn(),
  TerminalActionViews: vi.fn(),
  InvokeTerminalAction: vi.fn(),
  RenderTerminalClipboardAction: vi.fn(),
  SetClipboardText: vi.fn(),
  getTerminalEndpoint: vi.fn(),
  createTerminalClient: vi.fn(),
  useTerminalWindows: vi.fn(),
  openBlank: vi.fn(),
  SetTerminalFontSize: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice', () => ({
  Available: mocks.Available,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  AppearanceSettings: vi.fn().mockResolvedValue({ theme: '', terminalFontSize: '', terminalShowWindows: true, terminalPoolSize: 3 }),
  SetTerminalFontSize: mocks.SetTerminalFontSize,
  SetTerminalShowWindows: vi.fn(),
  SetTerminalPoolSize: vi.fn(),
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({
  ListSessions: mocks.ListSessions,
  SessionStatuses: mocks.SessionStatuses,
  SessionDetail: mocks.SessionDetail,
  SessionRisk: mocks.SessionRisk,
  RenameSession: mocks.RenameSession,
  DeleteSession: mocks.DeleteSession,
  RecycleSession: mocks.RecycleSession,
  PruneSessions: mocks.PruneSessions,
  TerminalActionViews: mocks.TerminalActionViews,
  InvokeTerminalAction: mocks.InvokeTerminalAction,
  RenderTerminalClipboardAction: mocks.RenderTerminalClipboardAction,
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
  Clipboard: { SetText: mocks.SetClipboardText },
}))

function fakeSession() {
  return {
    tabs: ref([
      { uid: 1, windowId: '@1', name: 'agent', active: true, scrolledUp: false, term: {}, fit: {} },
      { uid: 2, windowId: '@2', name: 'shell', active: false, scrolledUp: false, term: {}, fit: {} },
    ]),
    activeWindowId: ref('@1'),
    status: ref<'connecting' | 'live' | 'ended'>('live'),
    painted: ref(true),
    endReason: ref<string | null>(null),
    error: ref<string | null>(null),
    actionError: ref<string | null>(null),
    sizeConstraint: ref<{ voted: { cols: number; rows: number }; granted: { cols: number; rows: number } } | null>(null),
    dismissSizeConstraint: vi.fn(),
    search: ref({ open: false, query: '', matches: 0, index: 0 }),
    openSearch: vi.fn(),
    closeSearch: vi.fn(),
    setSearchQuery: vi.fn(),
    findNext: vi.fn(),
    findPrevious: vi.fn(),
    start: vi.fn().mockResolvedValue(undefined),
    reconnect: vi.fn().mockResolvedValue(undefined),
    select: vi.fn().mockResolvedValue(undefined),
    newWindow: vi.fn().mockResolvedValue(undefined),
    closeWindow: vi.fn().mockResolvedValue(undefined),
    rename: vi.fn().mockResolvedValue(undefined),
    moveWindow: vi.fn().mockResolvedValue(undefined),
    attachTab: vi.fn(),
    disposeTab: vi.fn(),
    focusActive: vi.fn(),
    scrollToBottom: vi.fn(),
    dispose: vi.fn(),
  }
}

// TerminalMode only renders under the terminal route in App.vue, so every
// mount starts there; tests deep-link by passing the session in the path.
// Transitions are stubbed because happy-dom never fires transitionend, which
// would leave leave-transitioned elements in the DOM past their v-if.
async function mountAt(path = '/terminal') {
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const wrapper = mount(TerminalMode, { global: { plugins: [router], stubs: { transition: true, 'transition-group': true } } })
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
    resetTerminalAvailabilityForTests()
    resetTerminalFontForTests()
    resetTerminalSessionsForTests()
    resetSessionStatusesForTests()
    resetTerminalWindowListingsForTests()
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.getTerminalEndpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 't' })
    mocks.createTerminalClient.mockReturnValue({})
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'bump deps', slug: 'hive-bump-deps', repo: 'hay-kot/hive', state: 'active' },
    ])
    mocks.SessionStatuses.mockResolvedValue({ items: [], pollIntervalMs: 60_000 })
    mocks.SessionRisk.mockResolvedValue({ uncommittedChanges: false, unpushedCommits: false, recycleDeletes: false })
    mocks.TerminalActionViews.mockResolvedValue([])
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

  it('shows session liveness on sessions and agent activity on windows with icons', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'live', slug: 'live', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'stopped', slug: 'stopped', repo: 'hay-kot/hive', state: 'active' },
    ])
    mocks.createTerminalClient.mockReturnValue({
      listWindows: vi.fn(async (slug: string) => ({
        windows: slug === 'live'
          ? [
              { windowId: '@1', name: 'working', active: true, width: 0, height: 0 },
              { windowId: '@2', name: 'approval', active: false, width: 0, height: 0 },
              { windowId: '@3', name: 'ready', active: false, width: 0, height: 0 },
              { windowId: '@4', name: 'unknown', active: false, width: 0, height: 0 },
            ]
          : [],
      })),
    })
    const liveSession = fakeSession()
    liveSession.tabs.value = [
      { uid: 1, windowId: '@1', name: 'working', active: true, scrolledUp: false, term: {}, fit: {} },
      { uid: 2, windowId: '@2', name: 'approval', active: false, scrolledUp: false, term: {}, fit: {} },
      { uid: 3, windowId: '@3', name: 'ready', active: false, scrolledUp: false, term: {}, fit: {} },
      { uid: 4, windowId: '@4', name: 'unknown', active: false, scrolledUp: false, term: {}, fit: {} },
    ]
    mocks.useTerminalWindows.mockReturnValue(liveSession)
    mocks.SessionStatuses.mockResolvedValue({
      items: [
        {
          sessionId: '1',
          running: true,
          windows: [
            { windowId: '@1', status: 'active', tool: 'pi' },
            { windowId: '@2', status: 'approval', tool: 'claude' },
            { windowId: '@3', status: 'ready', tool: 'codex' },
            { windowId: '@4', status: 'missing', tool: '' },
          ],
        },
        { sessionId: '2', running: false, windows: [] },
      ],
      pollIntervalMs: 60_000,
    })

    const { wrapper } = await mountAt()
    const liveness = wrapper.findAll('[data-testid="terminal-session-liveness"]')
    expect(liveness).toHaveLength(2)
    expect(wrapper.get('[data-testid="terminal-session-liveness"][data-status="running"]').attributes('title')).toBe('Terminal running')
    expect(wrapper.get('[data-testid="terminal-session-liveness"][data-status="running"] span[aria-hidden="true"]').classes()).toEqual(expect.arrayContaining(['size-2.5', 'rounded-full', 'bg-current']))
    expect(wrapper.find('[data-testid="terminal-session-liveness"][data-status="inactive"] svg').exists()).toBe(true)
    expect(wrapper.get('[data-testid="terminal-session-liveness"][data-status="inactive"]').classes()).toContain('text-text-4')
    const liveRow = wrapper.get('[data-testid="terminal-session-row"][data-slug="live"]')
    expect(liveRow.get('[data-testid="terminal-session-liveness"]').element.parentElement)
      .toBe(liveRow.get('[data-testid="terminal-session-menu-toggle"]').element.parentElement)

    const activity = wrapper.findAll('[data-testid="terminal-window-status"]')
    expect(activity).toHaveLength(4)
    expect(activity.every((indicator) => indicator.find('svg').exists())).toBe(true)
    expect(activity.every((indicator) => indicator.classes().includes('window-status'))).toBe(true)
    expect(wrapper.get('[data-testid="terminal-window-status"][data-status="active"]').attributes('title')).toBe('pi is working')
    expect(wrapper.get('[data-testid="terminal-window-status"][data-status="active"] svg').classes()).toContain('animate-spin')
    expect(wrapper.get('[data-testid="terminal-window-status"][data-status="approval"]').attributes('title')).toBe('claude needs approval')
    expect(wrapper.text()).not.toContain('[●]')

    await wrapper.get('[data-testid="terminal-session-row"][data-slug="live"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-testid="terminal-window-row"]')).toHaveLength(4)
    expect(wrapper.findAll('[data-testid="terminal-window-status"]')).toHaveLength(4)
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

  it('keeps the outgoing attach warm and snaps back to it without re-attaching', async () => {
    const first = fakeSession()
    const second = fakeSession()
    second.tabs.value = [{ uid: 9, windowId: '@9', name: 'other', active: true, scrolledUp: false, term: {}, fit: {} }]
    second.activeWindowId.value = '@9'
    mocks.useTerminalWindows.mockReturnValueOnce(first).mockReturnValueOnce(second)
    const { wrapper } = await mountAt()
    const rows = wrapper.findAll('[data-testid="terminal-session-row"]')
    await rows[0].trigger('click')
    await flushPromises()

    // Re-clicking the attached row must not re-attach.
    await wrapper.find('[data-testid="terminal-session-row"][data-attached="true"]').trigger('click')
    await flushPromises()
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(1)

    await rows[1].trigger('click')
    await flushPromises()
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(2)
    // The outgoing attach stays in the pool: nothing disposed, its panes still
    // mounted (hidden) beside the incoming session's, its subtree still live.
    expect(first.dispose).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="terminal-pane"]')).toHaveLength(3)
    expect(wrapper.findAll('[data-testid="terminal-tab"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="terminal-window-row"]')).toHaveLength(3)

    // Snapping back is a reveal and a refocus, not a re-attach.
    await rows[0].trigger('click')
    await flushPromises()
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(2)
    expect(first.focusActive).toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="terminal-tab"]')).toHaveLength(2)
  })

  it('holds the outgoing session on screen until the incoming one paints', async () => {
    const first = fakeSession()
    const second = fakeSession()
    second.tabs.value = []
    second.status.value = 'connecting'
    second.painted.value = false
    mocks.useTerminalWindows.mockReturnValueOnce(first).mockReturnValueOnce(second)
    const { wrapper } = await mountAt()
    const rows = wrapper.findAll('[data-testid="terminal-session-row"]')
    await rows[0].trigger('click')
    await flushPromises()

    await rows[1].trigger('click')
    await flushPromises()

    // The sidebar reflects the selection at once; the pane does not blank.
    expect(wrapper.find('[data-testid="terminal-session-row"][data-attached="true"]').attributes('data-slug')).toBe('hive-fix-parser')
    expect(wrapper.findAll('[data-testid="terminal-tab"]')).toHaveLength(2)

    // First paint is the swap signal.
    second.tabs.value = [{ uid: 9, windowId: '@9', name: 'other', active: true, scrolledUp: false, term: {}, fit: {} }]
    second.status.value = 'live'
    second.painted.value = true
    await flushPromises()
    expect(wrapper.findAll('[data-testid="terminal-tab"]')).toHaveLength(1)
    expect(second.focusActive).toHaveBeenCalled()
  })

  it('gives up the hold once the cap fires, so a slow attach is not a dead click', async () => {
    vi.useFakeTimers()
    try {
      const first = fakeSession()
      const second = fakeSession()
      second.tabs.value = []
      second.status.value = 'connecting'
      second.painted.value = false
      mocks.useTerminalWindows.mockReturnValueOnce(first).mockReturnValueOnce(second)
      const { wrapper } = await mountAt()
      const rows = wrapper.findAll('[data-testid="terminal-session-row"]')
      await rows[0].trigger('click')
      await flushPromises()

      await rows[1].trigger('click')
      await flushPromises()
      expect(wrapper.findAll('[data-testid="terminal-tab"]')).toHaveLength(2)

      await vi.advanceTimersByTimeAsync(300)
      expect(wrapper.findAll('[data-testid="terminal-tab"]')).toHaveLength(0)
      expect(wrapper.text()).toContain('Attaching…')
    } finally {
      vi.useRealTimers()
    }
  })

  it('detaches the least recently used session past the pool limit', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'aaa', slug: 'hive-aaa', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'bbb', slug: 'hive-bbb', repo: 'hay-kot/hive', state: 'active' },
      { id: '3', name: 'ccc', slug: 'hive-ccc', repo: 'hay-kot/hive', state: 'active' },
      { id: '4', name: 'ddd', slug: 'hive-ddd', repo: 'hay-kot/hive', state: 'active' },
    ])
    const sessions = [fakeSession(), fakeSession(), fakeSession(), fakeSession()]
    for (const session of sessions) mocks.useTerminalWindows.mockReturnValueOnce(session)
    const { wrapper } = await mountAt()

    for (const row of wrapper.findAll('[data-testid="terminal-session-row"]')) {
      await row.trigger('click')
      await flushPromises()
    }

    expect(sessions[0].dispose).toHaveBeenCalledTimes(1)
    expect(sessions[1].dispose).not.toHaveBeenCalled()
    expect(sessions[2].dispose).not.toHaveBeenCalled()
    expect(sessions[3].dispose).not.toHaveBeenCalled()
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

  it('re-enters from the caches and resumes without waiting on the probe', async () => {
    const listWindows = vi.fn(async () => ({ windows: [
      { windowId: '@7', name: 'agent', active: true, width: 0, height: 0 },
    ] }))
    mocks.createTerminalClient.mockReturnValue({ listWindows })
    const { wrapper } = await mountAvailable()
    expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(2)
    wrapper.unmount()

    // Neither the probe nor the listing resolves this time: everything the
    // second entry shows has to come from the caches, revalidation pending.
    mocks.Available.mockReturnValue(new Promise(() => {}))
    mocks.ListSessions.mockReturnValue(new Promise(() => {}))
    localStorage.setItem('hive.terminal.restore', JSON.stringify({ slug: 'hive-bump-deps', window: '' }))
    const session = fakeSession()
    mocks.useTerminalWindows.mockReturnValue(session)
    const { wrapper: second, router } = await mountAt()

    expect(second.find('[data-testid="terminal-session-sidebar"]').exists()).toBe(true)
    expect(second.findAll('[data-testid="terminal-session-row"]')).toHaveLength(2)
    // The refresh control is the staleness indicator while revalidation runs.
    expect(second.get('[data-testid="terminal-sessions-refresh"]').find('.animate-spin').exists()).toBe(true)
    expect(router.currentRoute.value.params.slug).toBe('hive-bump-deps')
    expect(mocks.useTerminalWindows).toHaveBeenCalledWith('hive-bump-deps', expect.anything())
    second.unmount()
  })

  it('stands in the cached listing for the tab strip and subtree while attaching', async () => {
    const listWindows = vi.fn(async () => ({ windows: [
      { windowId: '@7', name: 'agent', active: true, width: 0, height: 0 },
      { windowId: '@8', name: 'shell', active: false, width: 0, height: 0 },
    ] }))
    mocks.createTerminalClient.mockReturnValue({ listWindows })
    const session = fakeSession()
    session.tabs.value = []
    session.status.value = 'connecting'
    const { wrapper } = await mountAvailable(session)

    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    const placeholders = wrapper.findAll('[data-testid="terminal-placeholder-tab"]')
    expect(placeholders.map((tab) => tab.text())).toEqual(['agent', 'shell'])
    // The attached session's subtree keeps its cached rows too.
    expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(4)

    // The live tab set replaces the stand-ins in place.
    session.tabs.value = [
      { uid: 7, windowId: '@7', name: 'agent', active: true, scrolledUp: false, term: {}, fit: {} },
      { uid: 8, windowId: '@8', name: 'shell', active: false, scrolledUp: false, term: {}, fit: {} },
    ]
    session.status.value = 'live'
    await flushPromises()
    expect(wrapper.findAll('[data-testid="terminal-placeholder-tab"]')).toHaveLength(0)
    expect(wrapper.findAll('[data-testid="terminal-tab"]')).toHaveLength(2)
    expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(2)
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
    await tabs[1].trigger('mousedown')
    expect(session.select).toHaveBeenCalledWith('@2')

    await tabs[1].trigger('dblclick')
    const input = wrapper.get('[data-testid="terminal-rename-input"]')
    await input.setValue('build')
    await input.trigger('keydown.enter')
    expect(session.rename).toHaveBeenCalledWith('@2', 'build')
  })

  // The whole tab answers, and it answers to both halves of the press: the
  // label used to be the only target and it is the height of its own text, a
  // press that drifts 3px on a drag source is delivered as a drag with no click
  // at all, and mousedown's own default action takes focus off the pane after
  // the press has put it there.
  it('selects from the press and again from the click, and not from its close button', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    const tabs = wrapper.findAll('[data-testid="terminal-tab"]')
    await tabs[1].trigger('mousedown')
    expect(session.select).toHaveBeenCalledWith('@2')

    // The click reclaims the pane; select() on the active window is the refocus.
    session.select.mockClear()
    await tabs[1].trigger('click')
    expect(session.select).toHaveBeenCalledWith('@2')

    // Closing a window is not selecting it first, on either half of the press.
    session.select.mockClear()
    await wrapper.findAll('[data-testid="terminal-close-window"]')[0].trigger('mousedown')
    await wrapper.findAll('[data-testid="terminal-close-window"]')[0].trigger('click')
    expect(session.closeWindow).toHaveBeenCalledWith('@1')
    expect(session.select).not.toHaveBeenCalled()

    // Nor is a secondary button.
    await tabs[0].trigger('mousedown', { button: 2 })
    expect(session.select).not.toHaveBeenCalled()

    // Nor is a press inside the rename field, which would blur it and commit.
    await tabs[0].trigger('dblclick')
    await wrapper.get('[data-testid="terminal-rename-input"]').trigger('mousedown')
    expect(session.select).not.toHaveBeenCalled()
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

  it('offers to start a session tmux is not running rather than starting it on selection', async () => {
    const start = vi.fn().mockResolvedValue({ started: true })
    mocks.createTerminalClient.mockReturnValue({ start })
    const session = fakeSession()
    session.tabs.value = []
    session.status.value = 'ended'
    session.endReason.value = 'not-started'
    session.error.value = 'session "hive-fix-parser" is not running'
    const { wrapper } = await mountAvailable(session)

    await wrapper.find('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
    await flushPromises()

    // Selecting attaches and nothing more: starting runs the session's agent
    // command, so it waits for the click below.
    expect(session.start).toHaveBeenCalled()
    expect(start).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="terminal-session-not-started"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="terminal-session-ended"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="terminal-tab"]').exists()).toBe(false)

    await wrapper.get('[data-testid="terminal-start-session"]').trigger('click')
    await flushPromises()

    expect(start).toHaveBeenCalledWith('hive-fix-parser')
    expect(session.reconnect).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reports a start that failed without leaving the panel', async () => {
    const start = vi.fn().mockRejectedValue(new Error('session "hive-fix-parser" is recycled'))
    mocks.createTerminalClient.mockReturnValue({ start })
    const session = fakeSession()
    session.tabs.value = []
    session.status.value = 'ended'
    session.endReason.value = 'not-started'
    const { wrapper } = await mountAvailable(session)
    await wrapper.find('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="terminal-start-session"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="terminal-start-error"]').text()).toContain('recycled')
    expect(session.reconnect).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('starts a session from its row menu and attaches to it', async () => {
    const start = vi.fn().mockResolvedValue({ started: true })
    mocks.createTerminalClient.mockReturnValue({ start })
    const { wrapper, router } = await mountAvailable()

    await wrapper.findAll('[data-testid="terminal-session-menu-toggle"]')[0].trigger('click')
    await wrapper.get('[data-testid="session-menu-start"]').trigger('click')
    await flushPromises()

    // Started first, then selected: attaching before the spawn would only fail.
    expect(start).toHaveBeenCalledWith('hive-bump-deps')
    expect(router.currentRoute.value.params.slug).toBe('hive-bump-deps')
    wrapper.unmount()
  })

  it('kills the attached terminal behind a confirmation and re-attaches onto the start panel', async () => {
    const kill = vi.fn().mockResolvedValue({ killed: true })
    mocks.createTerminalClient.mockReturnValue({ kill })
    const { wrapper, session } = await mountAvailable()
    await wrapper.find('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
    await flushPromises()

    await wrapper.findAll('[data-testid="terminal-session-menu-toggle"]')[1].trigger('click')
    await wrapper.get('[data-testid="session-menu-kill"]').trigger('click')
    await flushPromises()

    // What it kills, and what it leaves, is the whole point of confirming it.
    const dialog = document.querySelector('[data-testid="session-confirmation"]')
    expect(dialog?.textContent).toContain('stopping the agent')
    expect(dialog?.textContent).toContain('checkout and its work are untouched')
    expect(kill).not.toHaveBeenCalled()

    document.querySelector<HTMLButtonElement>('[data-testid="session-confirmation-confirm"]')!.click()
    await flushPromises()

    expect(kill).toHaveBeenCalledWith('hive-fix-parser')
    // Re-attaching is what turns the dead session into the start panel.
    expect(session.reconnect).toHaveBeenCalled()
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

  describe('reordering tabs', () => {
    async function attached() {
      const mounted = await mountAvailable()
      await mounted.wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
      await flushPromises()
      return mounted
    }

    // happy-dom lays nothing out, so the drop edge has to be stated: a tab
    // 150px wide starting at x, and a pointer somewhere in it.
    function box(tab: DOMWrapper<Element>, left: number): void {
      (tab.element as HTMLElement).getBoundingClientRect = () =>
        ({ left, width: 150, right: left + 150, top: 0, bottom: 30, height: 30, x: left, y: 0 }) as DOMRect
    }

    async function drag(wrapper: Awaited<ReturnType<typeof attached>>['wrapper'], from: number, onto: number, clientX: number) {
      const tabs = wrapper.findAll('[data-testid="terminal-tab"]')
      box(tabs[onto], onto * 150)
      await tabs[from].trigger('dragstart', { dataTransfer: new DataTransfer() })
      await tabs[onto].trigger('dragover', { dataTransfer: new DataTransfer(), clientX })
      await tabs[onto].trigger('drop')
    }

    it('drops a tab into the gap the pointer is nearest', async () => {
      const { wrapper, session } = await attached()

      // Past the middle of the second tab: after it, which is the end.
      await drag(wrapper, 0, 1, 250)

      expect(session.moveWindow).toHaveBeenCalledWith('@1', 1)
    })

    it('drops before the tab when the pointer is on its leading half', async () => {
      const { wrapper, session } = await attached()

      await drag(wrapper, 1, 0, 20)

      expect(session.moveWindow).toHaveBeenCalledWith('@2', 0)
    })

    it('marks the gap it would drop into, and clears it when the drag ends', async () => {
      const { wrapper } = await attached()
      const tabs = wrapper.findAll('[data-testid="terminal-tab"]')
      box(tabs[1], 150)

      await tabs[0].trigger('dragstart', { dataTransfer: new DataTransfer() })
      await tabs[1].trigger('dragover', { dataTransfer: new DataTransfer(), clientX: 160 })
      expect(tabs[1].classes()).toContain('drop-before')
      expect(tabs[0].classes()).toContain('opacity-40')

      await tabs[0].trigger('dragend')
      expect(wrapper.findAll('[data-testid="terminal-tab"]')[1].classes()).not.toContain('drop-before')
    })

    // The strip and the sidebar's window well read the same list, so the order
    // cannot disagree between them.
    it('renders the tab strip and the sidebar tree in the order the tabs carry', async () => {
      const { wrapper, session } = await attached()
      expect(wrapper.findAll('[data-testid="terminal-tab"]').map((tab) => tab.text())).toEqual(['agent', 'shell'])

      session.tabs.value = [session.tabs.value[1], session.tabs.value[0]]
      await flushPromises()

      expect(wrapper.findAll('[data-testid="terminal-tab"]').map((tab) => tab.text())).toEqual(['shell', 'agent'])
      expect(wrapper.findAll('[data-testid="terminal-window-row"]').map((row) => row.attributes('data-window-id')))
        .toEqual(['@2', '@1'])
    })

    it('leaves a tab being renamed undraggable so its text can be selected', async () => {
      const { wrapper } = await attached()
      const tabs = wrapper.findAll('[data-testid="terminal-tab"]')
      expect(tabs[1].attributes('draggable')).toBe('true')

      await tabs[1].trigger('dblclick')

      expect(wrapper.findAll('[data-testid="terminal-tab"]')[1].attributes('draggable')).toBe('false')
    })

    // A tab dropped where it already is has not moved: both of its own edges
    // name the gap it fills, and reading either as an insertion put it last.
    it('does not move a tab dropped back onto itself', async () => {
      const { wrapper, session } = await attached()

      await drag(wrapper, 0, 0, 20)

      expect(session.moveWindow).not.toHaveBeenCalled()
      expect(wrapper.findAll('[data-testid="terminal-tab"]')[0].classes()).not.toContain('drop-after')
    })

    // The click a tab hands focus back to the pane on is the one a drag eats.
    it('puts focus back in the pane when a tab drag ends', async () => {
      const { wrapper, session } = await attached()
      const tabs = wrapper.findAll('[data-testid="terminal-tab"]')

      // Attaching focuses the pane on its own; this is about the drag.
      session.focusActive.mockClear()
      await tabs[0].trigger('dragstart', { dataTransfer: new DataTransfer() })
      await tabs[0].trigger('dragend')
      expect(session.focusActive).toHaveBeenCalled()

      // The sidebar is not the pane's own strip, so a drag there leaves focus
      // where the user put it.
      session.focusActive.mockClear()
      const slots = wrapper.findAll('[data-testid="terminal-window-slot"]')
      await slots[0].trigger('dragstart', { dataTransfer: new DataTransfer() })
      await slots[0].trigger('dragend')
      expect(session.focusActive).not.toHaveBeenCalled()
    })
  })

  // The sidebar's window well reorders the same order the strip does, so the
  // two cannot disagree — and a window can only land in the session it left.
  describe('reordering windows in the sidebar', () => {
    async function attached() {
      const mounted = await mountAvailable()
      await mounted.wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
      await flushPromises()
      return mounted
    }

    // happy-dom lays nothing out, so the drop edge has to be stated: a row 28px
    // tall at top, and a pointer somewhere in it.
    function box(slot: DOMWrapper<Element>, top: number): void {
      (slot.element as HTMLElement).getBoundingClientRect = () =>
        ({ left: 0, width: 220, right: 220, top, bottom: top + 28, height: 28, x: 0, y: top }) as DOMRect
    }

    it('drops a window into the gap the pointer is nearest', async () => {
      const { wrapper, session } = await attached()
      const slots = wrapper.findAll('[data-testid="terminal-window-slot"]')
      box(slots[1], 28)

      await slots[0].trigger('dragstart', { dataTransfer: new DataTransfer() })
      // Past the middle of the second row: after it, which is the end.
      await slots[1].trigger('dragover', { dataTransfer: new DataTransfer(), clientY: 50 })
      expect(wrapper.findAll('[data-testid="terminal-window-slot"]')[1].classes()).toContain('drop-after')

      await slots[1].trigger('drop')
      expect(session.moveWindow).toHaveBeenCalledWith('@1', 1)
    })

    // Both surfaces draw the same windows, so an unscoped mark would light up a
    // tab for a drag happening in the tree.
    it('marks only the surface the drag is in', async () => {
      const { wrapper } = await attached()
      const slots = wrapper.findAll('[data-testid="terminal-window-slot"]')
      box(slots[1], 28)

      await slots[0].trigger('dragstart', { dataTransfer: new DataTransfer() })
      await slots[1].trigger('dragover', { dataTransfer: new DataTransfer(), clientY: 50 })

      const tabs = wrapper.findAll('[data-testid="terminal-tab"]')
      expect(tabs[1].classes()).not.toContain('drop-after')
      expect(tabs[0].classes()).not.toContain('opacity-40')
    })

    it('refuses a window dragged into another session', async () => {
      const { wrapper, session } = await attached()
      // Both sessions pooled, so both subtrees carry live rows.
      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-bump-deps"]').trigger('click')
      await flushPromises()

      const slots = wrapper.findAll('[data-testid="terminal-window-slot"]')
      expect(slots).toHaveLength(4)
      box(slots[2], 100)

      await slots[0].trigger('dragstart', { dataTransfer: new DataTransfer() })
      await slots[2].trigger('dragover', { dataTransfer: new DataTransfer(), clientY: 120 })
      expect(wrapper.findAll('[data-testid="terminal-window-slot"]')[2].classes()).not.toContain('drop-after')

      await slots[2].trigger('drop')
      expect(session.moveWindow).not.toHaveBeenCalled()
    })

    // Moving a window is a control-client operation, so a session that is only
    // listed has nothing to move it with.
    it('leaves a listed window undraggable', async () => {
      mocks.createTerminalClient.mockReturnValue({
        listWindows: vi.fn(async (slug: string) => (slug === 'hive-bump-deps'
          ? { windows: [{ windowId: '@7', name: 'agent', active: true, width: 0, height: 0 }] }
          : { windows: [] })),
      })
      const { wrapper } = await attached()

      const slots = wrapper.findAll('[data-testid="terminal-window-slot"]')
      expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(1)
      expect(slots[0].attributes('draggable')).toBe('false')
      expect(slots[1].attributes('draggable')).toBe('true')
    })
  })

  describe('terminal overflow menu', () => {
    async function attached() {
      const mounted = await mountAvailable()
      await mounted.wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
      await flushPromises()
      return mounted
    }

    async function openMenu(wrapper: Awaited<ReturnType<typeof attached>>['wrapper']) {
      await wrapper.get('[data-testid="terminal-view-menu-toggle"]').trigger('click')
      return wrapper.get('[data-testid="terminal-view-menu"]')
    }

    it('opens from the tab strip and names the size it is on', async () => {
      const { wrapper } = await attached()

      const toggle = wrapper.get('[data-testid="terminal-view-menu-toggle"]')
      expect(toggle.attributes('aria-label')).toBe('Terminal options')
      expect(toggle.attributes('aria-haspopup')).toBe('menu')
      expect(toggle.attributes('aria-expanded')).toBe('false')
      expect(wrapper.find('[data-testid="terminal-view-menu"]').exists()).toBe(false)

      const menu = await openMenu(wrapper)

      expect(toggle.attributes('aria-expanded')).toBe('true')
      expect(menu.attributes('role')).toBe('menu')
      expect(menu.findAll('[role="menuitem"]').map((entry) => entry.text())).toEqual([
        'Decrease', 'Increase', 'Reset',
      ])
      expect(menu.text()).toContain('Text size · Medium')
      wrapper.unmount()
    })

    // Nothing to tune with no pane on screen.
    it('is absent until a session is attached', async () => {
      const { wrapper } = await mountAvailable()

      expect(wrapper.find('[data-testid="terminal-view-menu-toggle"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('steps the size through the appearance setting, one preset per click', async () => {
      const { wrapper } = await attached()
      const menu = await openMenu(wrapper)

      await menu.get('[data-testid="terminal-text-size-increase"]').trigger('click')
      await flushPromises()
      expect(mocks.SetTerminalFontSize).toHaveBeenLastCalledWith('large')
      // The menu stays open so the next notch is one click away.
      expect(wrapper.get('[data-testid="terminal-view-menu"]').text()).toContain('Text size · Large')

      await menu.get('[data-testid="terminal-text-size-decrease"]').trigger('click')
      await flushPromises()

      expect(mocks.SetTerminalFontSize.mock.calls.map(([size]) => size)).toEqual(['large', 'medium'])
      wrapper.unmount()
    })

    it('offers no step past the end of the ladder', async () => {
      const { wrapper } = await attached()
      const menu = await openMenu(wrapper)

      for (let step = 0; step < 2; step++) {
        await menu.get('[data-testid="terminal-text-size-decrease"]').trigger('click')
      }
      await flushPromises()

      expect(menu.text()).toContain('Text size · Small')
      expect(menu.get('[data-testid="terminal-text-size-decrease"]').attributes('disabled')).toBeDefined()
      expect(menu.get('[data-testid="terminal-text-size-increase"]').attributes('disabled')).toBeUndefined()
      // Two moves, not three: the third click had nowhere to go.
      expect(mocks.SetTerminalFontSize.mock.calls.map(([size]) => size)).toEqual(['small'])
      wrapper.unmount()
    })

    it('resets to the default size, and offers nothing to reset once there', async () => {
      const { wrapper } = await attached()
      const menu = await openMenu(wrapper)
      expect(menu.get('[data-testid="terminal-text-size-reset"]').attributes('disabled')).toBeDefined()

      await menu.get('[data-testid="terminal-text-size-increase"]').trigger('click')
      await flushPromises()
      expect(menu.get('[data-testid="terminal-text-size-reset"]').attributes('disabled')).toBeUndefined()

      await menu.get('[data-testid="terminal-text-size-reset"]').trigger('click')
      await flushPromises()

      expect(menu.text()).toContain('Text size · Medium')
      expect(mocks.SetTerminalFontSize.mock.calls.map(([size]) => size)).toEqual(['large', 'medium'])
      wrapper.unmount()
    })
  })

  describe('configured terminal actions', () => {
    const openInZed = { id: 'open-in-zed', label: 'Open in Zed', type: 'shell', showInDetail: false, requiresSessionInput: false }
    const interrupt = { id: 'interrupt', label: 'Interrupt', type: 'shell', showInDetail: false, requiresSessionInput: false }

    function offer(session: unknown[], window: unknown[] = []) {
      mocks.TerminalActionViews.mockImplementation(async (target: string) => (target === 'session' ? session : window))
    }

    it('offers session-targeted actions in the session row menu and runs one against its slug', async () => {
      offer([openInZed])
      const { wrapper } = await mountAvailable()

      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"] [data-testid="terminal-session-menu-toggle"]').trigger('click')
      await wrapper.get('[data-testid="terminal-action-open-in-zed"]').trigger('click')
      await flushPromises()

      expect(mocks.InvokeTerminalAction).toHaveBeenCalledWith('open-in-zed', { slug: 'hive-fix-parser', windowId: '' }, {})
      wrapper.unmount()
    })

    it('gives a window row its own menu, carrying the window id', async () => {
      offer([], [interrupt])
      const { wrapper } = await mountAvailable()
      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
      await flushPromises()

      const window = wrapper.get('[data-testid="terminal-window-row"][data-window-id="@2"]')
      await window.get('[data-testid="terminal-window-menu-toggle"]').trigger('click')
      await window.get('[data-testid="terminal-action-interrupt"]').trigger('click')
      await flushPromises()

      expect(mocks.InvokeTerminalAction).toHaveBeenCalledWith('interrupt', { slug: 'hive-fix-parser', windowId: '@2' }, {})
      wrapper.unmount()
    })

    // A window row has no operations of its own, so an empty menu would be an
    // affordance that does nothing.
    it('grows no window menu when nothing targets a window', async () => {
      offer([openInZed])
      const { wrapper } = await mountAvailable()
      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
      await flushPromises()

      expect(wrapper.find('[data-testid="terminal-window-menu-toggle"]').exists()).toBe(false)
      wrapper.unmount()
    })

    it('collects declared inputs before running', async () => {
      offer([{ ...openInZed, inputs: [{ name: 'branch', label: 'Branch', type: 'text', required: true, default: '', placeholder: '', options: [] }] }])
      const { wrapper } = await mountAvailable()

      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"] [data-testid="terminal-session-menu-toggle"]').trigger('click')
      await wrapper.get('[data-testid="terminal-action-open-in-zed"]').trigger('click')
      await flushPromises()
      expect(mocks.InvokeTerminalAction).not.toHaveBeenCalled()

      const field = document.querySelector<HTMLInputElement>('[data-testid="action-inputs-dialog"] input')!
      field.value = 'main'
      field.dispatchEvent(new Event('input'))
      await flushPromises()
      document.querySelector<HTMLButtonElement>('[data-testid="action-inputs-submit"]')!.click()
      await flushPromises()

      expect(mocks.InvokeTerminalAction).toHaveBeenCalledWith('open-in-zed', { slug: 'hive-fix-parser', windowId: '' }, { branch: 'main' })
      wrapper.unmount()
    })

    // Text is not a side effect: a clipboard action renders and copies rather
    // than starting a job, the same split the detail pane makes.
    it('copies a clipboard action instead of running it', async () => {
      offer([{ id: 'copy-path', label: 'Copy path', type: 'clipboard', showInDetail: false, requiresSessionInput: false }])
      mocks.RenderTerminalClipboardAction.mockResolvedValue('/work/hive-fix-parser')
      const { wrapper } = await mountAvailable()

      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"] [data-testid="terminal-session-menu-toggle"]').trigger('click')
      await wrapper.get('[data-testid="terminal-action-copy-path"]').trigger('click')
      await flushPromises()

      expect(mocks.RenderTerminalClipboardAction).toHaveBeenCalledWith('copy-path', { slug: 'hive-fix-parser', windowId: '' }, {})
      expect(mocks.SetClipboardText).toHaveBeenCalledWith('/work/hive-fix-parser')
      expect(mocks.InvokeTerminalAction).not.toHaveBeenCalled()
      wrapper.unmount()
    })
  })
})
