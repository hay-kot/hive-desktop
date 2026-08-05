import { ref, type Ref } from 'vue'
import { SessionStatuses } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'
import type { SessionStatus } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

const DEFAULT_POLL_INTERVAL_MS = 1500

const statuses = ref<Record<string, SessionStatus>>({})
const pollIntervalMs = ref(DEFAULT_POLL_INTERVAL_MS)
// Whether a poll has ever finished. An empty status map cannot answer that, and
// a view that narrows on liveness has to tell "nothing is running" from "we have
// not asked yet". Set whatever the poll returned: a probe that keeps failing
// must not leave such a view waiting forever.
const loaded = ref(false)
let requestSequence = 0
let pollGeneration = 0
let pollTimer: ReturnType<typeof setTimeout> | undefined
let activeRequest: ReturnType<typeof SessionStatuses> | undefined

function cancelActiveRequest(): void {
  const request = activeRequest
  activeRequest = undefined
  if (request && typeof request.cancel === 'function') void request.cancel()
}

async function reload(): Promise<void> {
  const sequence = ++requestSequence
  cancelActiveRequest()
  let request: ReturnType<typeof SessionStatuses> | undefined
  try {
    request = SessionStatuses()
    activeRequest = request
    const snapshot = await request
    if (sequence !== requestSequence) return
    statuses.value = Object.fromEntries((snapshot.items ?? []).map((status) => [status.sessionId, status]))
    if (snapshot.pollIntervalMs > 0) pollIntervalMs.value = snapshot.pollIntervalMs
  } catch {
    // A transient tmux probe failure must not erase the last status the user saw.
  } finally {
    if (request && activeRequest === request) activeRequest = undefined
    if (sequence === requestSequence) loaded.value = true
  }
}

async function poll(generation: number): Promise<void> {
  await reload()
  if (generation !== pollGeneration) return
  pollTimer = setTimeout(() => { void poll(generation) }, pollIntervalMs.value)
}

function startPolling(): void {
  clearTimeout(pollTimer)
  const generation = ++pollGeneration
  void poll(generation)
}

function stopPolling(): void {
  ++pollGeneration
  ++requestSequence
  clearTimeout(pollTimer)
  pollTimer = undefined
  cancelActiveRequest()
}

export function useSessionStatuses(): {
  statuses: Ref<Record<string, SessionStatus>>
  loaded: Ref<boolean>
  pollIntervalMs: Ref<number>
  reload: () => Promise<void>
  startPolling: () => void
  stopPolling: () => void
} {
  return { statuses, loaded, pollIntervalMs, reload, startPolling, stopPolling }
}

export function resetSessionStatusesForTests(): void {
  stopPolling()
  statuses.value = {}
  loaded.value = false
  pollIntervalMs.value = DEFAULT_POLL_INTERVAL_MS
}
