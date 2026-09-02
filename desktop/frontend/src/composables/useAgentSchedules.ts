import { ref, type Ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { useAgentWorkspaces } from './useAgentWorkspaces'
import type {
  AgentSchedule,
  AgentSchedulePreview,
  AgentScheduleRun,
  ScheduleEditRequest,
  SchedulePreviewRequest,
} from '../lib/agentWorkspacesClient'

// One workspace's schedules and its run history, held as a module singleton
// beside useAgentSessionsAll and for the same reasons: one Chats area is ever
// mounted, and reusing useAgentWorkspaces' client/ready() rather than probing
// again keeps every list on one availability answer. The state is keyed by the
// focused workspace, and load(dir) is what re-points it, so a workspace switch
// replaces the rows rather than merging two workspaces' schedules.

/** How much history the pane's run list asks for. */
const RUN_HISTORY_LIMIT = 50

const workspace = ref('')
const schedules = ref<AgentSchedule[]>([])
const runs = ref<AgentScheduleRun[]>([])
const loading = ref(false)
const loaded = ref(false)
const error = ref<string | null>(null)

let started = false
// A wake-up and a workspace switch race, and an older read must never land on
// top of a newer one's rows (useAgentCanvas' generation guard).
let sequence = 0

function start(): void {
  if (started) return
  started = true
  // Events.On rather than useWailsEvent: a module singleton has no effect
  // scope for onScopeDispose to attach to, and the subscription has to outlive
  // any one pane mount so reopening the pane shows rows that are already
  // current. The payload names the workspace that changed and is ignored on
  // purpose: this state only ever holds the focused workspace, so that is what
  // gets re-read.
  Events.On('schedules:updated', () => { void load(workspace.value) })
}

function message(cause: unknown, fallback: string): string {
  return cause instanceof Error && cause.message ? cause.message : fallback
}

async function load(dir: string): Promise<void> {
  // Keeping the last-good rows is for a *reload*: across a workspace switch
  // they belong to a different manifest, and the actions on them would name
  // the wrong workspace.
  const switched = dir !== workspace.value
  workspace.value = dir
  const token = ++sequence
  if (switched) {
    schedules.value = []
    runs.value = []
    error.value = null
    loaded.value = false
  }
  if (!dir) {
    loaded.value = true
    return
  }
  const { client, ready } = useAgentWorkspaces()
  await ready()
  if (!client.value || token !== sequence) return
  loading.value = true
  try {
    const [rows, history] = await Promise.all([
      client.value.schedules(dir),
      client.value.scheduleRuns(dir, '', RUN_HISTORY_LIMIT),
    ])
    if (token !== sequence) return
    schedules.value = rows
    runs.value = history
    error.value = null
  } catch (cause) {
    // Keep the last-good rows: a failed revalidation reports itself without
    // collapsing the list it could not refresh (useAgentWorkspaces' rule).
    if (token === sequence) error.value = message(cause, "Could not read this workspace's schedules.")
  } finally {
    if (token === sequence) {
      loading.value = false
      loaded.value = true
    }
  }
}

async function resolveClient() {
  const { client, ready } = useAgentWorkspaces()
  await ready()
  if (!client.value) throw new Error('The Chats area is unavailable.')
  return client.value
}

// Every mutation re-reads the workspace rather than folding a response into the
// list: a save moves nextRunAt, a run adds history, and a delete can leave a
// cursor behind, all of which the Go side computes.
async function save(edit: ScheduleEditRequest): Promise<AgentSchedule> {
  const saved = await (await resolveClient()).saveSchedule(edit)
  await load(edit.workspace)
  return saved
}

async function remove(dir: string, id: string): Promise<void> {
  await (await resolveClient()).deleteSchedule(dir, id)
  await load(dir)
}

async function runNow(dir: string, id: string): Promise<AgentScheduleRun> {
  const run = await (await resolveClient()).runSchedule(dir, id)
  await load(dir)
  return run
}

/** The enable switch: a save with the flag flipped, so the manifest stays the one source. */
async function setDisabled(schedule: AgentSchedule, disabled: boolean): Promise<void> {
  await save({
    workspace: schedule.workspace,
    id: schedule.id,
    name: schedule.name,
    cron: schedule.cron,
    prompt: schedule.prompt,
    onMissed: schedule.onMissed,
    disabled,
  })
}

async function preview(request: SchedulePreviewRequest): Promise<AgentSchedulePreview> {
  return await (await resolveClient()).previewSchedule(request)
}

export function useAgentSchedules(): {
  workspace: Ref<string>
  schedules: Ref<AgentSchedule[]>
  runs: Ref<AgentScheduleRun[]>
  loading: Ref<boolean>
  loaded: Ref<boolean>
  error: Ref<string | null>
  load: (workspace: string) => Promise<void>
  save: (edit: ScheduleEditRequest) => Promise<AgentSchedule>
  remove: (workspace: string, id: string) => Promise<void>
  runNow: (workspace: string, id: string) => Promise<AgentScheduleRun>
  setDisabled: (schedule: AgentSchedule, disabled: boolean) => Promise<void>
  preview: (request: SchedulePreviewRequest) => Promise<AgentSchedulePreview>
} {
  start()
  return { workspace, schedules, runs, loading, loaded, error, load, save, remove, runNow, setDisabled, preview }
}

export function resetAgentSchedulesForTests(): void {
  workspace.value = ''
  schedules.value = []
  runs.value = []
  loading.value = false
  loaded.value = false
  error.value = null
  started = false
  sequence = 0
}
