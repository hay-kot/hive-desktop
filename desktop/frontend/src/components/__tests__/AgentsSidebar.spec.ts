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
  { dir: 'demo-a', name: 'Demo A', agent: 'claude', autonomy: 'ask', mcps: [], skills: [], problem: '', notice: '' },
  { dir: 'demo-b', name: 'Demo B', agent: 'codex', autonomy: 'auto', mcps: [], skills: [], problem: '', notice: '' },
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

  it('marks the focused workspace in accent, with no rail of its own to compete with the chat rail', async () => {
    const wrapper = await mountSidebar({ selectedWorkspace: 'demo-b' })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(rows[1].attributes('data-focused')).toBe('true')
    expect(rows[0].attributes('data-focused')).toBe('false')
    expect(rows[1].find('.text-accent').exists()).toBe(true)
    expect(wrapper.find('[data-testid="agents-sidebar-workspace-rail"]').exists()).toBe(false)
  })

  it('the rail lands on the open chat row', async () => {
    const wrapper = await mountSidebar({ openSessionId: 2 })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[1].attributes('data-open')).toBe('true')
    expect(rows[0].attributes('data-open')).toBe('false')
    expect(wrapper.get('[data-testid="agents-sidebar-session-rail"]').attributes('data-shown')).toBe('true')
  })

  it('the rail fades out rather than sit on a stale row when no chat is open', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.get('[data-testid="agents-sidebar-session-rail"]').attributes('data-shown')).toBe('false')
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
    await wrapper.findAll('[data-testid="agents-sidebar-workspace-select"]')[0].trigger('click')
    expect(wrapper.emitted('select-workspace')).toEqual([['demo-a']])
    expect(wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')[0].attributes('data-expanded')).toBe('true')
  })

  it('clicking the focused workspace row keeps the focus rather than clearing it', async () => {
    const wrapper = await mountSidebar({ selectedWorkspace: 'demo-b' })
    await wrapper.findAll('[data-testid="agents-sidebar-workspace-select"]')[1].trigger('click')
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
    expect(rows[1].find('[data-testid="agents-sidebar-workspace-missing"]').exists()).toBe(true)
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

  it("the meta line reads live-vs-age, not the agent's name", async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].text()).toContain('1m ago')
    expect(rows[0].text()).not.toContain('claude')
    expect(rows[1].text()).toContain('live')
    expect(rows[1].text()).not.toContain('codex')
  })

  it('clicking a session row emits select-session with that session', async () => {
    const wrapper = await mountSidebar()
    await wrapper.findAll('[data-testid="agents-sidebar-session-select"]')[0].trigger('click')
    expect(wrapper.emitted('select-session')).toEqual([[recentFixtures[1]]])
  })

  // Row menu panels teleport to <body> (AppMenu's anchored mode), so entries
  // are queried off the document rather than the row wrapper.
  function menuEntry(testid: string): HTMLButtonElement | null {
    return document.querySelector<HTMLButtonElement>(`[data-testid="${testid}"]`)
  }

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

  it('renders the Code view\'s activity icons from sessionActivity, rolled up onto the workspace row', async () => {
    const wrapper = await mountSidebar({ sessionActivity: { 2: 'approval', 1: 'active' } })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].find('.animate-spin').exists()).toBe(true) // a-session is working
    expect(rows[1].find('.text-severity-warning').exists()).toBe(true) // b-session needs approval
    const workspaceRows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(workspaceRows[1].find('.bg-severity-warning').exists()).toBe(true) // demo-b rolls the approval up
  })

  it('exposes focus() for the global keymap handle', async () => {
    const wrapper = await mountSidebar()
    const aside = wrapper.get('[data-testid="agents-workspace-sidebar"]').element as HTMLElement
    const focusSpy = vi.spyOn(aside, 'focus')
    ;(wrapper.vm as unknown as { focus: () => void }).focus()
    expect(focusSpy).toHaveBeenCalled()
  })
})
