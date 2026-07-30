import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { resetSessionStatusesForTests, useSessionStatuses } from '../useSessionStatuses'

const mocks = vi.hoisted(() => ({ SessionStatuses: vi.fn() }))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({
  SessionStatuses: mocks.SessionStatuses,
}))

describe('useSessionStatuses', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetSessionStatusesForTests()
  })

  afterEach(() => {
    resetSessionStatusesForTests()
    vi.useRealTimers()
  })

  it('indexes a snapshot by session and adopts Hive’s poll interval', async () => {
    mocks.SessionStatuses.mockResolvedValue({
      items: [
        { sessionId: 's1', running: true, windows: [{ windowId: '@1', status: 'active', tool: 'pi' }] },
        { sessionId: 's2', running: false, windows: [] },
      ],
      pollIntervalMs: 1750,
    })
    const { statuses, pollIntervalMs, reload } = useSessionStatuses()

    await reload()

    expect(statuses.value).toEqual({
      s1: { sessionId: 's1', running: true, windows: [{ windowId: '@1', status: 'active', tool: 'pi' }] },
      s2: { sessionId: 's2', running: false, windows: [] },
    })
    expect(pollIntervalMs.value).toBe(1750)
  })

  it('keeps the last good snapshot when a poll fails', async () => {
    mocks.SessionStatuses.mockResolvedValueOnce({
      items: [{ sessionId: 's1', running: true, windows: [{ windowId: '@1', status: 'ready', tool: 'codex' }] }],
      pollIntervalMs: 1500,
    }).mockRejectedValueOnce(new Error('tmux unavailable'))
    const { statuses, reload } = useSessionStatuses()

    await reload()
    await reload()

    expect(statuses.value.s1.windows?.[0].status).toBe('ready')
  })

  it('does not let an older overlapping response replace a newer one', async () => {
    let resolveFirst!: (value: unknown) => void
    const first = new Promise((resolve) => { resolveFirst = resolve })
    mocks.SessionStatuses.mockReturnValueOnce(first).mockResolvedValueOnce({
      items: [{ sessionId: 's1', running: true, windows: [{ windowId: '@1', status: 'approval', tool: 'claude' }] }],
      pollIntervalMs: 1500,
    })
    const { statuses, reload } = useSessionStatuses()

    const older = reload()
    await reload()
    resolveFirst({
      items: [{ sessionId: 's1', running: true, windows: [{ windowId: '@1', status: 'active', tool: 'claude' }] }],
      pollIntervalMs: 1500,
    })
    await older

    expect(statuses.value.s1.windows?.[0].status).toBe('approval')
  })

  it('cancels an in-flight poll and ignores its result when polling stops', async () => {
    let resolvePending!: (value: { items: Array<{ sessionId: string; running: boolean; windows: Array<{ windowId: string; status: string; tool: string }> }>; pollIntervalMs: number }) => void
    const pending = Object.assign(new Promise<{ items: Array<{ sessionId: string; running: boolean; windows: Array<{ windowId: string; status: string; tool: string }> }>; pollIntervalMs: number }>((resolve) => { resolvePending = resolve }), { cancel: vi.fn() })
    mocks.SessionStatuses.mockReturnValue(pending)
    const { statuses, startPolling, stopPolling } = useSessionStatuses()
    statuses.value = { s1: { sessionId: 's1', running: true, windows: [{ windowId: '@1', status: 'ready', tool: 'pi' }] } }

    startPolling()
    expect(mocks.SessionStatuses).toHaveBeenCalledTimes(1)
    stopPolling()
    expect(pending.cancel).toHaveBeenCalledTimes(1)

    resolvePending({ items: [{ sessionId: 's1', running: true, windows: [{ windowId: '@1', status: 'approval', tool: 'pi' }] }], pollIntervalMs: 25 })
    await pending
    await Promise.resolve()

    expect(statuses.value.s1.windows?.[0].status).toBe('ready')
  })

  it('polls serially at the interval returned by Hive and stops cleanly', async () => {
    vi.useFakeTimers()
    mocks.SessionStatuses.mockResolvedValue({ items: [], pollIntervalMs: 25 })
    const { startPolling, stopPolling } = useSessionStatuses()

    startPolling()
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.SessionStatuses).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(24)
    expect(mocks.SessionStatuses).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.SessionStatuses).toHaveBeenCalledTimes(2)

    stopPolling()
    await vi.advanceTimersByTimeAsync(100)
    expect(mocks.SessionStatuses).toHaveBeenCalledTimes(2)
  })
})
