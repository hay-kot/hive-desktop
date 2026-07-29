import { describe, expect, it, vi, beforeEach } from 'vitest'
import { groupTerminalSessions, useTerminalSessions, type TerminalSessionRow } from '../useTerminalSessions'

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

describe('groupTerminalSessions', () => {
  function row(name: string, repo: string): TerminalSessionRow {
    return { id: name, name, slug: `slug-${name}`, repo, state: 'active' }
  }

  it('groups by remote with readable names, both levels alphabetical', () => {
    const groups = groupTerminalSessions([
      row('zeta', 'https://github.com/hay-kot/hive-desktop.git'),
      row('alpha', 'https://github.com/hay-kot/hive-desktop.git'),
      row('solo', 'git@github.com:colonyops/hive.git'),
    ])

    expect(groups.map((group) => group.name)).toEqual(['colonyops/hive', 'hay-kot/hive-desktop'])
    expect(groups[1].sessions.map((s) => s.name)).toEqual(['alpha', 'zeta'])
  })

  it('collects sessions without a remote under one label', () => {
    const groups = groupTerminalSessions([row('scratch', ''), row('local', '/Users/x/repos/scratchpad')])

    expect(groups.map((group) => group.name)).toEqual(['(no remote)', 'repos/scratchpad'])
  })
})
