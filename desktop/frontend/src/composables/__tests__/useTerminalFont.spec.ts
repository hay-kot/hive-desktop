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

  it('steps one preset at a time and persists each move', async () => {
    const { stepTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { size, px } = useTerminalFont()
    await settle()

    stepTerminalFontSize(1)
    expect(size.value).toBe('large')
    expect(px.value).toBe(14)

    stepTerminalFontSize(-1)
    stepTerminalFontSize(-1)
    expect(size.value).toBe('small')

    await settle()
    expect(mocks.SetTerminalFontSize.mock.calls.map(([arg]) => arg)).toEqual(['large', 'medium', 'small'])
  })

  it('holds at the ends of the ladder instead of wrapping', async () => {
    mocks.AppearanceSettings.mockResolvedValue({ terminalFontSize: 'xxl' })
    const { stepTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { size } = useTerminalFont()
    await settle()

    stepTerminalFontSize(1)
    await settle()

    expect(size.value).toBe('xxl')
    // A no-op must not write: the durable record is unchanged.
    expect(mocks.SetTerminalFontSize).not.toHaveBeenCalled()
  })

  it('resets to the default preset and persists it', async () => {
    mocks.AppearanceSettings.mockResolvedValue({ terminalFontSize: 'xl' })
    const { resetTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { size, px } = useTerminalFont()
    await settle()

    resetTerminalFontSize()
    await settle()

    expect(size.value).toBe('medium')
    expect(px.value).toBe(13)
    expect(mocks.SetTerminalFontSize).toHaveBeenCalledWith('medium')
  })

  it('writes nothing when reset is asked for at the default', async () => {
    const { resetTerminalFontSize } = await import('../useTerminalFont')
    await settle()

    resetTerminalFontSize()
    await settle()

    expect(mocks.SetTerminalFontSize).not.toHaveBeenCalled()
  })

  it('reports where the size sits on the ladder', async () => {
    const { terminalFontSizeState } = await import('../useTerminalFont')

    expect(terminalFontSizeState('small')).toEqual({ canDecrease: false, canIncrease: true, isDefault: false })
    expect(terminalFontSizeState('medium')).toEqual({ canDecrease: true, canIncrease: true, isDefault: true })
    expect(terminalFontSizeState('xxl')).toEqual({ canDecrease: true, canIncrease: false, isDefault: false })
  })

  it('keeps the chosen size when persisting fails', async () => {
    mocks.SetTerminalFontSize.mockRejectedValue(new Error('disk full'))
    const { stepTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { size } = useTerminalFont()
    await settle()

    stepTerminalFontSize(1)
    await settle()

    expect(size.value).toBe('large')
  })

  it('does not let a slow settings read clobber a size chosen meanwhile', async () => {
    let resolveRead: (value: { terminalFontSize: string }) => void = () => {}
    mocks.AppearanceSettings.mockReturnValue(new Promise((resolve) => { resolveRead = resolve }))
    const { stepTerminalFontSize, useTerminalFont } = await import('../useTerminalFont')
    const { size } = useTerminalFont()

    stepTerminalFontSize(1)
    resolveRead({ terminalFontSize: 'small' })
    await settle()

    expect(size.value).toBe('large')
  })
})
