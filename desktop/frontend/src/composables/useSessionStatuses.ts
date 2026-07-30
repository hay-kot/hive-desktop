import { ref, type Ref } from 'vue'
import { SessionStatuses } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'
import type { SessionStatus } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

const DEFAULT_POLL_INTERVAL_MS = 1500

const statuses = ref<Record<string, SessionStatus>>({})
const pollIntervalMs = ref(DEFAULT_POLL_INTERVAL_MS)
let requestSequence = 0
let pollGeneration = 0
let pollTimer: ReturnType<typeof setTimeout> | undefined

async function reload(): Promise<void> {
  const sequence = ++requestSequence
  try {
    const snapshot = await SessionStatuses()
    if (sequence !== requestSequence) return
    statuses.value = Object.fromEntries((snapshot.items ?? []).map((status) => [status.sessionId, status]))
    if (snapshot.pollIntervalMs > 0) pollIntervalMs.value = snapshot.pollIntervalMs
  } catch {
    // A transient tmux probe failure must not erase the last status the user saw.
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
  clearTimeout(pollTimer)
  pollTimer = undefined
}

export function useSessionStatuses(): {
  statuses: Ref<Record<string, SessionStatus>>
  pollIntervalMs: Ref<number>
  reload: () => Promise<void>
  startPolling: () => void
  stopPolling: () => void
} {
  return { statuses, pollIntervalMs, reload, startPolling, stopPolling }
}

export function resetSessionStatusesForTests(): void {
  stopPolling()
  ++requestSequence
  statuses.value = {}
  pollIntervalMs.value = DEFAULT_POLL_INTERVAL_MS
}
