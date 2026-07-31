import { describe, expect, it, beforeEach, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { useSkills } from '../useSkills'

const mocks = vi.hoisted(() => ({
  Catalog: vi.fn(),
  InstallTarget: vi.fn(),
  SetAutoUpdate: vi.fn(),
  SetTargetDir: vi.fn(),
  Sync: vi.fn(),
  UninstallTarget: vi.fn(),
  On: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/skillsservice', () => ({
  Catalog: mocks.Catalog,
  InstallTarget: mocks.InstallTarget,
  SetAutoUpdate: mocks.SetAutoUpdate,
  SetTargetDir: mocks.SetTargetDir,
  Sync: mocks.Sync,
  UninstallTarget: mocks.UninstallTarget,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.On },
}))

function withSkills() {
  let api!: ReturnType<typeof useSkills>
  const wrapper = mount(defineComponent({
    setup() {
      api = useSkills()
      return () => null
    },
  }))
  return { api, wrapper }
}

describe('useSkills', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.On.mockReturnValue(() => {})
  })

  it('re-reads the catalog on settings:updated', async () => {
    let handler: (() => void) | undefined
    mocks.On.mockImplementation((event: string, callback: () => void) => {
      if (event === 'settings:updated') handler = callback
      return () => {}
    })
    const initial = { skills: [], targets: [], autoUpdate: false }
    const updated = { skills: [], targets: [], autoUpdate: true }
    mocks.Catalog.mockResolvedValueOnce(initial).mockResolvedValueOnce(updated)
    const { api, wrapper } = withSkills()

    await api.refresh()
    handler?.()
    await flushPromises()

    expect(mocks.Catalog).toHaveBeenCalledTimes(2)
    expect(api.catalog.value).toEqual(updated)
    wrapper.unmount()
  })
})
