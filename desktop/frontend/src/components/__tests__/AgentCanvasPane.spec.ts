import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentCanvasPane from '../AgentCanvasPane.vue'
import { openSelect } from '../../test-utils/select'
import type { AgentWorkspacesClient, CanvasBlock, ChatCanvasMeta } from '../../lib/agentWorkspacesClient'

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

function block(overrides: Partial<CanvasBlock>): CanvasBlock {
  return { id: 'b', kind: 'markdown', title: '', body: '', url: '', createdAt: 1, updatedAt: 1, ...overrides }
}

function fakeCanvasClient(blocks: CanvasBlock[], metas: ChatCanvasMeta[] = []) {
  return {
    canvas: vi.fn().mockResolvedValue({ workspace: 'web-app', session: 7, createdAt: 1, updatedAt: 1, blocks }),
    canvases: vi.fn().mockResolvedValue(metas),
  } as unknown as AgentWorkspacesClient
}

async function mountPane(client: AgentWorkspacesClient, sessionNames: Record<number, string> = { 7: 'New Chat' }) {
  const wrapper = mount(AgentCanvasPane, {
    props: { session: 7, workspace: 'web-app', sessionNames, client },
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

  it('shows the empty state for a canvas with no blocks', async () => {
    const wrapper = await mountPane(fakeCanvasClient([]))
    expect(wrapper.find('[data-testid="agent-canvas-empty"]').exists()).toBe(true)
  })

  // The picker labels canvases by session name; a canvas whose session record
  // is gone still lists, labeled by date instead of by a name nothing has.
  it('labels an orphaned canvas by date in the picker', async () => {
    const metas: ChatCanvasMeta[] = [
      { workspace: 'web-app', session: 7, createdAt: 1, updatedAt: Date.now(), blockCount: 1 },
      { workspace: 'web-app', session: 9, createdAt: 1, updatedAt: Date.now(), blockCount: 2 },
    ]
    const wrapper = await mountPane(fakeCanvasClient([], metas))

    const popover = await openSelect(wrapper, 'agent-canvas-picker')
    const labels = Array.from(popover.querySelectorAll('[role="option"]')).map((option) => option.textContent?.trim())
    expect(labels).toContain('New Chat')
    expect(labels?.some((label) => label?.startsWith('Canvas ·'))).toBe(true)
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
