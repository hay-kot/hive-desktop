import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Editor from '../editor.vue'
import { defaults, descriptionMaxLen, notifyBodyMaxLen, notifySink, notifyTitleMaxLen, sink, validate } from '../config'

describe('feed editor', () => {
  it('renders the feed node body with icon and description fields', () => {
    const wrapper = mount(Editor, { props: { config: {} } })
    expect(wrapper.get('[data-testid="feed-node-editor"]').text()).toContain('FEEDS')
    expect(wrapper.find('[data-testid="feed-editor-icon"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="feed-editor-description"]').exists()).toBe(true)
  })

  it('emits the chosen icon from the searchable picker', async () => {
    const wrapper = mount(Editor, { props: { config: {} } })
    await wrapper.get('[data-testid="feed-editor-icon"]').trigger('click')
    await wrapper.get('[data-testid="feed-editor-icon-option-sparkles"]').trigger('click')
    const events = wrapper.emitted('update:config')
    expect(events?.at(-1)?.[0]).toEqual({ icon: 'sparkles' })
  })

  it('emits the typed description', async () => {
    const wrapper = mount(Editor, { props: { config: {} } })
    await wrapper.get('[data-testid="feed-editor-description"]').setValue('Team PRs')
    const events = wrapper.emitted('update:config')
    expect(events?.at(-1)?.[0]).toEqual({ description: 'Team PRs' })
  })

  it('clears a field back to undefined when emptied', async () => {
    const wrapper = mount(Editor, { props: { config: { description: 'x' } } })
    await wrapper.get('[data-testid="feed-editor-description"]').setValue('')
    const events = wrapper.emitted('update:config')
    expect(events?.at(-1)?.[0]).toEqual({ description: undefined })
  })
})

describe('feed notify editor', () => {
  it('hides the notification options until notifications are turned on', () => {
    const wrapper = mount(Editor, { props: { config: {} } })
    expect(wrapper.get<HTMLInputElement>('[data-testid="feed-editor-notify"]').element.checked).toBe(false)
    expect(wrapper.find('[data-testid="feed-editor-notify-options"]').exists()).toBe(false)

    const notifying = mount(Editor, { props: { config: { notify: { title: 'Review requested' } } } })
    expect(notifying.get<HTMLInputElement>('[data-testid="feed-editor-notify"]').element.checked).toBe(true)
    expect(notifying.find('[data-testid="feed-editor-notify-options"]').exists()).toBe(true)
  })

  it('turning notifications on and off adds and drops the whole block', async () => {
    const wrapper = mount(Editor, { props: { config: {} } })
    // On seeds the title a notification needs rather than starting invalid.
    await wrapper.get('[data-testid="feed-editor-notify"]').setValue(true)
    expect(wrapper.emitted('update:config')?.at(-1)?.[0]).toEqual({ notify: { title: '{{ .Payload.title }}' } })

    // Off drops the block rather than leaving its settings behind: presence is
    // the switch, so a stale block would silently notify again when re-enabled.
    const notifying = mount(Editor, { props: { config: { notify: { title: 'Review requested' } } } })
    await notifying.get('[data-testid="feed-editor-notify"]').setValue(false)
    expect(notifying.emitted('update:config')?.at(-1)?.[0]).toEqual({ notify: undefined })
  })

  it('emits the typed templates and clears an emptied one back to undefined', async () => {
    const wrapper = mount(Editor, { props: { config: { notify: { title: 'Review requested' } } } })
    await wrapper.get('[data-testid="feed-editor-notify-body"]').setValue('{{ .Payload.repo }}')
    expect(wrapper.emitted('update:config')?.at(-1)?.[0]).toEqual({ notify: { title: 'Review requested', body: '{{ .Payload.repo }}' } })

    // Title is required, so an emptied one stays empty rather than vanishing —
    // validation then flags it, matching the notify node's own rule.
    await wrapper.get('[data-testid="feed-editor-notify-title"]').setValue('')
    expect(wrapper.emitted('update:config')?.at(-1)?.[0]).toEqual({ notify: { title: '' } })
  })

  it('omits the notification defaults rather than writing them out', async () => {
    const wrapper = mount(Editor, { props: { config: { notify: { title: 'x', severity: 'warning' } } } })
    await wrapper.get('[data-testid="feed-editor-notify-severity"]').setValue('info')
    expect(wrapper.emitted('update:config')?.at(-1)?.[0]).toEqual({ notify: { title: 'x', severity: undefined } })

    const silenced = mount(Editor, { props: { config: { notify: { title: 'x', sound: false } } } })
    await silenced.get('[data-testid="feed-editor-notify-sound"]').setValue(true)
    expect(silenced.emitted('update:config')?.at(-1)?.[0]).toEqual({ notify: { title: 'x', sound: undefined } })
  })

  it('emits an explicit false when a feed silences itself', async () => {
    const wrapper = mount(Editor, { props: { config: { notify: { title: 'Review requested' } } } })
    await wrapper.get('[data-testid="feed-editor-notify-sound"]').setValue(false)
    expect(wrapper.emitted('update:config')?.at(-1)?.[0]).toEqual({ notify: { title: 'Review requested', sound: false } })
  })
})

describe('feed config', () => {
  it('accepts empty config', () => {
    expect(validate(defaults)).toEqual([])
  })

  it('accepts a supported icon', () => {
    expect(validate({ icon: 'sparkles' })).toEqual([])
  })

  it('rejects an unsupported icon', () => {
    expect(validate({ icon: 'not-an-icon' })).toHaveLength(1)
  })

  it('rejects an over-long description', () => {
    expect(validate({ description: 'x'.repeat(descriptionMaxLen + 1) })).toHaveLength(1)
  })

  it('accepts a notify block and its templates', () => {
    expect(validate({ notify: { title: 'Review requested' } })).toEqual([])
    expect(validate({ notify: { title: 'Review requested', body: '{{ .Payload.repo }}', severity: 'warning' } })).toEqual([])
  })

  it('rejects over-long notification templates and an unsupported severity', () => {
    expect(validate({ notify: { title: 'x'.repeat(notifyTitleMaxLen + 1) } })).toHaveLength(1)
    expect(validate({ notify: { title: 'x', body: 'x'.repeat(notifyBodyMaxLen + 1) } })).toHaveLength(1)
    expect(validate({ notify: { title: 'x', severity: 'critical' } })).toHaveLength(1)
  })

  it('sinks to the flow-qualified node id', () => {
    expect(sink('triage', 'team-feed')).toEqual({ kind: 'feed', targetId: 'triage/team-feed' })
  })

  it('notifies under the same id, since the feed is what asked to interrupt', () => {
    expect(notifySink('triage', 'team-feed')).toEqual({ kind: 'notify', targetId: 'triage/team-feed' })
  })
})
