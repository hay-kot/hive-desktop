import { onScopeDispose, ref, shallowRef } from 'vue'
import { Stats } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/devtoolsservice'
import type { RuntimeStats } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'

/** How many samples the sparklines keep — two minutes at the default cadence. */
const HISTORY = 60

const DEFAULT_INTERVAL_MS = 2000

// useRuntimeStats polls the process sample the developer-tools pane draws.
//
// The backend reports CPU as a rate against the previous call, so the cadence
// here is what that rate is measured over: stopping and restarting the poll
// makes the next sample cover the whole gap, which is correct but reads as a
// spike. That is why pausing keeps the history rather than clearing it — the
// break is visible in the line.
export function useRuntimeStats(intervalMs = DEFAULT_INTERVAL_MS) {
  const stats = shallowRef<RuntimeStats | null>(null)
  const rssHistory = ref<number[]>([])
  const cpuHistory = ref<number[]>([])
  const polling = ref(false)
  const error = ref('')
  let timer: ReturnType<typeof setInterval> | undefined

  async function refresh(): Promise<void> {
    try {
      const sample = await Stats()
      stats.value = sample
      rssHistory.value = [...rssHistory.value, sample.totalRssBytes].slice(-HISTORY)
      cpuHistory.value = [...cpuHistory.value, sample.totalCpuPercent].slice(-HISTORY)
      error.value = ''
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
    }
  }

  function start(): void {
    if (polling.value) return
    polling.value = true
    void refresh()
    timer = setInterval(() => void refresh(), intervalMs)
  }

  function stop(): void {
    polling.value = false
    if (timer !== undefined) clearInterval(timer)
    timer = undefined
  }

  onScopeDispose(stop)

  return { stats, rssHistory, cpuHistory, polling, error, refresh, start, stop }
}
