import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Editor, { type WebhookCaptureView, type WebhookEditorClient } from '../editor.vue'
import type { Config } from '../config'

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

describe('webhook-source editor', () => {
  it('renders the endpoint URL from the listener info and the configured path', async () => {
    const wrapper = mountEditor({ path: 'ci-alerts' })
    await flushPromises()
    expect(wrapper.get('[data-testid="webhook-source-editor-url"]').text()).toBe('http://127.0.0.1:4483/hooks/ci-alerts')
  })

  it('emits an immutable update:config on path edit', async () => {
    const config: Config = { path: '' }
    const wrapper = mountEditor(config)
    const input = wrapper.get<HTMLInputElement>('[data-testid="webhook-source-editor-path"]').element
    input.value = 'ci'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(config.path).toBe('')
    expect(wrapper.emitted('update:config')).toEqual([[{ path: 'ci' }]])
  })

  it('shows the placeholder when nothing was captured yet', async () => {
    const wrapper = mountEditor({ path: 'ci' })
    await flushPromises()
    expect(wrapper.find('[data-testid="webhook-source-editor-no-capture"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="webhook-source-editor-capture"]').exists()).toBe(false)
  })

  it('shows the captured payload with the non-blocking shape warning', async () => {
    const wrapper = mountEditor({ path: 'ci' }, {
      receivedAt: 1750000000000,
      body: '{"event":"deploy"}',
      feedShaped: false,
      missingFields: ['id', 'kind', 'repo', 'title', 'url'],
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="webhook-source-editor-capture"]').text()).toContain('"event": "deploy"')
    const warning = wrapper.get('[data-testid="webhook-source-editor-shape-warning"]').text()
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
    expect(wrapper.find('[data-testid="webhook-source-editor-shape-warning"]').exists()).toBe(false)
  })
})
