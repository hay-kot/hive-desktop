import { beforeEach, describe, expect, it, vi } from 'vitest'
import { riskDescription, useSessionActions } from '../useSessionActions'
import { resetToastsForTests, useToasts } from '../useToasts'
import type { SessionSummary } from '../../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

const mocks = vi.hoisted(() => ({
  DeleteSession: vi.fn(),
  PruneSessions: vi.fn(),
  RecycleSession: vi.fn(),
  RenameSession: vi.fn(),
  SessionDetail: vi.fn(),
  SessionRisk: vi.fn(),
  SetSessionGroup: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => mocks)

const session: SessionSummary = { id: 's1', name: 'review 81', slug: 'review-81', repo: 'acme/site', state: 'active', group: '' }

const noRisk = { uncommittedChanges: false, unpushedCommits: false, recycleDeletes: false }

beforeEach(() => {
  vi.clearAllMocks()
  resetToastsForTests()
  mocks.SessionRisk.mockResolvedValue(noRisk)
})

describe('riskDescription', () => {
  it('names uncommitted changes and unpushed commits together', () => {
    const text = riskDescription('review 81', 'delete', { uncommittedChanges: true, unpushedCommits: true, recycleDeletes: false })
    expect(text).toContain('Deleting review 81')
    expect(text).toContain('uncommitted changes and unpushed commits')
  })

  it('names each risk on its own', () => {
    expect(riskDescription('s', 'delete', { uncommittedChanges: true, unpushedCommits: false, recycleDeletes: false }))
      .toContain('It has uncommitted changes')
    expect(riskDescription('s', 'delete', { uncommittedChanges: false, unpushedCommits: true, recycleDeletes: false }))
      .toContain('It has unpushed commits')
  })

  it('says so when git reports nothing at risk', () => {
    expect(riskDescription('s', 'delete', noRisk)).toContain('no uncommitted changes and no unpushed commits')
  })

  it('warns that recycling a worktree session deletes it', () => {
    const text = riskDescription('s', 'recycle', { ...noRisk, recycleDeletes: true })
    expect(text).toContain('deletes it')
    expect(text).toContain('git worktree')
  })

  it('describes a full-clone recycle as a reset', () => {
    expect(riskDescription('s', 'recycle', noRisk)).toContain('resets its clone')
  })
})

describe('useSessionActions destructive operations', () => {
  it('confirms against the specific risk before deleting', async () => {
    mocks.SessionRisk.mockResolvedValue({ uncommittedChanges: true, unpushedCommits: false, recycleDeletes: false })
    const actions = useSessionActions()

    await actions.requestDelete(session)

    expect(mocks.SessionRisk).toHaveBeenCalledWith('s1')
    expect(actions.confirmation.open.value).toBe(true)
    expect(actions.confirmation.options.value?.description).toContain('uncommitted changes')
    expect(mocks.DeleteSession).not.toHaveBeenCalled()

    await actions.confirmation.confirm()
    expect(mocks.DeleteSession).toHaveBeenCalledWith('s1')
    expect(actions.confirmation.open.value).toBe(false)
  })

  it('does not confirm at all when the pre-flight fails', async () => {
    mocks.SessionRisk.mockRejectedValue(new Error('no such session'))
    const actions = useSessionActions()

    await actions.requestDelete(session)

    expect(actions.confirmation.open.value).toBe(false)
    expect(mocks.DeleteSession).not.toHaveBeenCalled()
    expect(useToasts().toasts.value[0]?.message).toBe('no such session')
  })

  it('recycles through the same gate', async () => {
    const actions = useSessionActions()

    await actions.requestRecycle(session)
    await actions.confirmation.confirm()

    expect(mocks.RecycleSession).toHaveBeenCalledWith('s1')
  })

  it('names the count in the prune confirmation', async () => {
    const actions = useSessionActions()

    actions.requestPrune(3)
    expect(actions.confirmation.options.value?.description).toContain('All 3 recycled and corrupted sessions')

    await actions.confirmation.confirm()
    expect(mocks.PruneSessions).toHaveBeenCalled()
  })

  it('leaves the confirmation open with its error when the job cannot start', async () => {
    mocks.DeleteSession.mockRejectedValue(new Error('sessions are unavailable'))
    const actions = useSessionActions()

    await actions.requestDelete(session)
    await actions.confirmation.confirm()

    expect(actions.confirmation.open.value).toBe(true)
    expect(actions.confirmation.error.value).toBe('sessions are unavailable')
  })
})

describe('useSessionActions edits', () => {
  it('renames the session and asks the host to re-read the list', async () => {
    mocks.RenameSession.mockResolvedValue({ ...session, name: 'review 82', slug: 'review-82' })
    const onChanged = vi.fn()
    const actions = useSessionActions({ onChanged })

    actions.requestRename(session)
    expect(actions.edit.value?.value).toBe('review 81')

    await actions.submitEdit('  review 82  ')

    expect(mocks.RenameSession).toHaveBeenCalledWith('s1', 'review 82')
    // The re-read is how the new slug reaches the host: it is the tmux target.
    expect(onChanged).toHaveBeenCalled()
    expect(actions.edit.value).toBeNull()
  })

  it('keeps the rename dialog open with the reason a name was rejected', async () => {
    mocks.RenameSession.mockRejectedValue(new Error('a session named "review 82" already exists'))
    const actions = useSessionActions()

    actions.requestRename(session)
    await actions.submitEdit('review 82')

    expect(actions.edit.value).not.toBeNull()
    expect(actions.editError.value).toContain('already exists')
  })

  it('treats an empty group as a clear, and an empty name as nothing to submit', async () => {
    const actions = useSessionActions()

    actions.requestGroup({ ...session, group: 'backend' })
    expect(actions.edit.value?.allowEmpty).toBe(true)
    await actions.submitEdit('')
    expect(mocks.SetSessionGroup).toHaveBeenCalledWith('s1', '')

    actions.requestRename(session)
    expect(actions.edit.value?.allowEmpty).toBe(false)
  })

  it('reads a session on demand for the detail view', async () => {
    mocks.SessionDetail.mockResolvedValue({ ...session, path: '/tmp/review-81' })
    const actions = useSessionActions()

    await actions.openDetail(session)
    expect(actions.detail.value?.path).toBe('/tmp/review-81')

    actions.closeDetail()
    expect(actions.detail.value).toBeNull()
  })
})
