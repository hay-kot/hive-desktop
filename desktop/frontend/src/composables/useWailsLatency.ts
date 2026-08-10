import { ref } from 'vue'
import { Echo, Ping } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/devtoolsservice'

/** Calls thrown away before timing starts, so a cold first call is not the p50. */
const WARMUP = 5
const SAMPLES = 40

/** The payload leg, sized like a terminal frame rather than like a settings read. */
export const PAYLOAD_BYTES = 64 * 1024

export interface Latency {
  samples: number
  p50: number
  p95: number
  max: number
}

export interface LatencyReport {
  /** An empty call: what a Wails round trip costs before it carries anything. */
  empty: Latency
  /** The same call carrying PAYLOAD_BYTES back, so the two differ by marshalling. */
  payload: Latency
}

/** Nearest-rank percentile over an unsorted sample set. */
export function percentile(values: number[], p: number): number {
  if (values.length === 0) return 0
  const sorted = [...values].sort((a, b) => a - b)
  const rank = Math.ceil((p / 100) * sorted.length)
  return sorted[Math.min(sorted.length - 1, Math.max(0, rank - 1))]
}

function summarize(durations: number[]): Latency {
  return {
    samples: durations.length,
    p50: percentile(durations, 50),
    p95: percentile(durations, 95),
    max: durations.length ? Math.max(...durations) : 0,
  }
}

// Calls are issued one at a time on purpose: firing them together would measure
// how deep the queue got, not what a round trip costs.
async function time(call: () => Promise<unknown>): Promise<number[]> {
  for (let i = 0; i < WARMUP; i++) await call()

  const durations: number[] = []
  for (let i = 0; i < SAMPLES; i++) {
    const started = performance.now()
    await call()
    durations.push(performance.now() - started)
  }
  return durations
}

// useWailsLatency prices the frontend↔Go boundary from the side that pays for
// it. Both legs go through the same bound-method plumbing every service call
// uses, so the numbers are the app's own, not a synthetic benchmark's.
export function useWailsLatency() {
  const report = ref<LatencyReport | null>(null)
  const measuring = ref(false)
  const error = ref('')

  async function measure(): Promise<void> {
    if (measuring.value) return
    measuring.value = true
    error.value = ''
    try {
      const empty = await time(() => Ping())
      const payload = await time(() => Echo(PAYLOAD_BYTES))
      report.value = { empty: summarize(empty), payload: summarize(payload) }
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
    } finally {
      measuring.value = false
    }
  }

  return { report, measuring, error, measure }
}
