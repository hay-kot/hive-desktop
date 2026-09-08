import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentsSidebar from '../AgentsSidebar.vue'
import { resetAgentWorkspacesForTests } from '../../composables/useAgentWorkspaces'
import { resetAgentSessionsAllForTests } from '../../composables/useAgentSessionsAll'
import type { AgentSession, AgentWorkspace } from '../../lib/agentWorkspacesClient'

// Two workspaces and one session belonging to each, standing in for the
// wire responses AgentWorkspacesClient normally decodes. Demo B's chat is
// live and Demo A's is not, which is also what the fold default keys on.
const workspaceFixtures: AgentWorkspace[] = [
  { dir: 'demo-a', name: 'Demo A', command: 'claude', danger: false, mcps: [], skills: [], problem: '', notice: '' },
  { dir: 'demo-b', name: 'Demo B', command: 'codex --sandbox workspace-write', danger: false, mcps: [], skills: [], problem: '', notice: '' },
]

const recentFixtures: AgentSession[] = [
  { id: 2, workspace: 'demo-b', name: 'b-session', agent: 'codex', lastOpenedAt: Date.now() - 1_000, slug: 'agentws-2', terminalId: 'agentws-2', windowId: '@2', cols: 80, rows: 24, resumeAttempted: true, notice: '' },
  { id: 1, workspace: 'demo-a', name: 'a-session', agent: 'claude', lastOpenedAt: Date.now() - 100_000, slug: 'agentws-1', terminalId: '', windowId: '', cols: 0, rows: 0, resumeAttempted: true, notice: '' },
]

const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  workspaces: vi.fn(),
  allSessions: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice', () => ({
  Available: mocks.Available,
  Endpoint: vi.fn().mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' }),
}))

vi.mock('../../lib/agentWorkspacesClient', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/agentWorkspacesClient')>()
  return {
    ...actual,
    createAgentWorkspacesClient: () => ({
      workspaces: mocks.workspaces,
      openWorkspace: vi.fn(),
      deleteWorkspace: vi.fn(),
      sessions: vi.fn(),
      allSessions: mocks.allSessions,
      startSession: vi.fn(),
      resumeSession: vi.fn(),
      closeSession: vi.fn(),
      deleteSession: vi.fn(),
      openStream: vi.fn(),
    }),
  }
})

const FOLD_KEY = 'hive.agents.sidebar.workspaces'
const BOTH_OPEN = { 'demo-a': true, 'demo-b': true }

// The fold state is read out of localStorage when the component is created, so
// seeding it is how a spec picks the tree it wants to assert against. Most
// specs are about a row rather than about folding, and want every chat on
// screen; the fold specs pass {} and take the defaults instead.
async function mountSidebar(props: Record<string, unknown> = {}, folds: Record<string, boolean> = BOTH_OPEN) {
  localStorage.setItem(FOLD_KEY, JSON.stringify(folds))
  const wrapper = mount(AgentsSidebar, { props: { active: true, ...props } })
  await flushPromises()
  return wrapper
}

describe('AgentsSidebar', () => {
  // Chat row menus and the delete confirmation teleport to <body>, and the
  // sidebar's list state is a module singleton — a wrapper left mounted keeps
  // reacting to the next test's data and re-teleports UI that then answers
  // that test's document-level queries. test-setup.ts's enableAutoUnmount is
  // what tears wrappers down (registering it here too would throw — it is
  // once-per-environment); the body wipe just clears any stray nodes an
  // unmount missed.

  beforeEach(() => {
    document.body.innerHTML = ''
    resetAgentWorkspacesForTests()
    resetAgentSessionsAllForTests()
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.workspaces.mockResolvedValue({ root: '/root', rootProblem: '', available: true, error: '', workspaces: workspaceFixtures })
    mocks.allSessions.mockResolvedValue(recentFixtures)
  })

  it('keeps the agents-workspace-sidebar testid for AgentsMode.spec.ts back-compat', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.find('[data-testid="agents-workspace-sidebar"]').exists()).toBe(true)
  })

  it('nests each chat under its own workspace, so no row repeats a workspace name', async () => {
    const wrapper = await mountSidebar()
    const workspaceRows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(workspaceRows.map((row) => row.attributes('data-dir'))).toEqual(['demo-a', 'demo-b'])

    const chatRows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(chatRows.map((row) => row.attributes('data-workspace'))).toEqual(['demo-a', 'demo-b'])
    expect(chatRows[0].text()).toContain('a-session')
    expect(chatRows[0].text()).not.toContain('Demo A')
    expect(chatRows[1].text()).toContain('b-session')
    expect(chatRows[1].text()).not.toContain('Demo B')
  })

  it('focusing a workspace leaves every other workspace\'s chats on screen', async () => {
    const wrapper = await mountSidebar({ selectedWorkspace: 'demo-b' })
    const chatRows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(chatRows).toHaveLength(2)
    expect(chatRows[0].text()).toContain('a-session')
  })

  // The open chat is the sidebar's only selection mark. Focus is sticky — it
  // lives on the route and the sidebar only ever moves it — so a header that
  // drew it would read as a row stuck lit from an earlier visit.
  it('draws no mark for the focused workspace; only the open chat is marked', async () => {
    const wrapper = await mountSidebar({ selectedWorkspace: 'demo-b', openSessionId: 2 })
    const workspaceRows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(workspaceRows[1].attributes('data-focused')).toBe('true')
    expect(workspaceRows[1].classes()).toEqual(workspaceRows[0].classes())
    expect(workspaceRows[1].get('[data-testid="agents-sidebar-workspace-toggle"]').classes())
      .toEqual(workspaceRows[0].get('[data-testid="agents-sidebar-workspace-toggle"]').classes())

    const chatRows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(chatRows[1].classes()).toContain('sidebar-entry-selected')
  })

  it("accents the open chat's row, and nothing else's", async () => {
    const wrapper = await mountSidebar({ openSessionId: 2 })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[1].attributes('data-open')).toBe('true')
    expect(rows[1].classes()).toContain('sidebar-entry-selected')
    expect(rows[0].attributes('data-open')).toBe('false')
    expect(rows[0].classes()).not.toContain('sidebar-entry-selected')
  })

  it("lands the Code view's traveling rail on the open chat, and only there", async () => {
    const wrapper = await mountSidebar({ openSessionId: 2 })
    expect(wrapper.get('[data-testid="agents-sidebar-session-rail"]').attributes('data-shown')).toBe('true')
    // One rail for the tree: a second on the focused workspace would read as
    // two competing selections in one column.
    expect(wrapper.find('[data-testid="agents-sidebar-workspace-rail"]').exists()).toBe(false)
  })

  it('the rail fades out rather than sit on a stale row when no chat is open', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.get('[data-testid="agents-sidebar-session-rail"]').attributes('data-shown')).toBe('false')
  })

  it("draws a workspace as one block: a header on the sidebar's surface over a well of its chats", async () => {
    const wrapper = await mountSidebar()
    const blocks = wrapper.findAll('[data-testid="agents-sidebar-workspace-block"]')
    expect(blocks.map((block) => block.attributes('data-dir'))).toEqual(['demo-a', 'demo-b'])
    // Only the first block sits flush; the rest are closed off by a rule above.
    expect(blocks[0].classes()).toContain('ws-block-first')
    expect(blocks[1].classes()).not.toContain('ws-block-first')

    // A header is not one of the rows it heads, and every chat is inside the
    // well rather than a sibling of it.
    const header = blocks[0].get('[data-testid="agents-sidebar-workspace-row"]')
    expect(header.classes()).toContain('ws-row')
    expect(header.classes()).not.toContain('sidebar-entry')
    const well = blocks[0].get('[data-testid="agents-sidebar-workspace-well"]')
    expect(well.findAll('[data-testid="agents-sidebar-session-row"]')).toHaveLength(1)
  })

  it('a folded workspace draws no well at all', async () => {
    const wrapper = await mountSidebar({}, { 'demo-a': false, 'demo-b': false })
    expect(wrapper.findAll('[data-testid="agents-sidebar-workspace-well"]')).toHaveLength(0)
  })

  it('gives every workspace a permanent disclosure triangle stating its fold state', async () => {
    const wrapper = await mountSidebar({}, {})
    const toggles = wrapper.findAll('[data-testid="agents-sidebar-workspace-toggle"]')
    expect(toggles).toHaveLength(2)
    expect(toggles[0].attributes('aria-expanded')).toBe('false')
    expect(toggles[1].attributes('aria-expanded')).toBe('true')
  })

  // ── Folding ─────────────────────────────────────────────────────────────
  it('opens a workspace with a live chat and folds a dormant one, with nothing stored', async () => {
    const wrapper = await mountSidebar({}, {})
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(rows[0].attributes('data-expanded')).toBe('false') // demo-a: nothing running
    expect(rows[1].attributes('data-expanded')).toBe('true') // demo-b: a live chat

    const chatRows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(chatRows).toHaveLength(1)
    expect(chatRows[0].text()).toContain('b-session')
  })

  it('opens a workspace holding the chat the pane has attached', async () => {
    const wrapper = await mountSidebar({ openSessionId: 1 }, {})
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(rows[0].attributes('data-expanded')).toBe('true')
  })

  it('the chevron folds a workspace, and the fold outlives the default and is persisted', async () => {
    const wrapper = await mountSidebar({ openSessionId: 2 }, {})
    const toggles = wrapper.findAll('[data-testid="agents-sidebar-workspace-toggle"]')
    await toggles[1].trigger('click')

    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(rows[1].attributes('data-expanded')).toBe('false')
    // Folded even though it is live and holds the open chat: a deliberate fold
    // outranks every default.
    expect(wrapper.findAll('[data-testid="agents-sidebar-session-row"]')).toHaveLength(0)
    expect(JSON.parse(localStorage.getItem(FOLD_KEY) ?? '{}')['demo-b']).toBe(false)
  })

  it('clicking a workspace row body focuses it and opens it', async () => {
    const wrapper = await mountSidebar({}, { 'demo-a': false, 'demo-b': false })
    await wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')[0].trigger('click')
    expect(wrapper.emitted('select-workspace')).toEqual([['demo-a']])
    expect(wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')[0].attributes('data-expanded')).toBe('true')
  })

  it('clicking the focused workspace row keeps the focus rather than clearing it', async () => {
    const wrapper = await mountSidebar({ selectedWorkspace: 'demo-b' })
    await wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')[1].trigger('click')
    expect(wrapper.emitted('select-workspace')).toEqual([['demo-b']])
  })

  it('an expanded workspace with no chats says so instead of drawing nothing', async () => {
    mocks.allSessions.mockResolvedValue([recentFixtures[0]])
    const wrapper = await mountSidebar()
    expect(wrapper.get('[data-testid="agents-sidebar-workspace-no-chats"]').text()).toBe('No chats yet.')
  })

  it('keeps chats whose workspace directory is gone, on a row of their own', async () => {
    mocks.workspaces.mockResolvedValue({ root: '/root', rootProblem: '', available: true, error: '', workspaces: [workspaceFixtures[0]] })
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(rows.map((row) => row.attributes('data-dir'))).toEqual(['demo-a', 'demo-b'])
    expect(rows[1].text()).toContain('demo-b') // the directory, since there is no name to read
    expect(rows[1].get('[data-testid="agents-sidebar-workspace-toggle"]').classes()).toContain('ws-toggle-problem')
    expect(rows[1].attributes('title')).toContain('no longer in the workspace root')
    // Nothing to open and nothing to edit, but its chats are still reachable.
    expect(rows[1].find('[data-testid="agents-sidebar-workspace-edit"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="agents-sidebar-session-row"]')).toHaveLength(2)
  })

  it('has one scroll region: the sidebar keeps its width handle and the workspaces/chats divider is gone', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.find('[data-testid="resize-handle-agents-sidebar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="resize-handle-agents-workspaces"]').exists()).toBe(false)
  })

  it('states liveness explicitly: a green dot for a live terminal, a hollow idle dot otherwise', async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].find('[data-testid="agents-sidebar-session-liveness"]').exists()).toBe(false) // a-session has no terminalId
    expect(rows[0].find('[data-testid="agents-sidebar-session-idle"]').exists()).toBe(true)
    expect(rows[1].find('[data-testid="agents-sidebar-session-liveness"]').exists()).toBe(true) // b-session has one
    expect(rows[1].find('[data-testid="agents-sidebar-session-idle"]').exists()).toBe(false)
  })

  it('a dormant chat says how long ago on the row; a live one lets its mark say it', async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].get('.entry-age').text()).toBe('1m ago')
    expect(rows[1].find('.entry-age').exists()).toBe(false)
  })

  it('a chat working or waiting on approval drops the age for its activity mark', async () => {
    const wrapper = await mountSidebar({ sessionActivity: { 1: 'active' } })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].find('.entry-age').exists()).toBe(false)
    expect(rows[0].find('.animate-spin').exists()).toBe(true)
  })

  it("a workspace's launch command moves to its row tooltip", async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(rows[0].attributes('title')).toContain('claude')
    expect(rows[0].text()).not.toContain('claude')
  })

  it('clicking a session row emits select-session with that session', async () => {
    const wrapper = await mountSidebar()
    await wrapper.findAll('[data-testid="agents-sidebar-session-row"]')[0].trigger('click')
    expect(wrapper.emitted('select-session')).toEqual([[recentFixtures[1]]])
  })

  // Row menu panels teleport to <body> (AppMenu's anchored mode), so entries
  // are queried off the document rather than the row wrapper.
  function menuEntry(testid: string): HTMLButtonElement | null {
    return document.querySelector<HTMLButtonElement>(`[data-testid="${testid}"]`)
  }

  // The panel only escapes the sidebar's scroll clipping when AppMenu gets an
  // attached anchor; without one it renders inline and is clipped, which is how
  // the menu looked broken while the anchor came from a ref map a fold could
  // stale out.
  it('anchors the chat menu to its row, so the panel teleports out of the scroll region', async () => {
    const wrapper = await mountSidebar()
    const row = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')[0]
    await row.get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    const panel = document.querySelector('[data-testid="agents-sidebar-session-menu-panel"]')
    expect(panel?.parentElement).toBe(document.body)
    expect((panel as HTMLElement).style.position).toBe('fixed')
  })

  it('opening a chat menu never also opens the chat', async () => {
    const wrapper = await mountSidebar()
    const row = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')[0]
    await row.get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    expect(wrapper.emitted('select-session')).toBeUndefined()
  })

  it('a second click on the toggle closes the menu it opened', async () => {
    const wrapper = await mountSidebar()
    const toggle = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')[0]
      .get('[data-testid="agents-sidebar-session-menu"]')
    await toggle.trigger('click')
    expect(menuEntry('agents-sidebar-session-rename')).not.toBeNull()
    await toggle.trigger('click')
    await flushPromises()
    expect(menuEntry('agents-sidebar-session-rename')).toBeNull()
  })

  it("a chat row's menu offers stop only while live, and stop emits with the session", async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')

    await rows[1].get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    expect(menuEntry('agents-sidebar-session-close')).not.toBeNull()
    menuEntry('agents-sidebar-session-close')!.click()
    await flushPromises()
    expect(wrapper.emitted('close-session')).toEqual([[recentFixtures[0]]])

    await rows[0].get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    expect(menuEntry('agents-sidebar-session-close')).toBeNull()
  })

  it('deleting a chat opens a confirmation and emits delete-session only on confirm', async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    await rows[0].get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    menuEntry('agents-sidebar-session-delete')!.click()
    await flushPromises()

    const confirmButton = document.querySelector<HTMLButtonElement>('[data-testid="agents-sidebar-delete-session-confirmation-confirm"]')
    expect(confirmButton, 'the confirmation dialog is teleported to <body>').not.toBeNull()
    expect(wrapper.emitted('delete-session')).toBeUndefined()

    confirmButton!.click()
    await flushPromises()
    expect(wrapper.emitted('delete-session')).toEqual([[recentFixtures[1]]])
  })

  it("a chat row's menu offers rename, which emits rename-session", async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    await rows[1].get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    menuEntry('agents-sidebar-session-rename')!.click()
    await flushPromises()
    expect(wrapper.emitted('rename-session')).toEqual([[recentFixtures[0]]])
  })

  it("a workspace's + starts a chat in it with no dialog, and opens the row it lands in", async () => {
    const wrapper = await mountSidebar({}, { 'demo-a': false, 'demo-b': false })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    await rows[0].get('[data-testid="agents-sidebar-workspace-new-session"]').trigger('click')
    expect(wrapper.emitted('start-session')).toEqual([['demo-a']])
    // Nothing else fires: the + is not the row's own click, and it is not the
    // header + that opens the dialog.
    expect(wrapper.emitted('select-workspace')).toBeUndefined()
    expect(wrapper.emitted('request-new-session')).toBeUndefined()
    expect(rows[0].attributes('data-expanded')).toBe('true')
  })

  it('a workspace the listing lost offers no + — there is no directory to start in', async () => {
    mocks.workspaces.mockResolvedValue({ root: '/root', rootProblem: '', available: true, error: '', workspaces: [workspaceFixtures[0]] })
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(rows[0].find('[data-testid="agents-sidebar-workspace-new-session"]').exists()).toBe(true)
    expect(rows[1].find('[data-testid="agents-sidebar-workspace-new-session"]').exists()).toBe(false)
  })

  it("a workspace's + is disabled while a chat is already launching", async () => {
    const wrapper = await mountSidebar({ startingSession: true })
    const add = wrapper.findAll('[data-testid="agents-sidebar-workspace-new-session"]')[0]
    expect(add.attributes('disabled')).toBeDefined()
  })

  it('the header\'s + buttons emit create-workspace and request-new-session', async () => {
    const wrapper = await mountSidebar()
    await wrapper.get('[data-testid="agents-sidebar-new-workspace"]').trigger('click')
    expect(wrapper.emitted('create-workspace')).toHaveLength(1)

    await wrapper.get('[data-testid="agents-sidebar-new-session"]').trigger('click')
    expect(wrapper.emitted('request-new-session')).toHaveLength(1)
  })

  it('a workspace row\'s edit button and its context menu both emit edit-workspace', async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    await rows[1].get('[data-testid="agents-sidebar-workspace-edit"]').trigger('click')
    await rows[0].trigger('contextmenu')
    expect(wrapper.emitted('edit-workspace')).toEqual([[workspaceFixtures[1]], [workspaceFixtures[0]]])
  })

  it("renders the Code view's activity icons from sessionActivity, on the chat rows alone", async () => {
    const wrapper = await mountSidebar({ sessionActivity: { 2: 'approval', 1: 'active' } })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].find('.animate-spin').exists()).toBe(true) // a-session is working
    expect(rows[1].find('.text-severity-warning').exists()).toBe(true) // b-session needs approval
  })

  it('a workspace header carries its name and three controls, and no rollup of its own', async () => {
    const wrapper = await mountSidebar({ sessionActivity: { 2: 'approval' } })
    const header = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')[1]
    expect(header.text()).toBe('Demo B') // no count beside the name
    expect(header.find('.bg-severity-warning').exists()).toBe(false)
    // +, edit, chevron — in that order, all on one pitch.
    expect(header.findAll('button').map((button) => button.attributes('data-testid'))).toEqual([
      'agents-sidebar-workspace-new-session',
      'agents-sidebar-workspace-edit',
      'agents-sidebar-workspace-toggle',
    ])
  })

  it('exposes focus() for the global keymap handle', async () => {
    const wrapper = await mountSidebar()
    const aside = wrapper.get('[data-testid="agents-workspace-sidebar"]').element as HTMLElement
    const focusSpy = vi.spyOn(aside, 'focus')
    ;(wrapper.vm as unknown as { focus: () => void }).focus()
    expect(focusSpy).toHaveBeenCalled()
  })
})
