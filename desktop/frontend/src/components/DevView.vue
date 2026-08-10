<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import IconBell from '~icons/lucide/bell'
import IconGauge from '~icons/lucide/gauge'
import IconPause from '~icons/lucide/pause'
import IconPlay from '~icons/lucide/play'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import { Notify as NotifyNative } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/notificationservice'
import { useNotificationSettings } from '../composables/useNotificationSettings'
import { notifySeverityMapping, useNotify, type NotifySeverity } from '../composables/useNotify'
import { useRuntimeStats } from '../composables/useRuntimeStats'
import { PAYLOAD_BYTES, useWailsLatency } from '../composables/useWailsLatency'
import { useToasts } from '../composables/useToasts'
import { formatBytes } from '../lib/bytes'
import AppCheckbox from './AppCheckbox.vue'
import AppSelect from './AppSelect.vue'
import BaseButton from './BaseButton.vue'
import BaseCard from './BaseCard.vue'
import SettingsField from './settings/SettingsField.vue'
import SettingsPage from './settings/SettingsPage.vue'
import SettingsSection from './settings/SettingsSection.vue'
import SparkLine from './SparkLine.vue'
import { useEscapeToClose } from '../composables/useEscapeToClose'
import ViewHeader from './settings/ViewHeader.vue'

const emit = defineEmits<{ close: [] }>()

type NotificationTestChannel = 'auto' | 'force-toast' | 'force-system'

type ResultTone = 'ok' | 'warn' | 'error' | 'muted'
interface TestResult {
  tone: ResultTone
  text: string
}

const severityOptions: Array<{ value: NotifySeverity; label: string }> = [
  { value: 'info', label: 'Info' },
  { value: 'success', label: 'Success' },
  { value: 'warning', label: 'Warning' },
  { value: 'error', label: 'Error' },
]

const channelOptions: Array<{ value: NotificationTestChannel; label: string; hint: string }> = [
  {
    value: 'auto',
    label: 'Follow app settings',
    hint: 'Records in Activity, then follows your delivery preference, window focus and OS permission.',
  },
  {
    value: 'force-toast',
    label: 'Force an in-app toast',
    hint: 'Shows a toast right now, bypassing settings, focus and Activity.',
  },
  {
    value: 'force-system',
    label: 'Force a system banner',
    hint: 'Asks the OS for a banner right now, bypassing settings, focus and Activity.',
  },
]

const resultToneClass: Record<ResultTone, string> = {
  ok: 'text-severity-success',
  warn: 'text-severity-warning',
  error: 'text-severity-error',
  muted: 'text-text-3',
}

const delaySeconds = 3

const severity = ref<NotifySeverity>('info')
const channel = ref<NotificationTestChannel>('auto')
const delay = ref(false)
const remaining = ref(0)
const result = ref<TestResult | null>(null)
const pending = computed(() => remaining.value > 0)

let ticker: ReturnType<typeof setInterval> | undefined
let sequence = 0

const { showToast } = useToasts()
const { notificationSound } = useNotificationSettings()

const testTitle = 'Test notification'
const testSubtitle = 'Hive Dev tools'

function testBody(selectedChannel: NotificationTestChannel): string {
  switch (selectedChannel) {
    case 'auto':
      return 'Dev tools auto test: uses focus and notification settings.'
    case 'force-toast':
      return 'Dev tools forced toast test: bypasses focus and Activity.'
    case 'force-system':
      return 'Dev tools forced system test: bypasses focus and Activity.'
  }
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

// useNotify picks between a toast, an OS banner and Activity-only from settings
// and focus, and returns none of that. Wrapping the two seams it already takes
// as injectable dependencies reports where the notification actually went
// without re-deriving that decision here, where it would drift.
async function deliverAuto(selectedSeverity: NotifySeverity, body: string): Promise<TestResult> {
  const surfaced: Array<'toast' | 'banner'> = []
  let bannerError: unknown

  const { notify } = useNotify({
    showToast: (message, options) => {
      surfaced.push('toast')
      return showToast(message, options)
    },
    osNotify: async (input) => {
      surfaced.push('banner')
      try {
        await NotifyNative(input)
      } catch (error) {
        bannerError = error
        throw error
      }
    },
  })

  try {
    await notify({ title: testTitle, body, severity: selectedSeverity, category: 'system', source: 'dev-view' })
  } catch (error) {
    return { tone: 'error', text: `Notification failed: ${errorText(error)}` }
  }

  switch (surfaced.at(-1)) {
    case 'banner':
      return { tone: 'ok', text: 'Recorded in Activity and shown as a system banner.' }
    case 'toast':
      return bannerError === undefined
        ? { tone: 'ok', text: 'Recorded in Activity and shown as an in-app toast.' }
        : { tone: 'warn', text: `Recorded in Activity. The system banner failed (${errorText(bannerError)}), so it fell back to an in-app toast.` }
    default:
      return { tone: 'muted', text: 'Recorded in Activity only — notifications are switched off, so nothing surfaced.' }
  }
}

async function deliver(selectedChannel: NotificationTestChannel, selectedSeverity: NotifySeverity): Promise<TestResult> {
  const body = testBody(selectedChannel)

  if (selectedChannel === 'auto') return deliverAuto(selectedSeverity, body)

  if (selectedChannel === 'force-toast') {
    const id = showToast(testTitle, { body, severity: notifySeverityMapping[selectedSeverity].toast })
    return { tone: 'ok', text: `Toast #${id} shown in-app. Nothing was recorded in Activity.` }
  }

  try {
    await NotifyNative({
      title: testTitle,
      subtitle: testSubtitle,
      body,
      severity: selectedSeverity,
      sound: notificationSound.value,
      data: { source: 'dev-view', channel: 'force-system', severity: selectedSeverity },
    })
    return { tone: 'ok', text: 'The OS accepted a system banner. Nothing was recorded in Activity.' }
  } catch (error) {
    return { tone: 'error', text: `System banner failed: ${errorText(error)}` }
  }
}

async function dispatchTest(selectedChannel: NotificationTestChannel, selectedSeverity: NotifySeverity): Promise<void> {
  const token = ++sequence
  result.value = { tone: 'muted', text: 'Sending…' }
  const outcome = await deliver(selectedChannel, selectedSeverity)
  // A newer send owns the result line by the time a slower one resolves.
  if (token !== sequence) return
  result.value = outcome
}

function stopCountdown(): void {
  if (ticker !== undefined) clearInterval(ticker)
  ticker = undefined
  remaining.value = 0
}

function cancelPending(): void {
  if (!pending.value) return
  stopCountdown()
  result.value = { tone: 'muted', text: 'Scheduled test cancelled.' }
}

function sendTest(): void {
  stopCountdown()

  const selectedChannel = channel.value
  const selectedSeverity = severity.value
  if (!delay.value) {
    void dispatchTest(selectedChannel, selectedSeverity)
    return
  }

  // One ticker drives both the countdown and the send, so the number on screen
  // is the time actually left rather than a second clock beside it.
  result.value = null
  remaining.value = delaySeconds
  ticker = setInterval(() => {
    remaining.value -= 1
    if (remaining.value > 0) return
    stopCountdown()
    void dispatchTest(selectedChannel, selectedSeverity)
  }, 1000)
}

onUnmounted(stopCountdown)

// ── Runtime ──────────────────────────────────────────────────────────────────
// What the install costs the machine, polled while this pane is open. Two
// halves that must not be confused: RSS is what the OS charges for, heap-in-use
// is what this program asked for, and on a cgo-heavy shell the gap between them
// is the native side the Go runtime cannot see.
const { stats, rssHistory, cpuHistory, polling, error: statsError, refresh: refreshStats, start, stop } = useRuntimeStats()
const { report: latency, measuring, error: latencyError, measure } = useWailsLatency()

const processRows = computed(() => {
  const sample = stats.value
  if (!sample) return []
  return [{ ...sample.process, self: true }, ...(sample.children ?? []).map((child) => ({ ...child, self: false }))]
})

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? '' : 's'}`
}

const uptime = computed(() => {
  const ms = stats.value?.uptimeMs ?? 0
  const minutes = Math.floor(ms / 60_000)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return hours < 24 ? `${hours}h ${minutes % 60}m` : `${Math.floor(hours / 24)}d ${hours % 24}h`
})

// The peak beats repeating the current value: with no child processes the
// totals and this process are the same number, and a card that prints it twice
// says nothing.
const peakRSS = computed(() => (rssHistory.value.length ? Math.max(...rssHistory.value) : 0))
const peakCPU = computed(() => (cpuHistory.value.length ? Math.max(...cpuHistory.value) : 0))

// Where the heap sits against the ceiling that triggers the next collection —
// the number that says whether a GC is imminent, which a bare heap size does not.
const heapPressure = computed(() => {
  const go = stats.value?.go
  if (!go?.nextGcBytes) return 0
  return Math.min(100, Math.round((go.heapAllocBytes / go.nextGcBytes) * 100))
})

const vitals = computed(() => {
  const sample = stats.value
  if (!sample) return []
  return [
    { label: 'Goroutines', value: `${sample.go.goroutines}` },
    { label: 'OS threads', value: `${sample.process.threads}` },
    { label: 'Processes', value: `${processRows.value.length}` },
    { label: 'Cores', value: `${sample.go.gomaxprocs} of ${sample.go.numCpu}` },
    { label: 'Uptime', value: uptime.value },
  ]
})

const goRows = computed(() => {
  const go = stats.value?.go
  if (!go) return []
  return [
    { label: 'Heap reserved', value: formatBytes(go.heapSysBytes), hint: `${go.heapObjects.toLocaleString()} objects` },
    { label: 'Stacks', value: formatBytes(go.stackSysBytes), hint: plural(go.goroutines, 'goroutine') },
    { label: 'Mapped by Go', value: formatBytes(go.totalSysBytes), hint: 'heap, stacks, runtime' },
    { label: 'Collections', value: `${go.gcCount}`, hint: `${go.lastPauseMs.toFixed(2)}ms last · ${go.totalPauseMs.toFixed(0)}ms total` },
  ]
})

function togglePolling(): void {
  polling.value ? stop() : start()
}

onMounted(start)

// The header no longer carries a close button, so Escape is the way out.
useEscapeToClose(() => emit('close'))
</script>

<template>
  <div class="flex h-full min-h-0 flex-1 flex-col" data-testid="dev-view">
    <ViewHeader>
      <template #title>
        <span class="text-[13px] font-semibold text-text">Developer tools</span>
        <span class="font-mono text-[11px] text-text-4">internal</span>
      </template>
    </ViewHeader>

    <div class="hive-scroll @container/pane min-h-0 flex-1 overflow-y-auto px-6 py-6">
      <SettingsPage>
        <SettingsSection
          title="Runtime"
          description="What this install is costing the machine right now, sampled every two seconds."
          testid="dev-runtime"
        >
          <template #actions>
            <div class="flex items-center gap-3">
              <button
                type="button"
                class="flex cursor-pointer items-center gap-1.5 text-[12px] font-medium text-text-3 hover:text-text"
                data-testid="dev-runtime-poll"
                @click="togglePolling"
              >
                <component :is="polling ? IconPause : IconPlay" class="size-3.5" />{{ polling ? 'Pause' : 'Resume' }}
              </button>
              <button
                type="button"
                class="flex cursor-pointer items-center gap-1.5 text-[12px] font-medium text-text-3 hover:text-text"
                data-testid="dev-runtime-refresh"
                @click="refreshStats"
              ><IconRefreshCw class="size-3.5" />Sample now</button>
            </div>
          </template>

          <p v-if="statsError" class="text-xs text-severity-error" data-testid="dev-runtime-error">{{ statsError }}</p>

          <div v-if="stats" class="flex flex-col gap-3">
            <!-- The chart sits under the numbers, never behind them: a value
                 drawn over its own gradient is the one thing on this pane you
                 actually have to read. -->
            <div class="grid grid-cols-1 gap-3 @[440px]/pane:grid-cols-2">
              <div
                class="flex flex-col overflow-hidden rounded-[11px] border border-card bg-raised"
                data-testid="dev-runtime-memory"
              >
                <div class="px-4 pb-2.5 pt-3.5">
                  <span class="text-[10.5px] font-semibold uppercase tracking-[.1em] text-text-4">Memory</span>
                  <div class="mt-1 font-mono text-[22px] leading-none tabular-nums text-text">{{ formatBytes(stats.totalRssBytes) }}</div>
                  <div class="mt-1.5 text-[11.5px] tabular-nums text-text-3">resident · peak {{ formatBytes(peakRSS) }}</div>
                </div>
                <SparkLine :values="rssHistory" class="h-12 text-accent" />
              </div>
              <div
                class="flex flex-col overflow-hidden rounded-[11px] border border-card bg-raised"
                data-testid="dev-runtime-cpu"
              >
                <div class="px-4 pb-2.5 pt-3.5">
                  <span class="text-[10.5px] font-semibold uppercase tracking-[.1em] text-text-4">CPU</span>
                  <div class="mt-1 font-mono text-[22px] leading-none tabular-nums text-text">{{ stats.totalCpuPercent.toFixed(1) }}%</div>
                  <div class="mt-1.5 text-[11.5px] tabular-nums text-text-3">of one core · peak {{ peakCPU.toFixed(1) }}%</div>
                </div>
                <SparkLine :values="cpuHistory" class="h-12 text-severity-info" />
              </div>
            </div>

            <div
              class="grid grid-cols-2 gap-x-6 gap-y-3 rounded-[11px] border border-card bg-raised px-4 py-3 @[440px]/pane:grid-cols-3 @[720px]/pane:grid-cols-5"
              data-testid="dev-runtime-vitals"
            >
              <div v-for="vital in vitals" :key="vital.label" class="min-w-0">
                <div class="text-[10.5px] font-semibold uppercase tracking-[.1em] text-text-4">{{ vital.label }}</div>
                <div class="mt-1 truncate font-mono text-[15px] tabular-nums text-text">{{ vital.value }}</div>
              </div>
            </div>

            <div class="overflow-hidden rounded-[11px] border border-card bg-raised" data-testid="dev-runtime-go">
              <div class="border-b border-row px-4 py-3.5">
                <div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
                  <span class="text-[10.5px] font-semibold uppercase tracking-[.1em] text-text-4">Go heap</span>
                  <span class="text-[11.5px] tabular-nums text-text-3" data-testid="dev-runtime-heap-pressure">{{ heapPressure }}% of the next GC target</span>
                </div>
                <div class="mt-1 font-mono text-[15px] tabular-nums text-text">
                  {{ formatBytes(stats.go.heapAllocBytes) }}
                  <span class="text-[11.5px] text-text-4">of {{ formatBytes(stats.go.nextGcBytes) }}</span>
                </div>
                <div class="mt-2.5 h-1.5 overflow-hidden rounded-full bg-chip">
                  <div class="h-full rounded-full bg-accent transition-[width]" :style="{ width: `${heapPressure}%` }" />
                </div>
              </div>
              <div class="grid grid-cols-2 gap-x-6 gap-y-3 px-4 py-3.5 @[560px]/pane:grid-cols-4">
                <div v-for="row in goRows" :key="row.label" class="min-w-0">
                  <div class="text-[10.5px] font-semibold uppercase tracking-[.1em] text-text-4">{{ row.label }}</div>
                  <div class="mt-0.5 truncate font-mono text-[13px] tabular-nums text-text">{{ row.value }}</div>
                  <div class="truncate text-[11px] tabular-nums text-text-3">{{ row.hint }}</div>
                </div>
              </div>
            </div>

            <div class="overflow-hidden rounded-[11px] border border-card bg-raised" data-testid="dev-runtime-processes">
              <div class="flex items-center gap-3 border-b border-row px-4 py-2 text-[10.5px] font-semibold uppercase tracking-[.1em] text-text-4">
                <span class="min-w-0 flex-1">Process</span>
                <span class="w-20 text-right">Memory</span>
                <span class="w-14 text-right">CPU</span>
                <span class="hidden w-16 text-right @[560px]/pane:block">Threads</span>
              </div>
              <div
                v-for="row in processRows"
                :key="row.pid"
                class="flex items-center gap-3 border-b border-row px-4 py-2 text-[12.5px] text-text-2 last:border-b-0"
              >
                <span class="min-w-0 flex-1 truncate" :class="row.self ? 'font-semibold text-text' : ''">
                  {{ row.name || 'unknown' }}
                  <span class="font-mono text-[11px] text-text-4">{{ row.pid }}</span>
                </span>
                <span class="w-20 text-right font-mono tabular-nums">{{ formatBytes(row.rssBytes) }}</span>
                <span class="w-14 text-right font-mono tabular-nums">{{ row.cpuPercent.toFixed(1) }}%</span>
                <span class="hidden w-16 text-right font-mono tabular-nums @[560px]/pane:block">{{ row.threads }}</span>
              </div>
              <p v-if="stats.childrenTruncated" class="border-t border-row px-4 py-2 text-[11px] text-text-4">
                Only the first processes in the tree are listed; the totals above are a floor.
              </p>
              <p v-else-if="processRows.length === 1" class="border-t border-row px-4 py-2 text-[11px] text-text-4">
                Nothing else is parented to Hive right now. Only processes Hive is the parent of are counted — a terminal's
                shell or an agent, not the webview's rendering helpers, which the OS starts and owns.
              </p>
            </div>
          </div>
          <p v-else-if="!statsError" class="text-xs text-text-4">Sampling…</p>
        </SettingsSection>

        <SettingsSection
          title="Wails round-trip"
          description="What one call across the frontend↔Go boundary costs, empty and carrying a payload."
          testid="dev-latency"
        >
          <BaseCard class="items-start rounded-lg border border-border bg-raised">
            <template #icon>
              <span class="flex size-9 items-center justify-center rounded-lg bg-accent-tint text-accent"><IconGauge class="size-4" /></span>
            </template>
            <div class="min-w-0 flex-1">
              <div class="text-[13.5px] font-semibold text-text">Measure the boundary</div>
              <div class="mt-0.5 text-xs text-text-3">
                40 calls each, issued one at a time after a warm-up, through the same bound-method plumbing every service call uses.
              </div>

              <div v-if="latency" class="mt-3 grid grid-cols-1 gap-3 @[420px]/pane:grid-cols-2" data-testid="dev-latency-results">
                <div
                  v-for="leg in [{ key: 'empty', label: 'Empty call', value: latency.empty }, { key: 'payload', label: `${formatBytes(PAYLOAD_BYTES)} payload`, value: latency.payload }]"
                  :key="leg.key"
                  class="rounded-[9px] border border-row px-3 py-2.5"
                  :data-testid="`dev-latency-${leg.key}`"
                >
                  <div class="text-[10.5px] font-semibold uppercase tracking-[.1em] text-text-4">{{ leg.label }}</div>
                  <div class="mt-1 font-mono text-[13px] text-text">{{ leg.value.p50.toFixed(2) }}ms <span class="text-[11px] text-text-4">p50</span></div>
                  <div class="mt-0.5 font-mono text-[11.5px] text-text-3">
                    p95 {{ leg.value.p95.toFixed(2) }}ms · max {{ leg.value.max.toFixed(2) }}ms · n={{ leg.value.samples }}
                  </div>
                </div>
              </div>

              <p v-if="latencyError" class="mt-3 text-xs text-severity-error" data-testid="dev-latency-error">{{ latencyError }}</p>

              <div class="mt-3">
                <BaseButton size="sm" :busy="measuring" data-testid="dev-latency-measure" @click="measure">
                  {{ measuring ? 'Measuring…' : 'Measure round-trip' }}
                </BaseButton>
              </div>
            </div>
          </BaseCard>
        </SettingsSection>

        <SettingsSection
          title="Notifications"
          description="Exercise notification delivery while developing Hive."
        >
          <BaseCard class="mt-4 items-start rounded-lg border border-border bg-raised" data-testid="dev-notification-test">
            <template #icon>
              <span class="flex size-9 items-center justify-center rounded-lg bg-severity-info-tint text-severity-info"><IconBell class="size-4" /></span>
            </template>
            <div class="@container/notify-test min-w-0 flex-1" data-testid="dev-notification-form">
              <div class="text-[13.5px] font-semibold text-text">Test notification</div>
              <div class="mt-0.5 text-xs text-text-3">Send one through a delivery path and see where it lands.</div>

              <div class="mt-4 grid grid-cols-1 gap-4 @[420px]/notify-test:grid-cols-2" data-testid="dev-notification-fields">
                <SettingsField label="Severity" hint="Sets the toast accent and the banner's severity." testid="dev-notification-severity-field">
                  <AppSelect
                    size="sm"
                    class="w-full font-medium"
                    :model-value="severity"
                    :options="severityOptions"
                    testid="dev-notification-severity"
                    aria-label="Notification severity"
                    @update:model-value="severity = $event as NotifySeverity"
                  />
                </SettingsField>

                <SettingsField label="Timing">
                  <AppCheckbox
                    :model-value="delay"
                    label="Delay 3 seconds"
                    hint="Long enough to click away, so the send lands while Hive is unfocused."
                    testid="dev-notification-delay"
                    @update:model-value="delay = $event"
                  />
                </SettingsField>
              </div>

              <div class="mt-4">
                <div class="mb-1.5 text-[12.5px] text-text-2">Delivery channel</div>
                <div
                  class="flex flex-col gap-2"
                  role="radiogroup"
                  aria-label="Delivery channel"
                  data-testid="dev-notification-channel"
                >
                  <label v-for="option in channelOptions" :key="option.value" class="flex cursor-pointer items-start gap-2.5">
                    <input
                      type="radio"
                      class="mt-0.5 accent-accent"
                      name="dev-notification-channel"
                      :value="option.value"
                      :checked="channel === option.value"
                      :data-testid="`dev-notification-channel-${option.value}`"
                      @change="channel = option.value"
                    >
                    <span>
                      <span class="block text-[12.5px] text-text">{{ option.label }}</span>
                      <span class="block text-[12px] text-text-3">{{ option.hint }}</span>
                    </span>
                  </label>
                </div>
              </div>

              <div class="mt-4 flex flex-col items-stretch gap-2 @[420px]/notify-test:flex-row @[420px]/notify-test:items-center" data-testid="dev-notification-actions">
                <BaseButton size="sm" data-testid="dev-notification-send" @click="sendTest">Send test notification</BaseButton>
                <div class="min-w-0 text-xs leading-relaxed" role="status" aria-live="polite">
                  <span v-if="pending" class="flex items-center gap-2 text-text-2" data-testid="dev-notification-pending">
                    <span>Sending in {{ remaining }}s…</span>
                    <button
                      type="button"
                      class="cursor-pointer rounded-md border border-border px-2 py-0.5 text-[11px] font-medium text-text-2 hover:bg-chip hover:text-text"
                      data-testid="dev-notification-cancel"
                      @click="cancelPending"
                    >Cancel</button>
                  </span>
                  <span v-else-if="result" :class="resultToneClass[result.tone]" data-testid="dev-notification-result">{{ result.text }}</span>
                </div>
              </div>
            </div>
          </BaseCard>
        </SettingsSection>
      </SettingsPage>
    </div>
  </div>
</template>
