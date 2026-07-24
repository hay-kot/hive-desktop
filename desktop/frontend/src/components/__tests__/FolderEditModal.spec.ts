import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import FolderEditModal from '../FolderEditModal.vue'
import type { FeedFolder } from '../../types/feed'

const work: FeedFolder = {
  id: 'work',
  name: 'Work',
  feeds: [{ id: 'backend', name: 'Backend', count: 1, newCount: 0 }],
}

// The modal teleports to the body.
function el<T extends HTMLElement>(testid: string): T {
  return document.querySelector<T>(`[data-testid="${testid}"]`)!
}

function mountModal(folder: FeedFolder = work) {
  return mount(FolderEditModal, { props: { folder }, attachTo: document.body })
}

beforeEach(() => { document.body.innerHTML = '' })

describe('FolderEditModal', () => {
  it('opens with the name focused and selected, and saves the trimmed name', async () => {
    const wrapper = mountModal()
    await wrapper.vm.$nextTick()

    const input = el<HTMLInputElement>('folder-edit-name')
    expect(document.activeElement).toBe(input)
    expect(input.selectionStart).toBe(0)
    expect(input.selectionEnd).toBe('Work'.length)

    input.value = '  Projects  '
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()
    el<HTMLButtonElement>('folder-edit-save').click()

    expect(wrapper.emitted('save')).toEqual([['Projects']])
    wrapper.unmount()
  })

  it('refuses to save a blank name', async () => {
    const wrapper = mountModal()
    const input = el<HTMLInputElement>('folder-edit-name')
    input.value = '   '
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(el<HTMLButtonElement>('folder-edit-save').disabled).toBe(true)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(wrapper.emitted('save')).toBeUndefined()
    wrapper.unmount()
  })

  it('states where the folder\'s feeds go, singular and plural, without deleting anything itself', async () => {
    const wrapper = mountModal()
    expect(el('folder-edit-modal').textContent).toContain('The 1 feed inside move')

    el<HTMLButtonElement>('folder-edit-delete').click()
    // Delete is a request the parent confirms; the modal never mutates a tree.
    expect(wrapper.emitted('delete')).toHaveLength(1)
    wrapper.unmount()

    const two = mountModal({ ...work, feeds: [...work.feeds, { id: 'ui', name: 'UI', count: 0, newCount: 0 }] })
    expect(el('folder-edit-modal').textContent).toContain('The 2 feeds inside move')
    two.unmount()

    const empty = mountModal({ ...work, feeds: [] })
    expect(el('folder-edit-modal').textContent).toContain('This folder is empty')
    empty.unmount()
  })
})
