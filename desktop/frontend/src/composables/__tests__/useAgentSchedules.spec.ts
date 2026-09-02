import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { resetAgentWorkspacesForTests } from '../useAgentWorkspaces'
import { resetAgentSchedulesForTests, useAgentSchedules } from '../useAgentSchedules'
import type { AgentSchedule, AgentScheduleRun } from '../../lib/agentWorkspacesClient'

const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  On: vi.fn(),
  schedules: vi.fn(),
  scheduleRuns: vi.fn(),
  saveSchedule: vi.fn(),
  deleteSchedule: vi.fn(),
  runSchedule: vi.fn(),
  previewSchedule: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice', () => ({
  Available: mocks.Available,
  Endpoint: vi.fn().mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' }),
}))
vi.mock('@wailsio/runtime', () => ({ Events: { On: mocks.On } }))
vi.mock('../../lib/agentWorkspacesClient', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/agentWorkspacesClient')>()),
  createAgentWorkspacesClient: () => ({
    schedules: mocks.schedules,
    scheduleRuns: mocks.scheduleRuns,
    saveSchedule: mocks.saveSchedule,
    deleteSchedule: mocks.deleteSchedule,
    runSchedule: mocks.runSchedule,
    previewSchedule: mocks.previewSchedule,
  }),
}))

function schedule(overrides: Partial<AgentSchedule> = {}): AgentSchedule {
  return {
    workspace: 'web-app',
    id: 'weekly-summary',
    name: 'Weekly summary',
    cron: '0 9 * * 5',
    prompt: 'Summarize the week.',
    disabled: false,
    onMissed: 'run',
    nextRunAt: 1_700_000_000_000,
    lastRun: null,
    ...overrides,
  }
}

function run(overrides: Partial<AgentScheduleRun> = {}): AgentScheduleRun {
  return {
    id: 1,
    workspace: 'web-app',
    scheduleId: 'weekly-summary',
    scheduleName: 'Weekly summary',
    scheduledFor: 1,
    startedAt: 2,
    reason: 'due',
    status: 'launched',
    missed: 0,
    sessionId: 7,
    prompt: 'Summarize the week.',
    error: '',
    ...overrides,
  }
}

describe('useAgentSchedules', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetAgentWorkspacesForTests()
    resetAgentSchedulesForTests()
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.On.mockReturnValue(() => {})
    mocks.schedules.mockResolvedValue([schedule()])
    mocks.scheduleRuns.mockResolvedValue([run()])
    mocks.saveSchedule.mockResolvedValue(schedule())
    mocks.deleteSchedule.mockResolvedValue(undefined)
    mocks.runSchedule.mockResolvedValue(run({ id: 2, reason: 'manual' }))
    mocks.previewSchedule.mockResolvedValue({ next: [1, 2, 3], prompt: 'rendered', cronError: '', promptError: '' })
  })

  it('loads a workspace\'s schedules and its run history together', async () => {
    const store = useAgentSchedules()

    await store.load('web-app')

    expect(mocks.schedules).toHaveBeenCalledWith('web-app')
    expect(mocks.scheduleRuns).toHaveBeenCalledWith('web-app', '', expect.any(Number))
    expect(store.schedules.value.map((row) => row.id)).toEqual(['weekly-summary'])
    expect(store.runs.value.map((row) => row.id)).toEqual([1])
    expect(store.loaded.value).toBe(true)
    expect(store.error.value).toBeNull()
  })

  // A failed revalidation reports itself without collapsing the list it could
  // not refresh. That is useAgentWorkspaces' rule, and the reason a transient
  // control-plane blip does not blank the pane.
  it('keeps the last-good rows when a reload fails', async () => {
    const store = useAgentSchedules()
    await store.load('web-app')

    mocks.schedules.mockRejectedValueOnce(new Error('the workspace is gone'))
    await store.load('web-app')

    expect(store.error.value).toBe('the workspace is gone')
    expect(store.schedules.value.map((row) => row.id)).toEqual(['weekly-summary'])
    expect(store.runs.value.map((row) => row.id)).toEqual([1])
  })

  // Keeping the last-good rows is for a reload. Across a switch they describe
  // a different manifest, and the pane's actions would name the wrong
  // workspace.
  it('drops the previous workspace\'s rows on a switch, even when the new read fails', async () => {
    const store = useAgentSchedules()
    await store.load('web-app')

    mocks.schedules.mockRejectedValueOnce(new Error('no such workspace'))
    await store.load('api')

    expect(store.schedules.value).toEqual([])
    expect(store.error.value).toBe('no such workspace')
  })

  it('clears the rows when the focus leaves every workspace', async () => {
    const store = useAgentSchedules()
    await store.load('web-app')

    await store.load('')

    expect(store.schedules.value).toEqual([])
    expect(store.runs.value).toEqual([])
  })

  it('reloads after a save, a delete and a manual run', async () => {
    const store = useAgentSchedules()
    await store.load('web-app')
    const readsBefore = mocks.schedules.mock.calls.length

    await store.save({ workspace: 'web-app', id: 'weekly-summary', name: 'Weekly summary', cron: '@daily', prompt: 'go', disabled: false, onMissed: 'run' })
    expect(mocks.saveSchedule).toHaveBeenCalledWith(expect.objectContaining({ cron: '@daily' }))

    await store.remove('web-app', 'weekly-summary')
    expect(mocks.deleteSchedule).toHaveBeenCalledWith('web-app', 'weekly-summary')

    const fired = await store.runNow('web-app', 'weekly-summary')
    expect(mocks.runSchedule).toHaveBeenCalledWith('web-app', 'weekly-summary')
    expect(fired.reason).toBe('manual')

    expect(mocks.schedules.mock.calls.length).toBe(readsBefore + 3)
  })

  // The switch is a save with the flag flipped, so the manifest stays the one
  // place a schedule's state is written.
  it('turns a schedule off by saving the whole entry with disabled set', async () => {
    const store = useAgentSchedules()
    await store.load('web-app')

    await store.setDisabled(schedule(), true)

    expect(mocks.saveSchedule).toHaveBeenCalledWith({
      workspace: 'web-app',
      id: 'weekly-summary',
      name: 'Weekly summary',
      cron: '0 9 * * 5',
      prompt: 'Summarize the week.',
      onMissed: 'run',
      disabled: true,
    })
  })

  // The Wails boundary degrades the payload to a wake-up, so the whole focused
  // workspace is re-read rather than a delta applied.
  it('re-reads the focused workspace on schedules:updated', async () => {
    const store = useAgentSchedules()
    await store.load('web-app')
    expect(mocks.On).toHaveBeenCalledWith('schedules:updated', expect.any(Function))

    mocks.schedules.mockResolvedValueOnce([schedule({ id: 'nightly' })])
    mocks.On.mock.calls[0][1]()
    await flushPromises()

    expect(store.schedules.value.map((row) => row.id)).toEqual(['nightly'])
  })

  it('passes a preview straight through to the client', async () => {
    const store = useAgentSchedules()
    await store.load('web-app')

    const preview = await store.preview({ workspace: 'web-app', cron: '@daily', prompt: 'go' })

    expect(mocks.previewSchedule).toHaveBeenCalledWith({ workspace: 'web-app', cron: '@daily', prompt: 'go' })
    expect(preview.next).toEqual([1, 2, 3])
  })
})
