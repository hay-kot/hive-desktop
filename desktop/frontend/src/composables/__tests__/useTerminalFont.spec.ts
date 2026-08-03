import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

const mocks = vi.hoisted(() => ({
  AppearanceSettings: vi.fn(),
  SetTerminalFontSize: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  AppearanceSettings: mocks.AppearanceSettings,
  SetTerminalFontSize: mocks.SetTerminalFontSize,
}))

// The size is a module singleton, so each test resets modules and imports fresh
// rather than inheriting the previous one's ladder position.
beforeEach(() => {
  vi.resetModules()
  vi.clearAllMocks()
  mocks.AppearanceSettings.mockResolvedValue({ terminalFontSize: '' })
  mocks.SetTerminalFontSize.mockResolvedValue(undefined)
})

// Yielding to a macrotask drains the hydrate and persist chains without
// counting the microtask hops inside them.
async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await nextTick()
}

describe('useTerminalFont', () => {
  it('hydrates from settings.yaml and exposes the preset in pixels', async () => {
    mocks.AppearanceSettings.mockResolvedValue({ terminalFontSize: 'large' })
    const { useTerminalFont } = await import('../useTerminalFont')

    const { size, px } = useTerminalFont()
    await settle()

    expect(size.value).toBe('large')
    expect(px.value).toBe(14)
  })

  it('keeps the chosen size when persisting fails', async () => {
    mocks.SetTerminalFontSize.mockRejectedValue(new Error('disk full'))
    const { setTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { size } = useTerminalFont()
    await settle()

    setTerminalFontSize('large')
    await settle()

    expect(size.value).toBe('large')
  })

  it('does not let a slow settings read clobber a size chosen meanwhile', async () => {
    let resolveRead: (value: { terminalFontSize: string }) => void = () => {}
    mocks.AppearanceSettings.mockReturnValue(new Promise((resolve) => { resolveRead = resolve }))
    const { setTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { size } = useTerminalFont()

    setTerminalFontSize('large')
    resolveRead({ terminalFontSize: 'small' })
    await settle()

    expect(size.value).toBe('large')
  })
})
