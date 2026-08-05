import { createMemoryHistory } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentsMode from '../AgentsMode.vue'
import AgentsSidebar from '../AgentsSidebar.vue'
import { resetAgentWorkspacesForTests } from '../../composables/useAgentWorkspaces'
import { resetAgentSessionsAllForTests } from '../../composables/useAgentSessionsAll'
import { createAppRouter } from '../../router'

// App.vue mounts AgentsMode once and hides it with v-show on a trip to the
// hub (ADR terminal-mode-is-hidden-not-unmounted): the component itself must never re-key or v-if anything
// internal to props.active, or a v-show'd parent would still pay for a
// rebuild on every round trip. This asserts that at the component level —
// App.spec.ts separately proves the parent uses v-show rather than v-if.

const xterm = vi.hoisted(() => {
  class FakeTerminal {
    static instances: FakeTerminal[] = []
    cols = 80
    rows = 24
    options: Record<string, unknown>
    write = vi.fn()
    open = vi.fn()
    loadAddon = vi.fn()
    dispose = vi.fn()
    resize = vi.fn()
    focus = vi.fn()

    constructor(options: Record<string, unknown> = {}) {
      this.options = { ...options }
      FakeTerminal.instances.push(this)
    }

    onData(_handler: (data: string) => void) {
      return { dispose: vi.fn() }
    }
  }

  class FakeAddon {
    dispose = vi.fn()
    activate = vi.fn()
    fit = vi.fn()
    proposeDimensions = vi.fn(() => ({ cols: 132, rows: 43 }))
  }

  return { FakeTerminal, FakeAddon }
})

vi.mock('@xterm/xterm', () => ({ Terminal: xterm.FakeTerminal }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: xterm.FakeAddon }))
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: xterm.FakeAddon }))

const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  Endpoint: vi.fn(),
  getAgentsEndpoint: vi.fn(),
  createAgentWorkspacesClient: vi.fn(),
  loadTerminalFaces: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice', () => ({
  Available: mocks.Available,
  Endpoint: mocks.Endpoint,
}))
vi.mock('../../lib/terminalFaces', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/terminalFaces')>()),
  loadTerminalFaces: mocks.loadTerminalFaces,
}))
vi.mock('../../lib/terminalRenderer', () => ({ claimAtlasRenderer: vi.fn() }))
vi.mock('../../lib/agentWorkspacesClient', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/agentWorkspacesClient')>()),
  getAgentsEndpoint: mocks.getAgentsEndpoint,
  createAgentWorkspacesClient: mocks.createAgentWorkspacesClient,
}))

class FakeSocket {
  static OPEN = 1
  readyState = 1
  binaryType = 'blob'
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  send(): void {}
  close(): void {}
}

class FakeResizeObserver {
  observe = vi.fn()
  disconnect = vi.fn()
  constructor(_callback: () => void) {}
}

const workspaceRows = [
  { dir: 'web-app', name: 'Web App', agent: 'claude', autonomy: '', mcps: [], problem: '', notice: '' },
  { dir: 'api', name: 'API', agent: 'claude', autonomy: '', mcps: [], problem: '', notice: '' },
]

// A chat row as the cross-workspace listing reports it: terminalId set means
// the listing's tmux probe found the session alive.
const chatRow = {
  id: 7, workspace: 'web-app', name: 'New Chat', agent: 'claude', lastOpenedAt: 0,
  terminalId: 'agentws-7', windowId: '', cols: 0, rows: 0, resumeAttempted: false, notice: '',
}

function fakeClient(editor = { command: 'zed', title: 'Zed' }) {
  return {
    workspaces: vi.fn().mockResolvedValue({
      root: '/tmp/agents', rootProblem: '', available: true, error: '',
      workspaces: workspaceRows, agents: ['claude'], autonomyFlags: {}, editor,
    }),
    openWorkspace: vi.fn((dir: string) => Promise.resolve({
      workspace: workspaceRows.find((ws) => ws.dir === dir) ?? workspaceRows[0],
      sessions: [], missingMcps: [],
    })),
    allSessions: vi.fn().mockResolvedValue([]),
    activity: vi.fn().mockResolvedValue([]),
    startSession: vi.fn().mockResolvedValue({
      id: 7, workspace: 'web-app', name: 'New Chat', agent: 'claude', lastOpenedAt: 0,
      terminalId: 't1', windowId: 'w1', cols: 80, rows: 24, resumeAttempted: false, notice: '',
    }),
    resumeSession: vi.fn().mockResolvedValue({ ...chatRow, windowId: 'w1', cols: 80, rows: 24, resumeAttempted: true }),
    closeSession: vi.fn().mockResolvedValue({ closed: true }),
    resizeSession: vi.fn().mockResolvedValue(undefined),
    openStream: vi.fn(() => new FakeSocket()),
    openWorkspaceInEditor: vi.fn().mockResolvedValue(undefined),
    revealWorkspace: vi.fn().mockResolvedValue(undefined),
  }
}

async function mountAgentsMode(path = '/workspaces') {
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  await router.isReady()
  const wrapper = mount(AgentsMode, { global: { plugins: [router] }, props: { active: true } })
  await flushPromises()
  return { wrapper, router }
}

// The status bar only exists over a launching or live pane, so every bar test
// starts a chat the way the sidebar does.
async function mountWithOpenChat(client = fakeClient()) {
  mocks.createAgentWorkspacesClient.mockReturnValue(client)
  const { wrapper, router } = await mountAgentsMode('/workspaces/web-app')
  wrapper.findComponent(AgentsSidebar).vm.$emit('request-new-session')
  await flushPromises()
  return { wrapper, router, client }
}

describe('AgentsMode', () => {
  beforeEach(() => {
    resetAgentWorkspacesForTests()
    resetAgentSessionsAllForTests()
    xterm.FakeTerminal.instances = []
    globalThis.ResizeObserver = FakeResizeObserver as unknown as typeof ResizeObserver
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.Endpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' })
    mocks.getAgentsEndpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' })
    mocks.createAgentWorkspacesClient.mockReturnValue(fakeClient())
    mocks.loadTerminalFaces.mockResolvedValue(undefined)
  })

  it('keeps the same root and sidebar elements across an active toggle', async () => {
    const { wrapper } = await mountAgentsMode()

    const rootBefore = wrapper.find('[data-testid="agents-mode"]').element
    const sidebarBefore = wrapper.find('[data-testid="agents-workspace-sidebar"]').element

    await wrapper.setProps({ active: false })
    await flushPromises()
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(wrapper.find('[data-testid="agents-mode"]').element).toBe(rootBefore)
    expect(wrapper.find('[data-testid="agents-workspace-sidebar"]').element).toBe(sidebarBefore)
  })

  it('reports the unavailable reason and offers a retry when ptyterm is unavailable', async () => {
    mocks.Available.mockResolvedValue({ available: false, reason: 'agent workspaces need macOS or Linux and a desktop build.' })
    const { wrapper } = await mountAgentsMode()

    expect(wrapper.find('[data-testid="agents-unavailable"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="agents-unavailable-reason"]').text())
      .toBe('agent workspaces need macOS or Linux and a desktop build.')
    expect(wrapper.find('[data-testid="agents-workspace-sidebar"]').exists()).toBe(false)
  })

  it('raises the status bar over an open chat and opens its workspace outside the app', async () => {
    mocks.createAgentWorkspacesClient.mockReturnValue(fakeClient())
    const { wrapper } = await mountAgentsMode('/workspaces/web-app')
    expect(wrapper.find('[data-testid="agents-pane-statusbar"]').exists()).toBe(false)

    wrapper.findComponent(AgentsSidebar).vm.$emit('request-new-session')
    await flushPromises()

    expect(wrapper.get('[data-testid="agents-pane-statusbar-workspace"]').text()).toBe('Web App')
    const openInEditor = wrapper.get('[data-testid="agents-pane-statusbar-open-editor"]')
    expect(openInEditor.attributes('title')).toBe('Open in Zed')
  })

  it('routes the bar actions at the open chat\'s workspace directory', async () => {
    const { wrapper, client } = await mountWithOpenChat()

    await wrapper.get('[data-testid="agents-pane-statusbar-open-editor"]').trigger('click')
    await wrapper.get('[data-testid="agents-pane-statusbar-reveal"]').trigger('click')

    expect(client.openWorkspaceInEditor).toHaveBeenCalledWith('web-app')
    expect(client.revealWorkspace).toHaveBeenCalledWith('web-app')
  })

  // The open chat need not belong to the focused workspace: moving the focus
  // filter must not repoint the bar (or its actions) at the newly focused one.
  it('keeps naming the open chat\'s workspace when the focus filter moves', async () => {
    const { wrapper, router } = await mountWithOpenChat()

    await router.push({ name: 'agents', params: { workspace: 'api' } })
    await flushPromises()

    expect(wrapper.get('[data-testid="agents-pane-statusbar-workspace"]').text()).toBe('Web App')
  })

  it('offers no editor action when no editor is configured', async () => {
    const { wrapper } = await mountWithOpenChat(fakeClient({ command: '', title: '' }))

    expect(wrapper.find('[data-testid="agents-pane-statusbar-open-editor"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="agents-pane-statusbar-reveal"]').exists()).toBe(true)
  })

  it('names the open chat in the route and clears it when the chat closes', async () => {
    const { wrapper, router, client } = await mountWithOpenChat()
    expect(router.currentRoute.value.query.chat).toBe('7')

    wrapper.findComponent(AgentsSidebar).vm.$emit('close-session', { ...chatRow })
    await flushPromises()

    expect(client.closeSession).toHaveBeenCalledWith(7)
    expect(router.currentRoute.value.query.chat).toBeUndefined()
  })

  it('carries the open chat across a focus switch', async () => {
    const { wrapper, router } = await mountWithOpenChat()

    wrapper.findComponent(AgentsSidebar).vm.$emit('select-workspace', 'api')
    await flushPromises()

    expect(router.currentRoute.value.params.workspace).toBe('api')
    expect(router.currentRoute.value.query.chat).toBe('7')
  })

  // The reload path: the hash preserved the route, the pane starts idle, and
  // the routed chat's tmux session is still running — so it reattaches.
  it('reattaches the routed chat when its tmux session is live', async () => {
    const client = fakeClient()
    client.allSessions.mockResolvedValue([chatRow])
    mocks.createAgentWorkspacesClient.mockReturnValue(client)
    const { wrapper } = await mountAgentsMode('/workspaces/web-app?chat=7')

    expect(client.resumeSession).toHaveBeenCalledWith({ id: 7 })
    expect(wrapper.get('[data-testid="agents-pane-statusbar-workspace"]').text()).toBe('Web App')
  })

  // ResumeSession relaunches a dead session, and a relaunch must stay a
  // deliberate click — the routed chat is dropped, never resumed blind.
  it('drops the routed chat instead of relaunching it when its session is dead', async () => {
    const client = fakeClient()
    client.allSessions.mockResolvedValue([{ ...chatRow, terminalId: '' }])
    mocks.createAgentWorkspacesClient.mockReturnValue(client)
    const { router } = await mountAgentsMode('/workspaces/web-app?chat=7')

    expect(client.resumeSession).not.toHaveBeenCalled()
    expect(router.currentRoute.value.query.chat).toBeUndefined()
  })

  it('reports a failed open/reveal in the bar instead of dropping it', async () => {
    const { wrapper, client } = await mountWithOpenChat()
    client.revealWorkspace.mockRejectedValue(new Error('the directory is gone'))

    await wrapper.get('[data-testid="agents-pane-statusbar-reveal"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="agents-pane-statusbar-error"]').text()).toBe('the directory is gone')
  })

  // A session that dies before the attach answers 200 with no terminalId and
  // the reason in its notice. The listing carries no notice, so this response
  // is the only place it exists — dropping it made a failed launch look like a
  // click that did nothing.
  it('reports the launch notice when a session exits before it can be attached', async () => {
    const client = fakeClient()
    client.startSession.mockResolvedValue({
      id: 7, workspace: 'web-app', name: 'New Chat', agent: 'claude', lastOpenedAt: 0,
      terminalId: '', windowId: '', cols: 0, rows: 0, resumeAttempted: true,
      notice: 'the session exited immediately; check that the agent CLI is installed and on PATH',
    })
    mocks.createAgentWorkspacesClient.mockReturnValue(client)
    const { wrapper } = await mountAgentsMode('/workspaces/web-app')

    wrapper.findComponent(AgentsSidebar).vm.$emit('request-new-session')
    await flushPromises()

    expect(wrapper.get('[data-testid="agents-pane-error"]').text())
      .toBe('the session exited immediately; check that the agent CLI is installed and on PATH')
  })
})
