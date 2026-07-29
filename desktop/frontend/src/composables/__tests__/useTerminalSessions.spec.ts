import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useTerminalSessions } from '../useTerminalSessions'

const mocks = vi.hoisted(() => ({ ListSessions: vi.fn() }))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({
  ListSessions: mocks.ListSessions,
}))

describe('useTerminalSessions', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads the active sessions the picker offers', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
    ])
    const { sessions, loading, reload } = useTerminalSessions()

    await reload()

    expect(loading.value).toBe(false)
    expect(sessions.value.map((row) => row.slug)).toEqual(['hive-fix-parser'])
  })

  it('reads a null listing as no sessions', async () => {
    mocks.ListSessions.mockResolvedValue(null)
    const { sessions, reload } = useTerminalSessions()

    await reload()

    expect(sessions.value).toEqual([])
  })

  it('surfaces a listing failure and clears the rows', async () => {
    mocks.ListSessions.mockRejectedValue(new Error('hive.db is locked'))
    const { sessions, error, reload } = useTerminalSessions()

    await reload()

    expect(error.value).toBe('hive.db is locked')
    expect(sessions.value).toEqual([])
  })
})
