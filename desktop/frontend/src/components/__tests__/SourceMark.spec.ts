import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SourceMark from '../SourceMark.vue'

// A stand-in glyph so the fallback branch is identifiable in the DOM.
const Glyph = { template: '<svg data-testid="glyph" />' }

describe('SourceMark', () => {
  it('renders the glyph when no image is set', () => {
    const wrapper = mount(SourceMark, { props: { icon: Glyph } })
    expect(wrapper.find('[data-testid="glyph"]').exists()).toBe(true)
    expect(wrapper.find('img').exists()).toBe(false)
  })

  it('renders the image over the glyph when one is provided', () => {
    const src = 'data:image/png;base64,LOGO'
    const wrapper = mount(SourceMark, { props: { icon: Glyph, image: src } })
    expect(wrapper.find('img').attributes('src')).toBe(src)
    expect(wrapper.find('[data-testid="glyph"]').exists()).toBe(false)
  })

  it('falls back to the glyph when the image fails to load', async () => {
    const wrapper = mount(SourceMark, { props: { icon: Glyph, image: 'data:image/png;base64,BROKEN' } })
    await wrapper.find('img').trigger('error')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('[data-testid="glyph"]').exists()).toBe(true)
  })

  it('retries the image after the source changes to a new one', async () => {
    const wrapper = mount(SourceMark, { props: { icon: Glyph, image: 'data:image/png;base64,BROKEN' } })
    await wrapper.find('img').trigger('error')
    expect(wrapper.find('img').exists()).toBe(false)

    await wrapper.setProps({ image: 'data:image/png;base64,FRESH' })
    expect(wrapper.find('img').attributes('src')).toBe('data:image/png;base64,FRESH')
  })
})
