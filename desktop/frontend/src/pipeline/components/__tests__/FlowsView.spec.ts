import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import FlowsView from '../FlowsView.vue'
import { resetFlowsSessionForTests, useFlowsSession } from '../../composables/useFlowsSession'
import type { WireFlow } from '../../lib/wireFlow'

// FlowsView reads everything from the useFlowsSession() singleton (a real
// adapter over the generated Wails bindings by default), so — same posture
// as App.spec.ts, which exercises this same toolbar through the full app
// tree — the bindings modules are mocked here rather than injecting a fake
// PipelineEditorClient directly.
const mocks = vi.hoisted(() => ({
  ListFlows: vi.fn(),
  GetFlow: vi.fn(),
  GetLayout: vi.fn(),
  SaveFlow: vi.fn(),
  SaveLayout: vi.fn(),
  ListInboxItemsByFeed: vi.fn(),
  ListUnarchivedInboxItems: vi.fn(),
  ListReplaySourceSnapshots: vi.fn(),
  EventLogTailOffset: vi.fn(),
  ActivateReplay: vi.fn(),
  NodeRuns: vi.fn(),
  ReadFrom: vi.fn(),
  Commit: vi.fn(),
  On: vi.fn(),
  SetText: vi.fn(),
  RenderPrompt: vi.fn(),
}))

// Prompt text is assembled by the Go prompts service, so "Copy prompt" is a
// service call followed by a clipboard write.
vi.mock('../../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/promptsservice', () => ({
  Catalog: vi.fn(),
  Render: mocks.RenderPrompt,
}))

vi.mock('../../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/flowsservice', () => ({
  ListFlows: mocks.ListFlows,
  GetFlow: mocks.GetFlow,
  GetLayout: mocks.GetLayout,
  SaveFlow: mocks.SaveFlow,
  SaveLayout: mocks.SaveLayout,
}))

vi.mock('../../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/pipelineservice', () => ({
  ListInboxItemsByFeed: mocks.ListInboxItemsByFeed,
  ListUnarchivedInboxItems: mocks.ListUnarchivedInboxItems,
  ListReplaySourceSnapshots: mocks.ListReplaySourceSnapshots,
  EventLogTailOffset: mocks.EventLogTailOffset,
  ActivateReplay: mocks.ActivateReplay,
  NodeRuns: mocks.NodeRuns,
  ReadFrom: mocks.ReadFrom,
  Commit: mocks.Commit,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.On },
  Clipboard: { SetText: mocks.SetText },
}))

const flowSummaries = [
  { id: 'flow-1', name: 'Flow one', enabled: true, valid: true },
  { id: 'flow-2', name: 'Flow two', enabled: true, valid: true },
]

function wireFlow(id: string, name: string): WireFlow {
  return { id, name, enabled: true, nodes: [{ id: 'feed', type: 'feed', feed: 'inbox' }], wires: [] }
}

async function mountFlowsView() {
  const wrapper = mount(FlowsView)
  await flushPromises()
  return wrapper
}

describe('FlowsView flow selector', () => {
  beforeEach(() => {
    // useFlowsSession is a module singleton — without a reset, a later
    // test's mount would silently reuse a prior test's already-torn-down
    // instance (see useFlowsSession.ts's module docs).
    resetFlowsSessionForTests()
    vi.clearAllMocks()
    mocks.ListFlows.mockResolvedValue(flowSummaries)
    mocks.GetFlow.mockImplementation(async (id: string) => wireFlow(id, id === 'flow-2' ? 'Flow two' : 'Flow one'))
    mocks.GetLayout.mockResolvedValue({ nodes: {} })
    mocks.NodeRuns.mockResolvedValue([])
    mocks.ListUnarchivedInboxItems.mockResolvedValue([])
    mocks.ListReplaySourceSnapshots.mockResolvedValue([])
    mocks.EventLogTailOffset.mockResolvedValue('0')
    mocks.ActivateReplay.mockResolvedValue(undefined)
    mocks.ReadFrom.mockResolvedValue([])
    mocks.Commit.mockResolvedValue(undefined)
    mocks.On.mockReturnValue(() => {})
  })

  it('does not expose the in-canvas new-flow input or Add button', async () => {
    const wrapper = await mountFlowsView()

    await wrapper.get('[data-testid="flow-selector-toggle"]').trigger('click')

    expect(wrapper.find('[data-testid="flow-selector-menu"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="flow-selector-new-name"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="flow-selector-new-submit"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('still lists existing flows, and clicking one switches the active flow (mirrors the profile rail)', async () => {
    const wrapper = await mountFlowsView()
    useFlowsSession().bindActiveFlow('flow-1')
    await flushPromises()

    expect(wrapper.get('[data-testid="flow-selector-toggle"]').text()).toContain('Flow one')

    await wrapper.get('[data-testid="flow-selector-toggle"]').trigger('click')
    await wrapper.get('[data-testid="flow-selector-option-flow-2"]').trigger('click')
    await flushPromises()

    expect(mocks.GetFlow).toHaveBeenCalledWith('flow-2')
    expect(wrapper.get('[data-testid="flow-selector-toggle"]').text()).toContain('Flow two')
    // Picking a flow also closes the menu.
    expect(wrapper.find('[data-testid="flow-selector-menu"]').exists()).toBe(false)

    wrapper.unmount()
  })
})

describe('FlowsView deploy menu', () => {
  beforeEach(() => {
    // useFlowsSession is a module singleton — without a reset, a later
    // test's mount would silently reuse a prior test's already-torn-down
    // instance (see useFlowsSession.ts's module docs).
    resetFlowsSessionForTests()
    vi.clearAllMocks()
    mocks.ListFlows.mockResolvedValue(flowSummaries)
    mocks.GetFlow.mockImplementation(async (id: string) => wireFlow(id, id === 'flow-2' ? 'Flow two' : 'Flow one'))
    mocks.GetLayout.mockResolvedValue({ nodes: {} })
    mocks.NodeRuns.mockResolvedValue([])
    mocks.ListUnarchivedInboxItems.mockResolvedValue([])
    mocks.ListReplaySourceSnapshots.mockResolvedValue([])
    mocks.EventLogTailOffset.mockResolvedValue('0')
    mocks.ActivateReplay.mockResolvedValue(undefined)
    mocks.ReadFrom.mockResolvedValue([])
    mocks.Commit.mockResolvedValue(undefined)
    mocks.On.mockReturnValue(() => {})
  })

  async function mountWithActiveFlow() {
    const wrapper = await mountFlowsView()
    useFlowsSession().bindActiveFlow('flow-1')
    await flushPromises()
    return wrapper
  }

  it('"Refresh now" triggers an immediate manual pump via the session and closes the menu', async () => {
    const wrapper = await mountWithActiveFlow()
    mocks.ReadFrom.mockClear()

    await wrapper.get('[data-testid="deploy-menu-toggle"]').trigger('click')
    await wrapper.get('[data-testid="deploy-menu-refresh"]').trigger('click')
    await flushPromises()

    expect(mocks.ReadFrom).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="deploy-menu"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('"Refresh now" is disabled when no flow is active', async () => {
    const wrapper = await mountFlowsView()

    await wrapper.get('[data-testid="deploy-menu-toggle"]').trigger('click')

    expect(wrapper.get('[data-testid="deploy-menu-refresh"]').attributes('disabled')).toBeDefined()

    wrapper.unmount()
  })

  it('"Copy prompt" copies the rendered flows prompt', async () => {
    mocks.SetText.mockResolvedValue(undefined)
    mocks.RenderPrompt.mockResolvedValue({ id: 'flows', title: 'Flows', description: '', target: '', text: 'FLOWS PROMPT' })
    const wrapper = await mountWithActiveFlow()

    await wrapper.get('[data-testid="deploy-menu-toggle"]').trigger('click')
    await wrapper.get('[data-testid="deploy-menu-copy-prompt"]').trigger('click')
    await flushPromises()

    expect(mocks.RenderPrompt).toHaveBeenCalledWith('flows', expect.anything())
    expect(mocks.SetText).toHaveBeenCalledWith('FLOWS PROMPT')
    expect(wrapper.get('[data-testid="copy-prompt-status"]').text()).toBe('Prompt copied')

    wrapper.unmount()
  })

  // A prompt that cannot be rendered must not put a half-built or stale prompt
  // on the clipboard.
  it('"Copy prompt" reports failure when the prompt cannot be rendered', async () => {
    mocks.SetText.mockResolvedValue(undefined)
    mocks.RenderPrompt.mockRejectedValue(new Error('unavailable'))
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const wrapper = await mountWithActiveFlow()

    await wrapper.get('[data-testid="deploy-menu-toggle"]').trigger('click')
    await wrapper.get('[data-testid="deploy-menu-copy-prompt"]').trigger('click')
    await flushPromises()

    expect(mocks.SetText).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="copy-prompt-status"]').text()).toBe('Could not copy')

    warn.mockRestore()
    wrapper.unmount()
  })

  it('debug toggle is labeled "Show debug panel" / "Hide debug panel" and toggles FlowDebugPanel', async () => {
    const wrapper = await mountWithActiveFlow()

    await wrapper.get('[data-testid="deploy-menu-toggle"]').trigger('click')
    expect(wrapper.get('[data-testid="deploy-menu-debug-toggle"]').text()).toBe('Show debug panel')
    expect(wrapper.find('[data-testid="flow-debug-aside"]').exists()).toBe(false)

    await wrapper.get('[data-testid="deploy-menu-debug-toggle"]').trigger('click')
    expect(wrapper.find('[data-testid="flow-debug-aside"]').exists()).toBe(true)
    // Toggling also closes the menu — reopen it to check the label flipped.
    await wrapper.get('[data-testid="deploy-menu-toggle"]').trigger('click')
    expect(wrapper.get('[data-testid="deploy-menu-debug-toggle"]').text()).toBe('Hide debug panel')

    wrapper.unmount()
  })
})
