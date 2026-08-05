import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Editor from '../editor.vue'
import { defaults, validate, type Config } from '../config'

function config(overrides: Partial<Config> = {}): Config {
  return { command: 'gcx alerts list -o json', timeout: '30s', ...overrides }
}

function typeInto(wrapper: ReturnType<typeof mount>, testid: string, value: string) {
  const input = wrapper.get<HTMLInputElement | HTMLTextAreaElement>(`[data-testid="${testid}"]`).element
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('sources.exec editor', () => {
  it('renders the current command and timeout', () => {
    const wrapper = mount(Editor, { props: { config: config() } })

    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="sources.exec-editor-command"]').element.value).toBe('gcx alerts list -o json')
    expect(wrapper.get<HTMLInputElement>('[data-testid="sources.exec-editor-timeout"]').element.value).toBe('30s')
  })

  // The node runs a command on the user's machine on a timer. Whatever the
  // trust decision is, it is not one to make silently.
  it('says that it runs a command on this machine', () => {
    const wrapper = mount(Editor, { props: { config: config() } })

    expect(wrapper.get('[data-testid="sources.exec-editor-notice"]').text()).toContain('runs a command on your machine')
  })

  it('emits an immutable update:config on edit, without mutating the config prop', async () => {
    const props = { config: config() }
    const wrapper = mount(Editor, { props })

    typeInto(wrapper, 'sources.exec-editor-command', 'echo "[]"')
    await wrapper.vm.$nextTick()

    expect(props.config.command).toBe('gcx alerts list -o json')
    expect(wrapper.emitted('update:config')).toEqual([[{ command: 'echo "[]"', timeout: '30s' }]])
  })

  it('drops an emptied optional field rather than writing an empty string', async () => {
    const wrapper = mount(Editor, { props: { config: config({ cwd: '~/src' }) } })

    typeInto(wrapper, 'sources.exec-editor-cwd', '')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:config')).toEqual([[{ command: 'gcx alerts list -o json', timeout: '30s', cwd: undefined }]])
  })

  it('edits environment variables as name/value rows', async () => {
    const wrapper = mount(Editor, { props: { config: config({ env: { TOKEN: 'abc' } }) } })

    typeInto(wrapper, 'sources.exec-editor-env-value-0', 'xyz')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:config')).toEqual([[{ command: 'gcx alerts list -o json', timeout: '30s', env: { TOKEN: 'xyz' } }]])
  })

  it('removes the last environment variable by unsetting the map', async () => {
    const wrapper = mount(Editor, { props: { config: config({ env: { TOKEN: 'abc' } }) } })

    await wrapper.get('[data-testid="sources.exec-editor-env-remove-0"]').trigger('click')

    expect(wrapper.emitted('update:config')).toEqual([[{ command: 'gcx alerts list -o json', timeout: '30s', env: undefined }]])
  })
})

describe('sources.exec validate', () => {
  it('reports the unset required fields', () => {
    expect(validate(defaults)).toEqual(['a command is required'])
    expect(validate({ ...defaults, timeout: '' })).toEqual(['a command is required', 'a timeout is required, like "30s"'])
  })

  // Go reads a bare number as nanoseconds and rejects it; the editor must not
  // let one through as if it meant seconds.
  it('rejects a bare number for a duration', () => {
    expect(validate(config({ timeout: '30' }))).toEqual(['timeout must be a duration like "30s", not a bare number'])
    expect(validate(config({ interval: '60' }))).toEqual(['interval must be a duration like "1h", not a bare number'])
  })

  it('rejects a relative working directory', () => {
    expect(validate(config({ cwd: 'src/repo' }))).toEqual(['cwd must be an absolute path'])
  })

  it('accepts a complete config', () => {
    expect(validate(config({ cwd: '~/src', interval: '1h', env: { TOKEN: 'x' } }))).toEqual([])
  })
})
