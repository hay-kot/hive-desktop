import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import NewSessionDialog from '../NewSessionDialog.vue'

const options = {
  repositories: [{ name: 'hive', repository: 'https://github.com/hay-kot/hive-desktop.git' }],
  defaultRepository: 'https://github.com/hay-kot/hive-desktop.git',
  agents: ['claude', 'pi'],
  defaultAgent: 'claude',
}

const blank = { repository: '', name: '', prompt: '' }

function mountDialog(overrides: Record<string, unknown> = {}) {
  return mount(NewSessionDialog, { attachTo: document.body, props: { options, initial: blank, busy: false, error: null, ...overrides }, global: { stubs: { Teleport: true } } })
}

describe('NewSessionDialog', () => {
  it('prefills from the draft and emits repository, name, prompt, and agent', async () => {
    const wrapper = mountDialog({ initial: { repository: 'acme/site', name: 'fix-crash', prompt: 'Fix the crash' } })
    await wrapper.get('[data-testid="new-session-submit"]').trigger('click')
    expect(wrapper.emitted('submit')).toEqual([[{ repository: 'acme/site', name: 'fix-crash', prompt: 'Fix the crash', agent: 'claude' }]])
  })

  it('defaults the repository to the backend default and allows an empty prompt', async () => {
    const wrapper = mountDialog()
    await wrapper.get('[data-testid="new-session-name"]').setValue('standalone')
    await wrapper.get('[data-testid="new-session-submit"]').trigger('click')
    expect(wrapper.emitted('submit')).toEqual([[{ repository: options.defaultRepository, name: 'standalone', prompt: '', agent: 'claude' }]])
  })

  it('picks a repository through the shared selector', async () => {
    const wrapper = mountDialog({
      options: { ...options, repositories: [...options.repositories, { name: 'site', repository: 'https://github.com/acme/site.git' }] },
    })
    await wrapper.get('[data-testid="new-session-repository"]').trigger('click')
    await wrapper.get('[data-testid="new-session-repository-search"]').setValue('acme')
    await wrapper.get('[data-testid="new-session-repository-option"]').trigger('click')
    await wrapper.get('[data-testid="new-session-name"]').setValue('fix-crash')
    await wrapper.get('[data-testid="new-session-submit"]').trigger('click')

    expect(wrapper.emitted('submit')).toEqual([[{ repository: 'https://github.com/acme/site.git', name: 'fix-crash', prompt: '', agent: 'claude' }]])
  })

  it('keeps the dialog open and reports invalid names locally', async () => {
    const wrapper = mountDialog()
    await wrapper.get('[data-testid="new-session-name"]').setValue('bad@name')
    await wrapper.get('[data-testid="new-session-submit"]').trigger('click')
    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(wrapper.get('[data-testid="new-session-error"]').text()).toContain('Use letters')
  })
})
