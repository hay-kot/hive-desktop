import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentCanvasPane from '../AgentCanvasPane.vue'
import type { AgentWorkspacesClient, CanvasBlock, WorkspaceCanvasMeta } from '../../lib/agentWorkspacesClient'

const wailsEvents = vi.hoisted(() => ({
  handlers: [] as Array<[string, (event: { data: unknown }) => void]>,
  fire(name: string, data: unknown) {
    for (const [registered, handler] of this.handlers) {
      if (registered === name) handler({ data })
    }
  },
}))
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: (name: string, handler: (event: { data: unknown }) => void) => {
    wailsEvents.handlers.push([name, handler])
  },
}))

const runtime = vi.hoisted(() => ({
  setText: vi.fn().mockResolvedValue(undefined),
  saveFile: vi.fn().mockResolvedValue('/tmp/plan.md'),
}))
vi.mock('@wailsio/runtime', () => ({
  Clipboard: { SetText: runtime.setText },
  Dialogs: { SaveFile: runtime.saveFile },
}))

function block(overrides: Partial<CanvasBlock>): CanvasBlock {
  return { id: 'b', kind: 'markdown', title: '', body: '', url: '', createdAt: 1, updatedAt: 1, ...overrides }
}

function meta(overrides: Partial<WorkspaceCanvasMeta>): WorkspaceCanvasMeta {
  return { workspace: 'web-app', name: 'plan', title: '', session: 7, createdAt: 1, updatedAt: 1, blockCount: 1, ...overrides }
}

function fakeCanvasClient(blocks: CanvasBlock[], metas: WorkspaceCanvasMeta[] = [meta({})]) {
  return {
    canvas: vi.fn().mockImplementation((workspace: string, name: string) =>
      Promise.resolve({ workspace, name, title: '', session: 7, createdAt: 1, updatedAt: 1, blocks })),
    canvases: vi.fn().mockResolvedValue(metas),
    canvasMarkdown: vi.fn().mockResolvedValue('# The Plan\n\nhello\n'),
    exportCanvas: vi.fn().mockResolvedValue(undefined),
  } as unknown as AgentWorkspacesClient
}

async function mountPane(client: AgentWorkspacesClient, name: string | null = null) {
  const wrapper = mount(AgentCanvasPane, {
    props: { session: 7, workspace: 'web-app', name, client },
  })
  await flushPromises()
  return wrapper
}

describe('AgentCanvasPane', () => {
  beforeEach(() => {
    wailsEvents.handlers = []
  })

  // Agent-authored markdown is untrusted: raw HTML must arrive escaped, never
  // as live elements in the webview.
  it('renders markdown with raw HTML escaped', async () => {
    const wrapper = await mountPane(fakeCanvasClient([
      block({ id: 'doc', body: '# Plan\n\n<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>' }),
    ]))

    const body = wrapper.get('[data-testid="agent-canvas-block-doc"]')
    expect(body.find('script').exists()).toBe(false)
    expect(body.find('img').exists()).toBe(false)
    expect(body.text()).toContain('<script>alert(1)</script>')
    expect(body.find('h1').text()).toBe('Plan')
  })

  it('intercepts markdown links and emits open-url instead of navigating', async () => {
    const wrapper = await mountPane(fakeCanvasClient([
      block({ id: 'doc', body: '[the PR](https://example.com/pr/1)' }),
    ]))

    await wrapper.get('[data-testid="agent-canvas-block-doc"] a').trigger('click')

    expect(wrapper.emitted('open-url')).toEqual([['https://example.com/pr/1']])
  })

  it('opens a link block through the same scheme filter', async () => {
    const wrapper = await mountPane(fakeCanvasClient([
      block({ id: 'ok', kind: 'link', title: 'The PR', url: 'https://example.com/pr/1' }),
      block({ id: 'bad', kind: 'link', title: 'Nope', url: 'javascript:alert(1)' }),
    ]))

    await wrapper.get('[data-testid="agent-canvas-block-ok"] button').trigger('click')
    await wrapper.get('[data-testid="agent-canvas-block-bad"] button').trigger('click')

    expect(wrapper.emitted('open-url')).toEqual([['https://example.com/pr/1']])
  })

  it('shows the empty state when the workspace has no canvases', async () => {
    const wrapper = await mountPane(fakeCanvasClient([], []))
    expect(wrapper.find('[data-testid="agent-canvas-empty"]').exists()).toBe(true)
  })

  // No route-pinned name: the default pick prefers the open chat's canvas
  // over a more recently updated one another chat made.
  it('defaults to the open chat\'s canvas', async () => {
    const client = fakeCanvasClient([], [
      meta({ name: 'other-report', session: 9, updatedAt: 5 }),
      meta({ name: 'plan', session: 7, updatedAt: 3 }),
    ])
    await mountPane(client)

    expect(vi.mocked(client.canvas)).toHaveBeenCalledWith('web-app', 'plan')
  })

  it('pins the route-named canvas and lists canvases by title in the browse view', async () => {
    const client = fakeCanvasClient([], [
      meta({ name: 'plan', title: 'The Plan', session: 7 }),
      meta({ name: 'perf-report', title: '', session: 9 }),
    ])
    const wrapper = await mountPane(client, 'perf-report')

    expect(vi.mocked(client.canvas)).toHaveBeenCalledWith('web-app', 'perf-report')

    await wrapper.get('[data-testid="agent-canvas-title"]').trigger('click')
    const browse = wrapper.get('[data-testid="agent-canvas-browse"]')
    expect(browse.text()).toContain('The Plan')
    expect(browse.text()).toContain('perf-report')
    expect(wrapper.get('[data-testid="agent-canvas-browse-perf-report"]').attributes('aria-current')).toBe('true')
  })

  it('emits pick from the browse view instead of switching locally', async () => {
    const client = fakeCanvasClient([], [
      meta({ name: 'plan', title: 'The Plan', session: 7 }),
      meta({ name: 'perf-report', session: 9 }),
    ])
    const wrapper = await mountPane(client)

    await wrapper.get('[data-testid="agent-canvas-title"]').trigger('click')
    await wrapper.get('[data-testid="agent-canvas-browse-perf-report"]').trigger('click')

    expect(wrapper.emitted('pick')).toEqual([['perf-report']])
    expect(wrapper.find('[data-testid="agent-canvas-browse"]').exists()).toBe(false)
  })

  it('filters the browse list from the search input', async () => {
    const client = fakeCanvasClient([], [
      meta({ name: 'plan', title: 'The Plan' }),
      meta({ name: 'perf-report' }),
    ])
    const wrapper = await mountPane(client)

    await wrapper.get('[data-testid="agent-canvas-title"]').trigger('click')
    await wrapper.get('[data-testid="agent-canvas-search"]').setValue('perf')

    expect(wrapper.find('[data-testid="agent-canvas-browse-plan"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="agent-canvas-browse-perf-report"]').exists()).toBe(true)

    await wrapper.get('[data-testid="agent-canvas-search"]').setValue('nothing-matches')
    expect(wrapper.get('[data-testid="agent-canvas-browse"]').text()).toContain('No canvases match.')
  })

  it('groups the browse list by activity', async () => {
    const client = fakeCanvasClient([], [
      meta({ name: 'fresh', updatedAt: Date.now() }),
      meta({ name: 'stale', updatedAt: 1 }),
    ])
    const wrapper = await mountPane(client)

    await wrapper.get('[data-testid="agent-canvas-title"]').trigger('click')

    const browse = wrapper.get('[data-testid="agent-canvas-browse"]').text()
    expect(browse).toContain('Today')
    expect(browse).toContain('Older')
  })

  it('copies the Go-rendered markdown to the native clipboard', async () => {
    const client = fakeCanvasClient([])
    const wrapper = await mountPane(client)

    await wrapper.get('[data-testid="agent-canvas-copy"]').trigger('click')
    await flushPromises()

    expect(vi.mocked(client.canvasMarkdown)).toHaveBeenCalledWith('web-app', 'plan')
    expect(runtime.setText).toHaveBeenCalledWith('# The Plan\n\nhello\n')
  })

  it('saves through the native dialog and skips a cancelled one', async () => {
    const client = fakeCanvasClient([])
    const wrapper = await mountPane(client)

    await wrapper.get('[data-testid="agent-canvas-download"]').trigger('click')
    await flushPromises()
    expect(runtime.saveFile).toHaveBeenCalledWith(expect.objectContaining({ Filename: 'plan.md' }))
    expect(vi.mocked(client.exportCanvas)).toHaveBeenCalledWith('web-app', 'plan', '/tmp/plan.md')

    runtime.saveFile.mockResolvedValueOnce('')
    await wrapper.get('[data-testid="agent-canvas-download"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(client.exportCanvas)).toHaveBeenCalledTimes(1)
  })

  it('re-reads the canvas on canvas:updated', async () => {
    const client = fakeCanvasClient([])
    await mountPane(client)
    const readsBefore = vi.mocked(client.canvas).mock.calls.length

    wailsEvents.fire('canvas:updated', 7)
    await flushPromises()

    expect(vi.mocked(client.canvas).mock.calls.length).toBeGreaterThan(readsBefore)
  })
})
