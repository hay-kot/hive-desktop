import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import PromptSettingsView from '../PromptSettingsView.vue'

const mocks = vi.hoisted(() => ({
  Catalog: vi.fn(),
  Render: vi.fn(),
  SetText: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/desktop/promptsservice', () => ({
  Catalog: mocks.Catalog,
  Render: mocks.Render,
}))

vi.mock('@wailsio/runtime', () => ({
  Clipboard: { SetText: mocks.SetText },
}))

function prompt(id: string, overrides: Record<string, string> = {}) {
  return {
    id,
    title: `${id} title`,
    description: `${id} description`,
    target: `/config/${id}.yaml`,
    text: `${id} PROMPT BODY`,
    ...overrides,
  }
}

beforeEach(() => {
  mocks.Catalog.mockReset()
  mocks.Render.mockReset()
  mocks.SetText.mockReset()
  mocks.SetText.mockResolvedValue(undefined)
})

describe('PromptSettingsView', () => {
  // The page must render whatever the service reports — adding a prompt in Go
  // must not require touching this component.
  it('lists every prompt the service returns, with its description and target', async () => {
    mocks.Catalog.mockResolvedValue([prompt('flows'), prompt('actions'), prompt('keybindings')])
    const wrapper = mount(PromptSettingsView)
    await flushPromises()

    for (const id of ['flows', 'actions', 'keybindings']) {
      const card = wrapper.get(`[data-testid="prompt-${id}"]`)
      expect(card.text()).toContain(`${id} title`)
      expect(card.text()).toContain(`${id} description`)
      expect(card.text()).toContain(`/config/${id}.yaml`)
    }
  })

  it('passes the bindable command catalog to the service', async () => {
    mocks.Catalog.mockResolvedValue([])
    mount(PromptSettingsView)
    await flushPromises()

    const input = mocks.Catalog.mock.calls[0]![0] as { commands: { id: string }[] }
    expect(input.commands.length).toBeGreaterThan(0)
    expect(input.commands.map((c) => c.id)).toContain('feed.next')
  })

  it('copies a prompt body to the clipboard and confirms on that row only', async () => {
    mocks.Catalog.mockResolvedValue([prompt('flows'), prompt('actions')])
    const wrapper = mount(PromptSettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="prompt-flows-copy"]').trigger('click')
    await flushPromises()

    expect(mocks.SetText).toHaveBeenCalledWith('flows PROMPT BODY')
    expect(wrapper.get('[data-testid="prompt-flows-copy"]').text()).toBe('Copied')
    expect(wrapper.get('[data-testid="prompt-actions-copy"]').text()).toBe('Copy')
  })

  it('surfaces a clipboard failure instead of silently doing nothing', async () => {
    mocks.Catalog.mockResolvedValue([prompt('flows')])
    mocks.SetText.mockRejectedValue(new Error('no clipboard'))
    const wrapper = mount(PromptSettingsView)
    await flushPromises()

    await wrapper.get('[data-testid="prompt-flows-copy"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="prompt-flows-copy-error"]').exists()).toBe(true)
  })

  // The preview exists so a user can check a prompt before pasting it into an
  // agent.
  it('reveals the full prompt text on preview, one at a time', async () => {
    mocks.Catalog.mockResolvedValue([prompt('flows'), prompt('actions')])
    const wrapper = mount(PromptSettingsView)
    await flushPromises()

    expect(wrapper.find('[data-testid="prompt-flows-text"]').exists()).toBe(false)

    await wrapper.get('[data-testid="prompt-flows-preview"]').trigger('click')
    expect(wrapper.get('[data-testid="prompt-flows-text"]').text()).toBe('flows PROMPT BODY')

    await wrapper.get('[data-testid="prompt-actions-preview"]').trigger('click')
    expect(wrapper.find('[data-testid="prompt-flows-text"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="prompt-actions-text"]').exists()).toBe(true)

    await wrapper.get('[data-testid="prompt-actions-preview"]').trigger('click')
    expect(wrapper.find('[data-testid="prompt-actions-text"]').exists()).toBe(false)
  })

  it('reports a load failure rather than rendering an empty page', async () => {
    mocks.Catalog.mockRejectedValue(new Error('service unavailable'))
    const wrapper = mount(PromptSettingsView)
    await flushPromises()

    expect(wrapper.get('[data-testid="prompt-settings-error"]').text()).toContain('service unavailable')
  })
})
