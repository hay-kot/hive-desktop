import { describe, expect, it, vi, beforeEach } from 'vitest'
import { groupTerminalSessions, resetTerminalSessionsForTests, sessionRepository, useTerminalSessions, type TerminalSessionRow } from '../useTerminalSessions'

const mocks = vi.hoisted(() => ({ ListSessions: vi.fn() }))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({
  ListSessions: mocks.ListSessions,
}))

describe('useTerminalSessions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetTerminalSessionsForTests()
  })

  it('loads every session the list carries, whatever its state', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
      { id: '2', name: 'old work', slug: 'old-work', repo: 'hay-kot/hive', state: 'recycled' },
    ])
    const { sessions, loading, reload } = useTerminalSessions()

    await reload()

    expect(loading.value).toBe(false)
    // Filtering to what can be attached belongs to the tree, not the listing:
    // a recycled session is what the prune entry counts.
    expect(sessions.value.map((row) => row.slug)).toEqual(['hive-fix-parser', 'old-work'])
  })

  it('reads a null listing as no sessions', async () => {
    mocks.ListSessions.mockResolvedValue(null)
    const { sessions, reload } = useTerminalSessions()

    await reload()

    expect(sessions.value).toEqual([])
  })

  it('surfaces a listing failure and keeps the last-good rows', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'hay-kot/hive', state: 'active' },
    ])
    const { sessions, error, reload } = useTerminalSessions()
    await reload()

    mocks.ListSessions.mockRejectedValue(new Error('hive.db is locked'))
    await reload()

    expect(error.value).toBe('hive.db is locked')
    // The reload is a revalidation of a tree that is already on screen; a
    // failure must not collapse it.
    expect(sessions.value.map((row) => row.slug)).toEqual(['hive-fix-parser'])
  })
})

describe('sessionRepository', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetTerminalSessionsForTests()
  })

  it('resolves the remote of the session at a slug', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'fix the parser', slug: 'hive-fix-parser', repo: 'https://github.com/hay-kot/hive.git', state: 'active' },
    ])
    await useTerminalSessions().reload()

    expect(sessionRepository('hive-fix-parser')).toBe('https://github.com/hay-kot/hive.git')
  })

  it('is empty for an unknown slug, no slug, and a session without a remote', async () => {
    mocks.ListSessions.mockResolvedValue([
      { id: '1', name: 'scratch', slug: 'scratch', repo: '', state: 'active' },
    ])
    await useTerminalSessions().reload()

    expect(sessionRepository('scratch')).toBe('')
    expect(sessionRepository('gone')).toBe('')
    expect(sessionRepository('')).toBe('')
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
