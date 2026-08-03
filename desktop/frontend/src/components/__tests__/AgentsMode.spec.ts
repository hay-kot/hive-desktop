import { createMemoryHistory } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentsMode from '../AgentsMode.vue'
import { resetAgentWorkspacesForTests } from '../../composables/useAgentWorkspaces'
import { createAppRouter } from '../../router'

// App.vue mounts AgentsMode once and hides it with v-show on a trip to the
// hub (ADR 0054): the component itself must never re-key or v-if anything
// internal to props.active, or a v-show'd parent would still pay for a
// rebuild on every round trip. This asserts that at the component level —
// App.spec.ts separately proves the parent uses v-show rather than v-if.

const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  Endpoint: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice', () => ({
  Available: mocks.Available,
  Endpoint: mocks.Endpoint,
}))

async function mountAgentsMode() {
  const router = createAppRouter(createMemoryHistory())
  await router.push('/workspaces')
  await router.isReady()
  const wrapper = mount(AgentsMode, { global: { plugins: [router] }, props: { active: true } })
  await flushPromises()
  return wrapper
}

describe('AgentsMode', () => {
  beforeEach(() => {
    resetAgentWorkspacesForTests()
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.Endpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' })
  })

  it('keeps the same root and sidebar elements across an active toggle', async () => {
    const wrapper = await mountAgentsMode()

    const rootBefore = wrapper.find('[data-testid="agents-mode"]').element
    const sidebarBefore = wrapper.find('[data-testid="agents-workspace-sidebar"]').element

    await wrapper.setProps({ active: false })
    await flushPromises()
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(wrapper.find('[data-testid="agents-mode"]').element).toBe(rootBefore)
    expect(wrapper.find('[data-testid="agents-workspace-sidebar"]').element).toBe(sidebarBefore)
  })

  it('reports the unavailable reason and offers a retry when ptyterm is unavailable', async () => {
    mocks.Available.mockResolvedValue({ available: false, reason: 'agent workspaces need macOS or Linux and a desktop build.' })
    const wrapper = await mountAgentsMode()

    expect(wrapper.find('[data-testid="agents-unavailable"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="agents-unavailable-reason"]').text())
      .toBe('agent workspaces need macOS or Linux and a desktop build.')
    expect(wrapper.find('[data-testid="agents-workspace-sidebar"]').exists()).toBe(false)
  })
})
