import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Editor, { type WebhookCaptureView, type WebhookEditorClient } from '../editor.vue'
import type { Config } from '../config'

// The transform prompt is assembled by the Go prompts service; this editor
// only supplies the endpoint path and the last captured delivery.
const mocks = vi.hoisted(() => ({ Render: vi.fn(), SetText: vi.fn() }))

vi.mock('../../../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/promptsservice', () => ({
  Catalog: vi.fn(),
  Render: mocks.Render,
}))

vi.mock('@wailsio/runtime', () => ({ Clipboard: { SetText: mocks.SetText } }))

beforeEach(() => {
  mocks.Render.mockReset()
  mocks.SetText.mockReset()
  mocks.SetText.mockResolvedValue(undefined)
  mocks.Render.mockResolvedValue({ id: 'webhook-transform', title: '', description: '', target: '', text: 'TRANSFORM PROMPT' })
})

function fakeClient(capture?: Partial<WebhookCaptureView>): WebhookEditorClient {
  return {
    async info() {
      return { running: true, port: 4483, baseUrl: 'http://127.0.0.1:4483/hooks/' }
    },
    async capture() {
      return { receivedAt: 0, body: '', feedShaped: false, missingFields: [], ...capture }
    },
  }
}

function mountEditor(config: Config, capture?: Partial<WebhookCaptureView>) {
  return mount(Editor, {
    props: { config, flowId: 'triage', nodeId: 'hook', client: fakeClient(capture) },
  })
}

describe('sources.webhook editor', () => {
  it('renders the endpoint URL from the listener info and the configured path', async () => {
    const wrapper = mountEditor({ path: 'ci-alerts' })
    await flushPromises()
    expect(wrapper.get('[data-testid="sources.webhook-editor-url"]').text()).toBe('http://127.0.0.1:4483/hooks/ci-alerts')
  })

  it('emits an immutable update:config on path edit', async () => {
    const config: Config = { path: '' }
    const wrapper = mountEditor(config)
    const input = wrapper.get<HTMLInputElement>('[data-testid="sources.webhook-editor-path"]').element
    input.value = 'ci'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(config.path).toBe('')
    expect(wrapper.emitted('update:config')).toEqual([[{ path: 'ci' }]])
  })

  it('regenerates the path in place, emitting a valid slug without touching the secret', async () => {
    const config: Config = { path: 'ci', secret: 'keep-me' }
    const wrapper = mountEditor(config)
    await wrapper.get('[data-testid="sources.webhook-editor-path-generate"]').trigger('click')

    const emitted = wrapper.emitted('update:config') as [[Config]]
    expect(emitted[0]![0].path).toMatch(/^hook-[a-z0-9]{8}$/)
    expect(emitted[0]![0].path).not.toBe('ci')
    expect(emitted[0]![0].secret).toBe('keep-me')
    expect(config.path).toBe('ci')
  })

  it('regenerates the secret in place, emitting a printable value without touching the path', async () => {
    const config: Config = { path: 'ci' }
    const wrapper = mountEditor(config)
    await wrapper.get('[data-testid="sources.webhook-editor-secret-generate"]').trigger('click')

    const emitted = wrapper.emitted('update:config') as [[Config]]
    expect(emitted[0]![0].secret).toMatch(/^[!-~]{32}$/)
    expect(emitted[0]![0].path).toBe('ci')
    expect(config.secret).toBeUndefined()
  })

  it('shows the placeholder when nothing was captured yet', async () => {
    const wrapper = mountEditor({ path: 'ci' })
    await flushPromises()
    expect(wrapper.find('[data-testid="sources.webhook-editor-no-capture"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="sources.webhook-editor-capture"]').exists()).toBe(false)
  })

  it('shows the captured payload with the non-blocking shape warning', async () => {
    const wrapper = mountEditor({ path: 'ci' }, {
      receivedAt: 1750000000000,
      body: '{"event":"deploy"}',
      feedShaped: false,
      missingFields: ['id', 'kind', 'repo', 'title', 'url'],
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="sources.webhook-editor-capture"]').text()).toContain('"event": "deploy"')
    const warning = wrapper.get('[data-testid="sources.webhook-editor-shape-warning"]').text()
    expect(warning).toContain('Missing canonical item fields')
    expect(warning).toContain('id, kind, repo, title, url')
    expect(warning).toContain('renders minimally')
    expect(warning).toContain('ADR 0008')
    expect(warning).toContain('auto-archive')
  })

  it('hides the shape warning for feed-shaped captures', async () => {
    const wrapper = mountEditor({ path: 'ci' }, {
      receivedAt: 1750000000000,
      body: '{"id":"1","kind":"PR","repo":"o/r","title":"t","url":"https://x"}',
      feedShaped: true,
      missingFields: null,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="sources.webhook-editor-shape-warning"]').exists()).toBe(false)
  })

  it('copies the service-rendered transform prompt, scoped to this node', async () => {
    const wrapper = mountEditor({ path: 'ci-alerts' }, { receivedAt: 5, body: '{"event":"deploy"}' })
    await flushPromises()

    await wrapper.get('[data-testid="sources.webhook-editor-copy-prompt"]').trigger('click')
    await flushPromises()

    expect(mocks.Render).toHaveBeenCalledWith('webhook-transform', expect.objectContaining({
      webhookPath: 'ci-alerts',
      webhookSample: '{"event":"deploy"}',
    }))
    expect(mocks.SetText).toHaveBeenCalledWith('TRANSFORM PROMPT')
  })

  it('passes an empty sample when nothing has been captured yet', async () => {
    const wrapper = mountEditor({ path: 'ci-alerts' })
    await flushPromises()

    await wrapper.get('[data-testid="sources.webhook-editor-copy-prompt"]').trigger('click')
    await flushPromises()

    expect(mocks.Render).toHaveBeenCalledWith('webhook-transform', expect.objectContaining({ webhookSample: '' }))
  })
})
