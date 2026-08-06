import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentsSidebar from '../AgentsSidebar.vue'
import { resetAgentWorkspacesForTests } from '../../composables/useAgentWorkspaces'
import { resetAgentSessionsAllForTests } from '../../composables/useAgentSessionsAll'
import type { AgentSession, AgentWorkspace } from '../../lib/agentWorkspacesClient'

// Two workspaces and two sessions belonging to each, standing in for the
// wire responses AgentWorkspacesClient normally decodes.
const workspaceFixtures: AgentWorkspace[] = [
  { dir: 'demo-a', name: 'Demo A', agent: 'claude', autonomy: 'ask', mcps: [], problem: '', notice: '' },
  { dir: 'demo-b', name: 'Demo B', agent: 'codex', autonomy: 'auto', mcps: [], problem: '', notice: '' },
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

async function mountSidebar(props: Record<string, unknown> = {}) {
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

  it('lists every session across every workspace, naming each row\'s workspace, when nothing is focused', async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('b-session')
    expect(rows[0].text()).toContain('Demo B')
    expect(rows[1].text()).toContain('a-session')
    expect(rows[1].text()).toContain('Demo A')
  })

  it('focusing a workspace filters the session list to it and drops the workspace name from rows', async () => {
    const wrapper = await mountSidebar({ selectedWorkspace: 'demo-b' })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('b-session')
    expect(rows[0].text()).not.toContain('Demo B')
  })

  it('marks the focused workspace: the traveling rail lands on its row, the name goes accent', async () => {
    const wrapper = await mountSidebar({ selectedWorkspace: 'demo-b' })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(rows[1].attributes('data-focused')).toBe('true')
    expect(rows[0].attributes('data-focused')).toBe('false')
    expect(rows[1].find('.text-accent').exists()).toBe(true)
    expect(wrapper.get('[data-testid="agents-sidebar-workspace-rail"]').attributes('data-shown')).toBe('true')
  })

  it('the session rail lands on the open chat row', async () => {
    const wrapper = await mountSidebar({ openSessionId: 2 })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].attributes('data-open')).toBe('true')
    expect(rows[1].attributes('data-open')).toBe('false')
    expect(wrapper.get('[data-testid="agents-sidebar-session-rail"]').attributes('data-shown')).toBe('true')
  })

  it('the rails fade out rather than sit on a stale row when nothing is selected', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.get('[data-testid="agents-sidebar-workspace-rail"]').attributes('data-shown')).toBe('false')
    expect(wrapper.get('[data-testid="agents-sidebar-session-rail"]').attributes('data-shown')).toBe('false')
  })

  it('exposes resize handles for the sidebar width and the workspaces/chats divider', async () => {
    const wrapper = await mountSidebar()
    expect(wrapper.find('[data-testid="resize-handle-agents-sidebar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="resize-handle-agents-workspaces"]').exists()).toBe(true)
  })

  it('states liveness explicitly: a green dot for a live terminal, a hollow idle dot otherwise', async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].find('[data-testid="agents-sidebar-session-liveness"]').exists()).toBe(true) // b-session has a terminalId
    expect(rows[0].find('[data-testid="agents-sidebar-session-idle"]').exists()).toBe(false)
    expect(rows[1].find('[data-testid="agents-sidebar-session-liveness"]').exists()).toBe(false) // a-session has none
    expect(rows[1].find('[data-testid="agents-sidebar-session-idle"]').exists()).toBe(true)
  })

  it("the meta line reads live-vs-age, not the agent's name", async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].text()).toContain('live')
    expect(rows[0].text()).not.toContain('codex')
    expect(rows[1].text()).toContain('1m ago')
    expect(rows[1].text()).not.toContain('claude')
  })

  it('clicking a session row emits select-session with that session', async () => {
    const wrapper = await mountSidebar()
    await wrapper.findAll('[data-testid="agents-sidebar-session-select"]')[1].trigger('click')
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

    await rows[0].get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    expect(menuEntry('agents-sidebar-session-close')).not.toBeNull()
    menuEntry('agents-sidebar-session-close')!.click()
    await flushPromises()
    expect(wrapper.emitted('close-session')).toEqual([[recentFixtures[0]]])

    await rows[1].get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    expect(menuEntry('agents-sidebar-session-close')).toBeNull()
  })

  it('deleting a chat opens a confirmation and emits delete-session only on confirm', async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    await rows[1].get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
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
    await rows[0].get('[data-testid="agents-sidebar-session-menu"]').trigger('click')
    menuEntry('agents-sidebar-session-rename')!.click()
    await flushPromises()
    expect(wrapper.emitted('rename-session')).toEqual([[recentFixtures[0]]])
  })

  it('selecting a workspace row emits select-workspace with its dir', async () => {
    const wrapper = await mountSidebar()
    await wrapper.findAll('[data-testid="agents-sidebar-workspace-select"]')[1].trigger('click')
    expect(wrapper.emitted('select-workspace')).toEqual([['demo-b']])
  })

  it('clicking the focused workspace row clears the focus instead of re-selecting it', async () => {
    const wrapper = await mountSidebar({ selectedWorkspace: 'demo-b' })
    await wrapper.findAll('[data-testid="agents-sidebar-workspace-select"]')[1].trigger('click')
    expect(wrapper.emitted('select-workspace')).toEqual([['']])
  })

  it('the + in the Workspaces title row emits create-workspace, and the row\'s edit button emits edit-workspace', async () => {
    const wrapper = await mountSidebar()
    await wrapper.get('[data-testid="agents-sidebar-new-workspace"]').trigger('click')
    expect(wrapper.emitted('create-workspace')).toHaveLength(1)

    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    await rows[1].get('[data-testid="agents-sidebar-workspace-edit"]').trigger('click')
    expect(wrapper.emitted('edit-workspace')).toEqual([[workspaceFixtures[1]]])
  })

  it('right-clicking a workspace row opens the editor too — the row has no menu of its own', async () => {
    const wrapper = await mountSidebar()
    const rows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    await rows[0].trigger('contextmenu')
    expect(wrapper.emitted('edit-workspace')).toEqual([[workspaceFixtures[0]]])
  })

  it('the + in the Chats title row emits request-new-session', async () => {
    const wrapper = await mountSidebar()
    await wrapper.get('[data-testid="agents-sidebar-new-session"]').trigger('click')
    expect(wrapper.emitted('request-new-session')).toHaveLength(1)
  })

  it('renders the Code view\'s activity icons from sessionActivity, mirrored onto the workspace row', async () => {
    const wrapper = await mountSidebar({ sessionActivity: { 2: 'approval', 1: 'active' } })
    const rows = wrapper.findAll('[data-testid="agents-sidebar-session-row"]')
    expect(rows[0].find('.text-severity-warning').exists()).toBe(true) // b-session needs approval
    expect(rows[1].find('.animate-spin').exists()).toBe(true) // a-session is working
    const workspaceRows = wrapper.findAll('[data-testid="agents-sidebar-workspace-row"]')
    expect(workspaceRows[1].find('.bg-severity-warning').exists()).toBe(true) // demo-b mirrors the approval
  })

  it('exposes focus() for the global keymap handle', async () => {
    const wrapper = await mountSidebar()
    const aside = wrapper.get('[data-testid="agents-workspace-sidebar"]').element as HTMLElement
    const focusSpy = vi.spyOn(aside, 'focus')
    ;(wrapper.vm as unknown as { focus: () => void }).focus()
    expect(focusSpy).toHaveBeenCalled()
  })
})
