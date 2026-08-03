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
import { focusTerminalFilter, paneMayAutoFocus, selectTerminalWindow } from '../../lib/terminalTree'
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

// The sidebar sweeps every session in one call, so a listing mock answers a map
// keyed by slug. A slug with no tmux session behind it is simply absent.
type FakeWindow = { windowId: string; name: string; active: boolean; width: number; height: number }
function fakeListWindows(bySlug: Record<string, FakeWindow[]> = {}) {
  return vi.fn(async (slugs: string[]) => Object.fromEntries(
    slugs.filter((slug) => bySlug[slug]).map((slug) => [slug, bySlug[slug]])))
}

/** The same window set for whichever sessions the sweep asks about. */
function fakeListWindowsEach(windows: FakeWindow[]) {
  return vi.fn(async (slugs: string[]) => Object.fromEntries(slugs.map((slug) => [slug, windows])))
}

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

// What is actually on screen: every pooled session's panes stay mounted, and
// only the visible session's active window is shown. Read off v-show's own
// inline style — isVisible() resolves through getComputedStyle, which happy-dom
// does not derive from it, so it answers true for a hidden pane.
function shownWindow(wrapper: { findAll: (s: string) => DOMWrapper<Element>[] }): string | undefined {
  return wrapper.findAll('[data-testid="terminal-pane"]')
    .find((pane) => (pane.element as HTMLElement).style.display !== 'none')
    ?.attributes('data-window-id')
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
    paneMayAutoFocus.value = true
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.getTerminalEndpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 't' })
    mocks.createTerminalClient.mockReturnValue({})
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'bump deps', slug: 'hive-bump-deps', repo: 'hay-kot/hive', state: 'active' },
    ])
    // Both fixtures run, which is what keeps their repo group expanded by
    // default — a group with nothing live in it opens only on a click.
    mocks.SessionStatuses.mockResolvedValue({
      items: [
        { sessionId: '1', running: true, windows: [] },
        { sessionId: '2', running: true, windows: [] },
      ],
      pollIntervalMs: 60_000,
    })
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
    expect(wrapper.findAll('[data-testid="terminal-window-row"]').map((row) => row.text())).toEqual(['agent', 'shell'])
    expect(wrapper.findAll('[data-testid="terminal-pane"]')).toHaveLength(2)
  })

  it('shows session liveness on sessions and agent activity on windows with icons', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'live', slug: 'live', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'stopped', slug: 'stopped', repo: 'hay-kot/hive', state: 'active' },
    ])
    mocks.createTerminalClient.mockReturnValue({
      listWindows: fakeListWindows({
        live: [
          { windowId: '@1', name: 'working', active: true, width: 0, height: 0 },
          { windowId: '@2', name: 'approval', active: false, width: 0, height: 0 },
          { windowId: '@3', name: 'ready', active: false, width: 0, height: 0 },
          { windowId: '@4', name: 'unknown', active: false, width: 0, height: 0 },
        ],
      }),
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
    // Only a running session carries a glyph; an idle one greys its name instead.
    const liveness = wrapper.findAll('[data-testid="terminal-session-liveness"]')
    expect(liveness).toHaveLength(1)
    expect(liveness[0].attributes('title')).toBe('Terminal running')
    expect(liveness[0].get('span[aria-hidden="true"]').classes()).toEqual(expect.arrayContaining(['size-2.5', 'rounded-full', 'bg-current']))
    const idleRow = wrapper.get('[data-testid="terminal-session-row"][data-slug="stopped"]')
    expect(idleRow.find('[data-testid="terminal-session-liveness"]').exists()).toBe(false)
    expect(idleRow.get('span').classes()).toContain('text-text-3')
    const liveRow = wrapper.get('[data-testid="terminal-session-row"][data-slug="live"]')
    expect(liveRow.get('span').classes()).not.toContain('text-text-3')
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
    // Nothing is attached yet, so there is no repository to inherit.
    expect(mocks.openBlank).toHaveBeenCalledWith('')
  })

  it('starts a new session in the attached session’s repository', async () => {
    const { wrapper } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="terminal-new-session"]').trigger('click')
    expect(mocks.openBlank).toHaveBeenCalledWith('hay-kot/hive')
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

  // A machine's worth of repos would otherwise open at once and bury the one
  // being worked in, so an untouched group takes its state from its sessions.
  it('opens an untouched repo group only when something in it is running', async () => {
    mocks.SessionStatuses.mockResolvedValue({
      items: [
        { sessionId: '1', running: false, windows: [] },
        { sessionId: '2', running: false, windows: [] },
      ],
      pollIntervalMs: 60_000,
    })

    const { wrapper } = await mountAt()

    expect(wrapper.get('[data-testid="terminal-repo-group"]').attributes('aria-expanded')).toBe('false')
    expect(wrapper.findAll('[data-testid="terminal-session-row"]')).toHaveLength(0)
  })

  // The tree renders from cache a poll before the first status does, so a repo
  // that waited on one would collapse under the session already on screen.
  it('opens the attached repo group before any status has arrived', async () => {
    mocks.SessionStatuses.mockResolvedValue({ items: [], pollIntervalMs: 60_000 })
    mocks.useTerminalWindows.mockReturnValue(fakeSession())

    const { wrapper } = await mountAt('/terminal/hive-fix-parser')

    expect(wrapper.get('[data-testid="terminal-repo-group"]').attributes('aria-expanded')).toBe('true')
  })

  // Once toggled, the choice is the user's and outlives what the sessions do.
  it('keeps a collapsed group collapsed while its sessions run', async () => {
    const { wrapper } = await mountAt()
    await wrapper.get('[data-testid="terminal-repo-group"]').trigger('click')

    const { wrapper: reopened } = await mountAt()

    expect(reopened.get('[data-testid="terminal-repo-group"]').attributes('aria-expanded')).toBe('false')
  })

  // The markers are two elements that move, so what a test can pin is which row
  // each one is bound to — the travel itself is a layout the DOM here has none
  // of, and is verified by eye like the rest of the tree's motion.
  it('binds a selection rail to the attached session and to its active window', async () => {
    const { wrapper } = await mountAvailable()

    expect(wrapper.get('[data-testid="terminal-session-rail"]').attributes('data-shown')).toBe('false')
    expect(wrapper.get('[data-testid="terminal-window-rail"]').attributes('data-shown')).toBe('false')

    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="terminal-session-rail"]').attributes('data-shown')).toBe('true')
    expect(wrapper.get('[data-testid="terminal-window-rail"]').attributes('data-shown')).toBe('true')
  })

  // A rail whose row left the tree fades where it stands; resetting it would
  // make the next selection travel from the top of the list.
  it('hides a rail without moving it when its row goes away', async () => {
    const { wrapper } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    const placed = wrapper.get('[data-testid="terminal-session-rail"]').attributes('style')
    await wrapper.get('[data-testid="terminal-repo-group"]').trigger('click')
    await flushPromises()

    const rail = wrapper.get('[data-testid="terminal-session-rail"]')
    expect(rail.attributes('data-shown')).toBe('false')
    expect(rail.attributes('style')).toBe(placed)
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

  // The keymap dispatches ⌘1…⌘9 against the strip's positions; a session with
  // fewer windows than the digit ignores the chord rather than clamping.
  it('jumps to a window by position, and past the end does nothing', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    selectTerminalWindow(2)
    await flushPromises()
    expect(session.select).toHaveBeenCalledWith('@2')
    expect(paneMayAutoFocus.value).toBe(true)

    session.select.mockClear()
    selectTerminalWindow(3)
    await flushPromises()
    expect(session.select).not.toHaveBeenCalled()
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
    expect(wrapper.findAll('[data-testid="terminal-window-row"]')).toHaveLength(3)
    expect(shownWindow(wrapper)).toBe('@9')

    // Snapping back is a reveal and a refocus, not a re-attach.
    await rows[0].trigger('click')
    await flushPromises()
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(2)
    expect(first.focusActive).toHaveBeenCalled()
    expect(shownWindow(wrapper)).toBe('@1')
  })

  it('keeps the pool through a trip to the hub, so re-entry re-attaches nothing', async () => {
    const session = fakeSession()
    mocks.useTerminalWindows.mockReturnValue(session)
    const { wrapper, router } = await mountAt()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()
    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(1)

    // The mode is hidden, never unmounted (App.vue), so leaving it is the
    // `active` prop going false alongside the route change.
    await router.push('/feed')
    await wrapper.setProps({ active: false })
    await flushPromises()
    expect(session.dispose).not.toHaveBeenCalled()

    await router.push('/terminal/hive-bump-deps')
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(mocks.useTerminalWindows).toHaveBeenCalledTimes(1)
    expect(session.start).toHaveBeenCalledTimes(1)
    expect(wrapper.findAll('[data-testid="terminal-pane"]')).toHaveLength(2)
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
    expect(shownWindow(wrapper)).toBe('@1')

    // First paint is the swap signal.
    second.tabs.value = [{ uid: 9, windowId: '@9', name: 'other', active: true, scrolledUp: false, term: {}, fit: {} }]
    second.activeWindowId.value = '@9'
    second.status.value = 'live'
    second.painted.value = true
    await flushPromises()
    expect(shownWindow(wrapper)).toBe('@9')
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
      expect(shownWindow(wrapper)).toBe('@1')

      await vi.advanceTimersByTimeAsync(300)
      expect(shownWindow(wrapper)).toBeUndefined()
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
    const listWindows = fakeListWindows({
      'hive-bump-deps': [
        { windowId: '@7', name: 'agent', active: true, width: 0, height: 0 },
        { windowId: '@8', name: 'shell', active: false, width: 0, height: 0 },
      ],
    })
    mocks.createTerminalClient.mockReturnValue({ listWindows })
    const session = fakeSession()
    session.tabs.value = [
      { uid: 7, windowId: '@7', name: 'agent', active: true, scrolledUp: false, term: {}, fit: {} },
      { uid: 8, windowId: '@8', name: 'shell', active: false, scrolledUp: false, term: {}, fit: {} },
    ]
    session.activeWindowId.value = '@7'
    const { wrapper, router } = await mountAvailable(session)

    // Settings ▸ Appearance ▸ Terminal ships the listing on, so the tree fills
    // in without touching anything — and one call carries the whole sidebar.
    expect(listWindows).toHaveBeenCalledWith(['hive-fix-parser', 'hive-bump-deps'])
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
    const listWindows = fakeListWindowsEach([
      { windowId: '@7', name: 'agent', active: true, width: 0, height: 0 },
    ])
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
    const listWindows = fakeListWindowsEach([
      { windowId: '@7', name: 'agent', active: true, width: 0, height: 0 },
    ])
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

  it('stands in the cached listing for the subtree while attaching', async () => {
    const listWindows = fakeListWindowsEach([
      { windowId: '@7', name: 'agent', active: true, width: 0, height: 0 },
      { windowId: '@8', name: 'shell', active: false, width: 0, height: 0 },
    ])
    mocks.createTerminalClient.mockReturnValue({ listWindows })
    const session = fakeSession()
    session.tabs.value = []
    session.status.value = 'connecting'
    const { wrapper } = await mountAvailable(session)

    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    // The attached session's subtree stands in its cached rows until the live
    // ones arrive, so selecting a session does not empty the tree first.
    expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(4)

    // The live tab set replaces the stand-ins in place.
    session.tabs.value = [
      { uid: 7, windowId: '@7', name: 'agent', active: true, scrolledUp: false, term: {}, fit: {} },
      { uid: 8, windowId: '@8', name: 'shell', active: false, scrolledUp: false, term: {}, fit: {} },
    ]
    session.status.value = 'live'
    await flushPromises()
    expect(wrapper.findAll('[data-testid="terminal-window-row"]')).toHaveLength(2)
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

  it('drives the window controls from the tree and hands panes to the composable', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    expect(session.attachTab).toHaveBeenCalledTimes(2)
    expect(session.attachTab.mock.calls[0][0]).toBe('@1')

    // The attached session's row adds a window through its own client, which is
    // what makes the new one active.
    await wrapper.get('[data-testid="terminal-session-row"][data-attached="true"] [data-testid="terminal-new-window"]').trigger('click')
    expect(session.newWindow).toHaveBeenCalled()

    await wrapper.findAll('[data-testid="terminal-close-window"]')[1].trigger('click')
    expect(session.closeWindow).toHaveBeenCalledWith('@2')

    const rows = wrapper.findAll('[data-testid="terminal-window-row"]')
    await rows[1].trigger('dblclick')
    const input = wrapper.get('[data-testid="terminal-rename-input"]')
    await input.setValue('build')
    await input.trigger('keydown.enter')
    expect(session.rename).toHaveBeenCalledWith('@2', 'build')
  })

  // Adding a window is a property of the session, not of what is on screen, so
  // an unattached row offers it too — through the slug-keyed call, since there
  // is no control client to route it through yet.
  it('adds a window to an unattached session and selects it', async () => {
    const newWindow = vi.fn(async () => ({ windowId: '@5' }))
    mocks.createTerminalClient.mockReturnValue({ listWindows: fakeListWindows(), newWindow })
    const { wrapper, router } = await mountAt()

    await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-bump-deps"] [data-testid="terminal-new-window"]').trigger('click')
    await flushPromises()

    expect(newWindow).toHaveBeenCalledWith('hive-bump-deps')
    expect(router.currentRoute.value.params.slug).toBe('hive-bump-deps')
  })

  // The trailing controls sit inside the row, whose own click attaches or
  // selects; a press on one must not do both.
  it('does not select the row a trailing control was pressed in', async () => {
    const { wrapper, session } = await mountAvailable()
    await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
    await flushPromises()

    session.select.mockClear()
    await wrapper.findAll('[data-testid="terminal-close-window"]')[0].trigger('click')

    expect(session.closeWindow).toHaveBeenCalledWith('@1')
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
    expect(wrapper.find('[data-testid="terminal-window-row"]').exists()).toBe(false)

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

  // The window well is the only place windows are ordered — a window can only
  // land back in the session it left.
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

    it('marks the gap it would drop into, and clears it when the drag ends', async () => {
      const { wrapper } = await attached()
      const slots = wrapper.findAll('[data-testid="terminal-window-slot"]')
      box(slots[1], 28)

      await slots[0].trigger('dragstart', { dataTransfer: new DataTransfer() })
      await slots[1].trigger('dragover', { dataTransfer: new DataTransfer(), clientY: 30 })
      expect(wrapper.findAll('[data-testid="terminal-window-slot"]')[1].classes()).toContain('drop-before')
      expect(slots[0].classes()).toContain('opacity-40')

      await slots[0].trigger('dragend')
      expect(wrapper.findAll('[data-testid="terminal-window-slot"]')[1].classes()).not.toContain('drop-before')
    })

    // A window dropped where it already is has not moved: both of its own edges
    // name the gap it fills, and reading either as an insertion put it last.
    it('does not move a window dropped back onto itself', async () => {
      const { wrapper, session } = await attached()
      const slots = wrapper.findAll('[data-testid="terminal-window-slot"]')
      box(slots[0], 0)

      await slots[0].trigger('dragstart', { dataTransfer: new DataTransfer() })
      await slots[0].trigger('dragover', { dataTransfer: new DataTransfer(), clientY: 5 })
      await slots[0].trigger('drop')

      expect(session.moveWindow).not.toHaveBeenCalled()
      expect(wrapper.findAll('[data-testid="terminal-window-slot"]')[0].classes()).not.toContain('drop-after')
    })

    // A drag started on the name field would take the row instead of selecting
    // the text in it.
    it('leaves a window being renamed undraggable', async () => {
      const { wrapper } = await attached()
      expect(wrapper.findAll('[data-testid="terminal-window-slot"]')[1].attributes('draggable')).toBe('true')

      await wrapper.findAll('[data-testid="terminal-window-row"]')[1].trigger('dblclick')

      expect(wrapper.findAll('[data-testid="terminal-window-slot"]')[1].attributes('draggable')).toBe('false')
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
        listWindows: fakeListWindows({
          'hive-bump-deps': [{ windowId: '@7', name: 'agent', active: true, width: 0, height: 0 }],
        }),
      })
      const { wrapper } = await attached()

      const slots = wrapper.findAll('[data-testid="terminal-window-slot"]')
      expect(wrapper.findAll('[data-testid="terminal-listed-window-row"]')).toHaveLength(1)
      expect(slots[0].attributes('draggable')).toBe('false')
      expect(slots[1].attributes('draggable')).toBe('true')
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

  describe('filtering the session list', () => {
    async function filter(wrapper: { get: (s: string) => Pick<DOMWrapper<Element>, 'setValue'> }, query: string) {
      await wrapper.get('[data-testid="terminal-sessions-filter"]').setValue(query)
      await flushPromises()
    }

    function slugs(wrapper: { findAll: (s: string) => DOMWrapper<Element>[] }): (string | undefined)[] {
      return wrapper.findAll('[data-testid="terminal-session-row"]').map((row) => row.attributes('data-slug'))
    }

    it('narrows the tree to the sessions that match, and says when none do', async () => {
      const { wrapper } = await mountAvailable()

      await filter(wrapper, 'bump')
      expect(slugs(wrapper)).toEqual(['hive-bump-deps'])

      await filter(wrapper, 'PARSER')
      expect(slugs(wrapper)).toEqual(['hive-fix-parser'])

      await filter(wrapper, 'nothing here')
      expect(slugs(wrapper)).toEqual([])
      expect(wrapper.get('[data-testid="terminal-sessions-no-matches"]').text()).toContain('nothing here')
      wrapper.unmount()
    })

    it('carries a whole repo when the repo itself matches', async () => {
      mocks.ListSessions.mockResolvedValue([
        { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
        { id: '3', name: 'landing copy', slug: 'site-landing-copy', repo: 'hay-kot/haykot.dev', state: 'active' },
      ])
      const { wrapper } = await mountAvailable()

      await filter(wrapper, 'haykot.dev')
      expect(slugs(wrapper)).toEqual(['site-landing-copy'])
      wrapper.unmount()
    })

    // A collapsed group would hide the very row the query asked for.
    it('opens a collapsed group for as long as the filter is on', async () => {
      const { wrapper } = await mountAvailable()
      await wrapper.get('[data-testid="terminal-repo-group"]').trigger('click')
      expect(slugs(wrapper)).toEqual([])

      await filter(wrapper, 'bump')
      expect(slugs(wrapper)).toEqual(['hive-bump-deps'])

      await filter(wrapper, '')
      expect(slugs(wrapper)).toEqual([])
      wrapper.unmount()
    })

    // Filtering is a way to look at the list, not a way to detach: the session
    // on screen is the one the user is working in.
    it('keeps the attached session on screen while it is filtered out', async () => {
      const { wrapper, router } = await mountAvailable()
      await wrapper.findAll('[data-testid="terminal-session-row"]')[0].trigger('click')
      await flushPromises()

      await filter(wrapper, 'parser')

      expect(slugs(wrapper)).toEqual(['hive-fix-parser'])
      expect(router.currentRoute.value.path).toBe('/terminal/hive-bump-deps')
      expect(shownWindow(wrapper)).toBe('@1')
      wrapper.unmount()
    })

    it('walks only the rows the filter left', async () => {
      const { wrapper, router } = await mountAvailable()

      await filter(wrapper, 'parser')
      await wrapper.get('[data-testid="terminal-session-sidebar"]').trigger('keydown', { key: 'ArrowDown' })
      await flushPromises()

      expect(router.currentRoute.value.path).toBe('/terminal/hive-fix-parser')
      wrapper.unmount()
    })

    // Esc is the way back out, and it is two keystrokes on purpose: the first
    // restores the list, the second leaves a field that has nothing left to
    // undo. ↓ and Enter are the way out that keeps the query.
    it('clears on Esc, then hands focus to the tree', async () => {
      const { wrapper } = await mountAvailable()
      const field = wrapper.get('[data-testid="terminal-sessions-filter"]')
      // Nothing in this DOM can hold focus, so the move is observed on the call.
      const focusRow = vi.spyOn(HTMLElement.prototype, 'focus').mockImplementation(() => {})

      await filter(wrapper, 'bump')
      await field.trigger('keydown', { key: 'Escape' })
      await flushPromises()
      expect(slugs(wrapper)).toEqual(['hive-bump-deps', 'hive-fix-parser'])
      expect(focusRow).not.toHaveBeenCalled()

      await field.trigger('keydown', { key: 'Escape' })
      await flushPromises()
      expect(focusRow).toHaveBeenCalled()

      focusRow.mockClear()
      await filter(wrapper, 'bump')
      await field.trigger('keydown', { key: 'ArrowDown' })
      await flushPromises()
      expect(focusRow).toHaveBeenCalled()
      expect(slugs(wrapper)).toEqual(['hive-bump-deps'])

      focusRow.mockRestore()
      wrapper.unmount()
    })

    // `/` is dispatched by App.vue against the published handle; this is the
    // half that lands in the field.
    it('takes the field on the focus-filter command, selecting what is there', async () => {
      const { wrapper } = await mountAvailable()
      const input = wrapper.get('[data-testid="terminal-sessions-filter"]').element as HTMLInputElement
      const select = vi.spyOn(input, 'select')

      focusTerminalFilter()
      await flushPromises()

      expect(select).toHaveBeenCalled()
      wrapper.unmount()
    })
  })

  // An arrow is a click on the neighbouring row — there is no cursor running
  // ahead of the selection and no Enter to commit.
  describe('keyboard navigation in the tree', () => {
    // Attached to the sidebar rather than the row, so it answers wherever focus
    // is — including the panel itself before the first arrow.
    async function press(wrapper: { get: (s: string) => Pick<DOMWrapper<Element>, 'trigger'> }, key: string) {
      await wrapper.get('[data-testid="terminal-session-sidebar"]').trigger('keydown', { key })
      await flushPromises()
    }

    function tabStop(wrapper: { findAll: (s: string) => DOMWrapper<Element>[] }): string | undefined {
      return wrapper.findAll('[data-tree-key]')
        .find((row) => row.attributes('tabindex') === '0')
        ?.attributes('data-tree-key')
    }

    it('attaches the session an arrow lands on, as a click would', async () => {
      const { wrapper, router } = await mountAvailable()
      expect(wrapper.find('[data-testid="terminal-no-session"]').exists()).toBe(true)

      // Alphabetical within the group, so the first row is 'bump deps'.
      await press(wrapper, 'ArrowDown')

      expect(router.currentRoute.value.path).toBe('/terminal/hive-bump-deps')
      wrapper.unmount()
    })

    // Attaching the first session unfolds its two windows into the walk, so the
    // next session sits four rows down rather than one.
    it('walks through the attached session windows on to the next session', async () => {
      const { wrapper, router } = await mountAvailable()

      for (let i = 0; i < 4; i++) await press(wrapper, 'ArrowDown')

      expect(router.currentRoute.value.path).toBe('/terminal/hive-fix-parser')
      wrapper.unmount()
    })

    it('selects a window row the same way', async () => {
      const session = fakeSession()
      const { wrapper } = await mountAvailable(session)

      await press(wrapper, 'ArrowDown')
      // Onto the attached session's second window.
      await press(wrapper, 'ArrowDown')
      await press(wrapper, 'ArrowDown')

      expect(session.select).toHaveBeenCalledWith('@2')
      wrapper.unmount()
    })

    it('moves on j and k as well', async () => {
      const session = fakeSession()
      const { wrapper, router } = await mountAvailable(session)

      await press(wrapper, 'j')
      expect(router.currentRoute.value.path).toBe('/terminal/hive-bump-deps')

      await press(wrapper, 'j')
      expect(tabStop(wrapper)).toBe('w:2:@2')

      await press(wrapper, 'k')
      expect(tabStop(wrapper)).toBe('w:2:@1')
      wrapper.unmount()
    })

    // A running session is its windows. Stopping on its row first changed
    // nothing, so the row leaves the walk once there is a terminal behind it.
    it('skips a running session row and walks its windows', async () => {
      const { wrapper } = await mountAvailable()

      // Nothing is attached yet and no windows are known, so the row is still
      // the only way in.
      await press(wrapper, 'ArrowDown')
      expect(tabStop(wrapper)).toBe('w:2:@1')

      const walked = wrapper.findAll('[data-tree-key][tabindex]')
        .filter((row) => row.attributes('tabindex') === '0')
        .map((row) => row.attributes('data-tree-key'))
      expect(walked).not.toContain('s:2')
      wrapper.unmount()
    })

    // The exception that keeps every session reachable: with window listing off,
    // an unattached session has no windows to stand in for its row.
    it('keeps the row of a running session that lists no windows', async () => {
      setTerminalShowWindows(false)
      const { wrapper, router } = await mountAvailable()

      // The attached session still contributes its own live windows, so the
      // second session's row is three stops down rather than one.
      for (let i = 0; i < 3; i++) await press(wrapper, 'ArrowDown')

      expect(router.currentRoute.value.path).toBe('/terminal/hive-fix-parser')
      wrapper.unmount()
    })

    // A repo header collapses on click, which is not something to do to every
    // group an arrow passes.
    it('skips repo headers', async () => {
      const { wrapper } = await mountAvailable()

      await press(wrapper, 'ArrowUp')

      expect(wrapper.findAll('[data-tree-key]').map((row) => row.attributes('data-tree-key')))
        .not.toContain('g:hay-kot/hive')
      wrapper.unmount()
    })

    // One tab stop, not one per row: the tree is a single widget, so Tab steps
    // over it and the arrows move within it.
    it('carries a single tab stop that follows the selection', async () => {
      const { wrapper } = await mountAvailable()

      expect(wrapper.findAll('[data-tree-key][tabindex="0"]')).toHaveLength(1)
      expect(tabStop(wrapper)).toBe('s:2')

      for (let i = 0; i < 3; i++) await press(wrapper, 'ArrowDown')

      expect(wrapper.findAll('[data-tree-key][tabindex="0"]')).toHaveLength(1)
      expect(tabStop(wrapper)).toBe('w:1:@1')
      wrapper.unmount()
    })

    it('clamps at the bottom instead of wrapping', async () => {
      const { wrapper, router } = await mountAvailable()

      for (let i = 0; i < 12; i++) await press(wrapper, 'ArrowDown')

      expect(tabStop(wrapper)).toBe('w:1:@2')
      expect(router.currentRoute.value.path).toBe('/terminal/hive-fix-parser')
      wrapper.unmount()
    })

    // Enter on a row the walk only stops at because nothing is running behind
    // it. A click still just opens it — the pane's own Start button is there.
    it('starts a session that has no terminal on Enter', async () => {
      const start = vi.fn().mockResolvedValue(undefined)
      mocks.createTerminalClient.mockReturnValue({ start })
      mocks.SessionStatuses.mockResolvedValue({
        items: [
          { sessionId: '1', running: false, windows: [] },
          { sessionId: '2', running: true, windows: [] },
        ],
        pollIntervalMs: 60_000,
      })
      const { wrapper } = await mountAvailable()

      await wrapper.get('[data-tree-key="s:1"]').trigger('keydown', { key: 'Enter' })
      await flushPromises()

      expect(start).toHaveBeenCalledWith('hive-fix-parser')

      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
      await flushPromises()

      expect(start).toHaveBeenCalledTimes(1)
      wrapper.unmount()
    })

    // Cmd+↓ is not the tree's to take — it resolves through the global keymap.
    it('leaves modified arrows to the keymap', async () => {
      const { wrapper, router } = await mountAvailable()

      await wrapper.get('[data-testid="terminal-session-sidebar"]')
        .trigger('keydown', { key: 'ArrowDown', metaKey: true })
      await flushPromises()

      expect(router.currentRoute.value.path).toBe('/terminal')
      wrapper.unmount()
    })

    it('leaves the arrows to a rename input mid-edit', async () => {
      const { wrapper } = await mountAvailable(fakeSession())
      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
      await flushPromises()
      await wrapper.get('[data-testid="terminal-window-row"]').trigger('dblclick')

      const before = tabStop(wrapper)
      await wrapper.get('[data-testid="terminal-rename-input"]').trigger('keydown', { key: 'ArrowDown' })
      await flushPromises()

      expect(tabStop(wrapper)).toBe(before)
      wrapper.unmount()
    })

    // The bug this guards: a pane that grabs focus mid-walk sends the next
    // arrow to tmux instead of the tree.
    it('keeps focus in the tree while the arrows walk it', async () => {
      const session = fakeSession()
      const { wrapper } = await mountAvailable(session)

      for (let i = 0; i < 3; i++) await press(wrapper, 'ArrowDown')

      expect(paneMayAutoFocus.value).toBe(false)
      expect(session.focusActive).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    // The mouse keeps the old behaviour: clicking a session is how you go to
    // work in it.
    it('lets a click hand focus to the pane', async () => {
      const session = fakeSession()
      const { wrapper } = await mountAvailable(session)

      await press(wrapper, 'ArrowDown')
      expect(paneMayAutoFocus.value).toBe(false)

      await wrapper.get('[data-testid="terminal-session-row"][data-slug="hive-fix-parser"]').trigger('click')
      await flushPromises()

      expect(paneMayAutoFocus.value).toBe(true)
      wrapper.unmount()
    })

    it('enters the pane on Enter', async () => {
      const session = fakeSession()
      const { wrapper } = await mountAvailable(session)

      await press(wrapper, 'ArrowDown')
      await wrapper.get('[data-tree-key="s:2"]').trigger('keydown', { key: 'Enter' })
      await flushPromises()

      expect(paneMayAutoFocus.value).toBe(true)
      expect(session.focusActive).toHaveBeenCalled()
      wrapper.unmount()
    })

    it('shows the tree keys once there is a tree to drive', async () => {
      const { wrapper } = await mountAvailable()

      const hints = wrapper.get('[data-testid="terminal-tree-hints"]').text()
      expect(hints).toContain('switch')
      expect(hints).toContain('enter')
      wrapper.unmount()
    })

    // The chord worth showing is the one that leaves where focus already is.
    it('offers the way out of whichever side holds focus', async () => {
      const { wrapper } = await mountAvailable()
      const sidebar = wrapper.get('[data-testid="terminal-session-sidebar"]')

      expect(wrapper.get('[data-testid="terminal-tree-hints"]').text()).toContain('tree')

      await sidebar.trigger('focusin')
      expect(wrapper.get('[data-testid="terminal-tree-hints"]').text()).toContain('terminal')

      await sidebar.trigger('focusout')
      expect(wrapper.get('[data-testid="terminal-tree-hints"]').text()).toContain('tree')
      wrapper.unmount()
    })

    it('omits the hint bar when there are no sessions to navigate', async () => {
      mocks.ListSessions.mockResolvedValue([])
      const { wrapper } = await mountAvailable()

      expect(wrapper.find('[data-testid="terminal-tree-hints"]').exists()).toBe(false)
      wrapper.unmount()
    })
  })
})
