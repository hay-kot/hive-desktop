<script setup lang="ts">
// A workspace's schedules beside the chat pane: the definitions on top, the
// runs they produced beneath. It is a sibling of the pane column, never inside
// it, for the canvas pane's reason (opening it must not re-key the terminal
// host), and it follows the focused workspace (the route's :workspace param)
// rather than the open chat, because a schedule belongs to the manifest, not
// to a session.
import { computed, ref, watch } from 'vue'
import IconCalendarClock from '~icons/lucide/calendar-clock'
import IconMessageSquare from '~icons/lucide/message-square'
import IconPencil from '~icons/lucide/pencil'
import IconPlay from '~icons/lucide/play'
import IconPlus from '~icons/lucide/plus'
import IconTrash2 from '~icons/lucide/trash-2'
import IconX from '~icons/lucide/x'
import AppSwitch from './AppSwitch.vue'
import ConfirmationDialog from './ConfirmationDialog.vue'
import PanelResizeHandle from './PanelResizeHandle.vue'
import ScheduleEditorDialog from './ScheduleEditorDialog.vue'
import { useAgentSchedules } from '../composables/useAgentSchedules'
import { useConfirmation } from '../composables/useConfirmation'
import { useResizablePanel } from '../composables/useResizablePanel'
import { timeLabel } from '../lib/activityPresentation'
import { relativeAge } from '../lib/age'
import type { AgentSchedule, AgentScheduleRun, ScheduleEditRequest } from '../lib/agentWorkspacesClient'

const DAY_MS = 24 * 60 * 60 * 1000
/** Under a minute out, a countdown reads as noise; the row says it is up instead. */
const IMMINENT_MS = 60 * 1000

const props = defineProps<{
  /** The focused workspace directory, which is what the rows belong to. */
  workspace: string
  /** Its display name for the header. */
  workspaceName: string
}>()
// open-chat carries the run's session id: the pane asks the area to open that
// chat, it never writes ?chat itself, because a finished scheduled chat has to
// be relaunched and only the row-click path does that.
const emit = defineEmits<{ close: []; 'open-chat': [session: number] }>()

const { schedules, runs, loading, loaded, error, load, save, remove, runNow, setDisabled } = useAgentSchedules()

watch(() => props.workspace, (dir) => void load(dir), { immediate: true })

// ── Rows ────────────────────────────────────────────────────────────────────
function nextRunLabel(schedule: AgentSchedule): string {
  if (schedule.disabled) return 'paused'
  if (schedule.nextRunAt === null) return 'not scheduled'
  const now = Date.now()
  const delta = schedule.nextRunAt - now
  if (delta <= IMMINENT_MS) return 'due now'
  // relativeAge measures backwards from its second argument, so the two are
  // swapped here to get the same terse form ("2h") for a time still to come.
  if (delta < DAY_MS) return `in ${relativeAge(now, schedule.nextRunAt)}`
  return new Date(schedule.nextRunAt).toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit', hour12: false })
}

const REASON_LABELS: Record<string, string> = { due: 'due', catch_up: 'catch-up', manual: 'manual' }

function reasonLabel(run: AgentScheduleRun): string {
  return REASON_LABELS[run.reason] ?? run.reason
}

// JobsPopover's pill vocabulary: a launch reads as success, a failure as an
// error, and everything else stays neutral chrome.
function statusClasses(status: string): string {
  if (status === 'failed') return 'border-severity-error-border bg-severity-error-tint text-severity-error'
  if (status === 'launched') return 'border-severity-success-border bg-severity-success-tint text-severity-success'
  return 'border-border bg-chip text-text-2'
}

// A run list spanning days needs the day; one from today does not.
function runStamp(at: number): string {
  const when = new Date(at)
  if (when.toDateString() === new Date().toDateString()) return timeLabel(at)
  return `${when.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${timeLabel(at)}`
}

// ── Actions ─────────────────────────────────────────────────────────────────
const busyId = ref('')
const actionError = ref('')

function fail(cause: unknown, fallback: string): void {
  actionError.value = cause instanceof Error && cause.message ? cause.message : fallback
}

async function toggleEnabled(schedule: AgentSchedule, enabled: boolean): Promise<void> {
  if (busyId.value) return
  busyId.value = schedule.id
  actionError.value = ''
  try {
    await setDisabled(schedule, !enabled)
  } catch (cause) {
    fail(cause, 'The schedule could not be saved.')
  } finally {
    busyId.value = ''
  }
}

async function fireNow(schedule: AgentSchedule): Promise<void> {
  if (busyId.value) return
  busyId.value = schedule.id
  actionError.value = ''
  try {
    await runNow(props.workspace, schedule.id)
  } catch (cause) {
    fail(cause, 'The schedule could not be run.')
  } finally {
    busyId.value = ''
  }
}

const confirmation = useConfirmation()

function requestDelete(schedule: AgentSchedule): void {
  confirmation.request({
    title: 'Delete schedule?',
    description: `${schedule.name || schedule.id} stops firing and leaves the workspace manifest. Chats it already started are untouched.`,
    confirmLabel: 'Delete schedule',
    onConfirm: async () => { await remove(props.workspace, schedule.id) },
  })
}

// ── The editor ──────────────────────────────────────────────────────────────
const editorOpen = ref(false)
const editing = ref<AgentSchedule | null>(null)
const editorBusy = ref(false)
const editorError = ref('')

function openNew(): void {
  editing.value = null
  editorError.value = ''
  editorOpen.value = true
}

function openEdit(schedule: AgentSchedule): void {
  editing.value = schedule
  editorError.value = ''
  editorOpen.value = true
}

async function submitEdit(edit: ScheduleEditRequest): Promise<void> {
  editorBusy.value = true
  editorError.value = ''
  try {
    await save(edit)
    editorOpen.value = false
  } catch (cause) {
    editorError.value = cause instanceof Error && cause.message ? cause.message : 'The schedule could not be saved.'
  } finally {
    editorBusy.value = false
  }
}

const showEmpty = computed(() => loaded.value && !loading.value && !schedules.value.length)

const { size: paneWidth, startResize: startPaneResize, step: stepPane } = useResizablePanel({
  storageKey: 'hive.panel.agents.schedules',
  defaultSize: 380,
  min: 300,
  max: 900,
  edge: 'left',
})
</script>

<template>
  <aside
    class="relative flex shrink-0 flex-col border-l border-border bg-pane"
    :style="{ width: paneWidth + 'px' }"
    data-testid="agent-schedules-pane"
  >
    <PanelResizeHandle edge="left" name="agents-schedules" :start="startPaneResize" :step="stepPane" />

    <div class="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1">
      <span class="flex h-6 min-w-0 items-center gap-1.5 px-1.5">
        <IconCalendarClock class="size-3.5 shrink-0 text-text-4" aria-hidden="true" />
        <span class="min-w-0 truncate text-[12px] font-semibold text-text" data-testid="agent-schedules-workspace">{{ workspaceName || workspace }}</span>
      </span>
      <div class="min-w-0 flex-1" />
      <button
        type="button"
        class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
        title="New schedule"
        aria-label="New schedule"
        data-testid="agent-schedules-new"
        @click="openNew"
      ><IconPlus class="size-3.5" /></button>
      <button
        type="button"
        class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
        title="Close schedules"
        aria-label="Close schedules"
        data-testid="agent-schedules-close"
        @click="emit('close')"
      ><IconX class="size-3.5" /></button>
    </div>

    <div class="hive-scroll min-h-0 flex-1 overflow-y-auto">
      <p v-if="error" class="px-3 py-2 text-xs leading-relaxed text-severity-error" data-testid="agent-schedules-error">{{ error }}</p>
      <p v-if="actionError" class="px-3 py-2 text-xs leading-relaxed text-severity-error" data-testid="agent-schedules-action-error">{{ actionError }}</p>

      <p
        v-if="loading && !loaded"
        class="px-3 py-2 font-mono text-xs text-text-4"
        data-testid="agent-schedules-loading"
      >Loading…</p>

      <div v-else-if="showEmpty" class="flex flex-col gap-2 px-4 py-4" data-testid="agent-schedules-empty">
        <p class="text-xs leading-relaxed text-text-3">
          A schedule starts a chat in this workspace on a timetable, like every Friday at 09:00.
        </p>
        <p class="text-xs leading-relaxed text-text-4">
          Its prompt is a template, so one schedule can ask for everything since the last run.
        </p>
      </div>

      <div v-else class="divide-y divide-border">
        <div
          v-for="schedule in schedules"
          :key="schedule.id"
          class="flex items-start gap-2 px-3 py-2.5"
          :data-testid="`agent-schedule-row-${schedule.id}`"
        >
          <div class="min-w-0 flex-1">
            <div class="flex min-w-0 items-center gap-1.5">
              <span class="min-w-0 truncate text-[12.5px] font-medium" :class="schedule.disabled ? 'text-text-3' : 'text-text'">{{ schedule.name || schedule.id }}</span>
              <span
                v-if="schedule.lastRun"
                class="shrink-0 rounded-full border px-1.5 py-px text-[10px]"
                :class="statusClasses(schedule.lastRun.status)"
                :title="schedule.lastRun.error"
                :data-testid="`agent-schedule-last-${schedule.id}`"
              >{{ schedule.lastRun.status }} · {{ reasonLabel(schedule.lastRun) }}</span>
            </div>
            <div class="mt-0.5 flex min-w-0 items-center gap-1.5 font-mono text-[10.5px] text-text-4">
              <span class="truncate">{{ schedule.cron }}</span>
              <span aria-hidden="true">·</span>
              <span class="shrink-0" :data-testid="`agent-schedule-next-${schedule.id}`">{{ nextRunLabel(schedule) }}</span>
            </div>
          </div>

          <div class="flex shrink-0 items-center gap-1">
            <AppSwitch
              :model-value="!schedule.disabled"
              size="sm"
              :disabled="busyId === schedule.id"
              :aria-label="schedule.disabled ? `Enable ${schedule.name || schedule.id}` : `Pause ${schedule.name || schedule.id}`"
              :testid="`agent-schedule-toggle-${schedule.id}`"
              @update:model-value="(value) => toggleEnabled(schedule, value)"
            />
            <button
              type="button"
              class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text disabled:cursor-default disabled:opacity-40"
              title="Run now"
              aria-label="Run now"
              :disabled="busyId === schedule.id"
              :data-testid="`agent-schedule-run-${schedule.id}`"
              @click="fireNow(schedule)"
            ><IconPlay class="size-3" /></button>
            <button
              type="button"
              class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
              title="Edit schedule"
              aria-label="Edit schedule"
              :data-testid="`agent-schedule-edit-${schedule.id}`"
              @click="openEdit(schedule)"
            ><IconPencil class="size-3" /></button>
            <button
              type="button"
              class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-severity-error"
              title="Delete schedule"
              aria-label="Delete schedule"
              :data-testid="`agent-schedule-delete-${schedule.id}`"
              @click="requestDelete(schedule)"
            ><IconTrash2 class="size-3" /></button>
          </div>
        </div>
      </div>

      <template v-if="runs.length">
        <p class="border-t border-border px-3 pb-1 pt-3 text-[10.5px] font-medium uppercase tracking-wide text-text-4">Run history</p>
        <div class="divide-y divide-border">
          <div
            v-for="run in runs"
            :key="run.id"
            class="flex items-start gap-2 px-3 py-2"
            :data-testid="`agent-schedule-run-row-${run.id}`"
          >
            <span class="mt-px shrink-0 font-mono text-[10.5px] text-text-4" :title="new Date(run.startedAt).toLocaleString()">{{ runStamp(run.startedAt) }}</span>
            <div class="min-w-0 flex-1">
              <div class="flex min-w-0 items-center gap-1.5">
                <span class="min-w-0 truncate text-[11.5px] text-text-2">{{ run.scheduleName || run.scheduleId }}</span>
                <span
                  class="shrink-0 rounded-full border px-1.5 py-px text-[10px]"
                  :class="statusClasses(run.status)"
                >{{ run.status }} · {{ reasonLabel(run) }}</span>
                <span
                  v-if="run.missed > 0"
                  class="shrink-0 font-mono text-[10px] text-text-4"
                  :data-testid="`agent-schedule-run-missed-${run.id}`"
                >{{ run.missed }} missed</span>
              </div>
              <p
                v-if="run.error"
                class="mt-0.5 text-[10.5px] leading-relaxed text-severity-error"
                :data-testid="`agent-schedule-run-error-${run.id}`"
              >{{ run.error }}</p>
            </div>
            <button
              v-if="run.sessionId"
              type="button"
              class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
              title="Open chat"
              aria-label="Open chat"
              :data-testid="`agent-schedule-open-chat-${run.id}`"
              @click="emit('open-chat', run.sessionId)"
            ><IconMessageSquare class="size-3" /></button>
          </div>
        </div>
      </template>
    </div>

    <ScheduleEditorDialog
      v-if="editorOpen"
      :workspace="workspace"
      :schedule="editing"
      :busy="editorBusy"
      :error="editorError"
      @close="editorOpen = false"
      @save="submitEdit"
    />

    <ConfirmationDialog
      v-if="confirmation.open.value && confirmation.options.value"
      :title="confirmation.options.value.title"
      :description="confirmation.options.value.description"
      :confirm-label="confirmation.options.value.confirmLabel"
      :busy="confirmation.busy.value"
      :error="confirmation.error.value"
      testid="agent-schedules-delete-confirmation"
      @confirm="confirmation.confirm"
      @cancel="confirmation.cancel"
    />
  </aside>
</template>
