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
  it('hydrates a persisted pixel size', async () => {
    mocks.AppearanceSettings.mockResolvedValue({ terminalFontSize: '17' })
    const { useTerminalFont } = await import('../useTerminalFont')

    const { px } = useTerminalFont()
    await settle()

    expect(px.value).toBe(17)
  })

  // The names this setting held before it was a number. A settings.yaml written
  // then is the common case for anyone upgrading, not an edge one.
  it('reads a legacy preset name as the pixels it meant', async () => {
    mocks.AppearanceSettings.mockResolvedValue({ terminalFontSize: 'large' })
    const { useTerminalFont } = await import('../useTerminalFont')

    const { px } = useTerminalFont()
    await settle()

    expect(px.value).toBe(14)
  })

  it('steps in twos and holds at the ladder ends', async () => {
    const { maxTerminalFontSizePx, stepTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { px } = useTerminalFont()
    await settle()
    const start = px.value

    stepTerminalFontSize(1)
    expect(px.value).toBe(start + 2)

    stepTerminalFontSize(-1)
    expect(px.value).toBe(start)

    for (let i = 0; i < 100; i++) stepTerminalFontSize(1)
    expect(px.value).toBe(maxTerminalFontSizePx)
  })

  it('persists the size as digits', async () => {
    const { setTerminalFontSize } = await import('../useTerminalFont')
    await settle()

    setTerminalFontSize(20)
    await settle()

    expect(mocks.SetTerminalFontSize).toHaveBeenCalledWith('20')
  })

  it('resets to the default size', async () => {
    const { defaultTerminalFontSizePx, resetTerminalFontSize, setTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { px } = useTerminalFont()
    await settle()

    setTerminalFontSize(24)
    resetTerminalFontSize()

    expect(px.value).toBe(defaultTerminalFontSizePx)
  })

  it('keeps the chosen size when persisting fails', async () => {
    mocks.SetTerminalFontSize.mockRejectedValue(new Error('disk full'))
    const { setTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { px } = useTerminalFont()
    await settle()

    setTerminalFontSize(14)
    await settle()

    expect(px.value).toBe(14)
  })

  it('does not let a slow settings read clobber a size chosen meanwhile', async () => {
    let resolveRead: (value: { terminalFontSize: string }) => void = () => {}
    mocks.AppearanceSettings.mockReturnValue(new Promise((resolve) => { resolveRead = resolve }))
    const { setTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { px } = useTerminalFont()

    setTerminalFontSize(14)
    resolveRead({ terminalFontSize: '12' })
    await settle()

    expect(px.value).toBe(14)
  })
})
