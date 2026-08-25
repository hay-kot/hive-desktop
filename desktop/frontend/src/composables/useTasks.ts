import { ref, watch, type Ref } from 'vue'
import { useStorage } from '@vueuse/core'
import {
  DeleteTask,
  ListTasks,
  PruneTasks,
  SetTaskStatus,
  TaskDetail as ReadTaskDetail,
  TaskRepoKeys,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/tasksservice'
import type { TaskDetail, TaskItem } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'
import { appErrorKind, errorText } from '../lib/appError'
import { DEFAULT_TASK_FILTER, type TaskFilterId } from '../lib/tasksPresentation'
import { useWindowFocus } from './useWindowFocus'

// useTasks is a module singleton, mirroring useSessionStatuses.ts's polling
// shape: a 2s self-rescheduling setTimeout (not setInterval, so a slow read
// cannot pile up overlapping requests), a generation counter that lets
// startPolling/stopPolling cut a running chain off cleanly, and per-read
// sequence counters so an out-of-order response can never overwrite a newer
// one. The hc store is a CLI's too — the poll is what surfaces a `hive hc`
// write made outside the app within one tick.

const POLL_INTERVAL_MS = 2000

const repoKey = useStorage('hive.tasks.repo', '')
const filter = useStorage<TaskFilterId>('hive.tasks.filter', DEFAULT_TASK_FILTER)
// Epics default-expanded, so this persists the exception (which ids are
// collapsed) rather than which are open.
const collapsedIds = useStorage<string[]>('hive.tasks.collapsed', [])

const items = ref<TaskItem[]>([])
const repoKeys = ref<string[]>([])
const selectedId = ref<string | null>(null)
const detail = ref<TaskDetail | null>(null)
const loading = ref(false)
const loaded = ref(false)
const error = ref<string | null>(null)
// Set when the backend reports the hc store itself as unreachable. Distinct
// from `error`: that is a normal fetch failure a retry can shake off, this is
// the runtime not being there at all, so polling stops until a manual
// refresh proves it is back.
const unavailable = ref(false)

let requestSequence = 0
let detailSequence = 0
let repoKeysSequence = 0
let pollGeneration = 0
let pollTimer: ReturnType<typeof setTimeout> | undefined
// True whenever the view wants live updates (between startPolling and
// stopPolling calls). Kept apart from whether the timer chain is actually
// running so an `unavailable` halt can be resumed by refresh() without the
// view having to call startPolling() again.
let pollingRequested = false

function haltPollLoop(): void {
  ++pollGeneration
  clearTimeout(pollTimer)
  pollTimer = undefined
}

async function loadRepoKeys(): Promise<void> {
  const sequence = ++repoKeysSequence
  try {
    const keys = await TaskRepoKeys()
    if (sequence !== repoKeysSequence) return
    repoKeys.value = keys ?? []
  } catch (err) {
    if (sequence !== repoKeysSequence) return
    console.warn('Unable to load task repo keys', err)
  }
}

async function reloadList(): Promise<void> {
  const sequence = ++requestSequence
  loading.value = true
  try {
    const result = await ListTasks(repoKey.value)
    if (sequence !== requestSequence) return
    items.value = result ?? []
    error.value = null
    unavailable.value = false
  } catch (err) {
    if (sequence !== requestSequence) return
    if (appErrorKind(err) === 'unavailable') {
      unavailable.value = true
      haltPollLoop()
    } else {
      // Keep the last-seen items: a transient failure must not blank a list
      // the user was already looking at.
      error.value = errorText(err, 'Could not load tasks.')
    }
  } finally {
    if (sequence === requestSequence) {
      loading.value = false
      loaded.value = true
    }
  }
}

async function loadDetail(id: string, sequence: number): Promise<void> {
  try {
    const result = await ReadTaskDetail(id)
    if (sequence !== detailSequence || selectedId.value !== id) return
    detail.value = result
  } catch (err) {
    if (sequence !== detailSequence || selectedId.value !== id) return
    if (appErrorKind(err) === 'not_found') {
      // The item vanished externally (e.g. a CLI delete) — the selection it
      // named no longer exists.
      selectedId.value = null
      detail.value = null
      return
    }
    console.warn('Unable to load task detail', err)
  }
}

async function reloadDetailIfSelected(): Promise<void> {
  const id = selectedId.value
  if (!id) return
  await loadDetail(id, ++detailSequence)
}

async function tick(): Promise<void> {
  await Promise.all([reloadList(), reloadDetailIfSelected()])
}

async function poll(generation: number): Promise<void> {
  await tick()
  if (generation !== pollGeneration) return
  pollTimer = setTimeout(() => { void poll(generation) }, POLL_INTERVAL_MS)
}

function startPolling(): void {
  pollingRequested = true
  // The hc store is not coming back mid-process, so don't spend a doomed
  // request every time the view mounts — refresh() is what proves it is back.
  if (unavailable.value) return
  clearTimeout(pollTimer)
  const generation = ++pollGeneration
  void loadRepoKeys()
  void poll(generation)
}

function stopPolling(): void {
  pollingRequested = false
  haltPollLoop()
}

async function refresh(): Promise<void> {
  await Promise.all([reloadList(), reloadDetailIfSelected(), loadRepoKeys()])
  // A successful refresh can prove the runtime is back; resume the chain a
  // prior `unavailable` halt cut off, if the view still wants it running.
  // Scheduled rather than fetched immediately — refresh() just read fresh
  // data, so the next read is due a full interval from now, not right away.
  if (pollingRequested && pollTimer === undefined && !unavailable.value) {
    const generation = ++pollGeneration
    pollTimer = setTimeout(() => { void poll(generation) }, POLL_INTERVAL_MS)
  }
}

// A repo scope change is a server-side filter, not a client-side one (unlike
// `filter`, which tasksPresentation applies over whatever is already
// loaded) — so it needs its own round trip.
watch(repoKey, () => { void refresh() })

const { focused } = useWindowFocus()
watch(focused, (isFocused) => {
  // Regaining focus is passive, not the user asking again — while known
  // unavailable, only an explicit refresh() (startPolling's own guard,
  // mirrored here) is allowed to retry.
  if (isFocused && pollingRequested && !unavailable.value) void refresh()
})

function select(id: string | null): void {
  selectedId.value = id
  if (id === null) {
    detail.value = null
    return
  }
  void loadDetail(id, ++detailSequence)
}

async function setStatus(id: string, status: string): Promise<void> {
  await SetTaskStatus(id, status)
  await refresh()
}

async function remove(id: string): Promise<void> {
  await DeleteTask(id)
  if (selectedId.value === id) select(null)
  await refresh()
}

/** Dry-run count for the confirm dialog — call before {@link prune}. */
async function pruneDryRun(olderThanDays: number, repoKeyScope: string): Promise<number> {
  return await PruneTasks(olderThanDays, repoKeyScope, true)
}

async function prune(olderThanDays: number, repoKeyScope: string): Promise<void> {
  await PruneTasks(olderThanDays, repoKeyScope, false)
  await refresh()
}

function isCollapsed(id: string): boolean {
  return collapsedIds.value.includes(id)
}

function toggleCollapsed(id: string): void {
  collapsedIds.value = isCollapsed(id)
    ? collapsedIds.value.filter((collapsed) => collapsed !== id)
    : [...collapsedIds.value, id]
}

export function useTasks(): {
  repoKey: Ref<string>
  filter: Ref<TaskFilterId>
  collapsedIds: Ref<string[]>
  items: Ref<TaskItem[]>
  repoKeys: Ref<string[]>
  selectedId: Ref<string | null>
  detail: Ref<TaskDetail | null>
  loading: Ref<boolean>
  loaded: Ref<boolean>
  error: Ref<string | null>
  unavailable: Ref<boolean>
  startPolling: () => void
  stopPolling: () => void
  refresh: () => Promise<void>
  select: (id: string | null) => void
  setStatus: (id: string, status: string) => Promise<void>
  remove: (id: string) => Promise<void>
  pruneDryRun: (olderThanDays: number, repoKeyScope: string) => Promise<number>
  prune: (olderThanDays: number, repoKeyScope: string) => Promise<void>
  isCollapsed: (id: string) => boolean
  toggleCollapsed: (id: string) => void
} {
  return {
    repoKey,
    filter,
    collapsedIds,
    items,
    repoKeys,
    selectedId,
    detail,
    loading,
    loaded,
    error,
    unavailable,
    startPolling,
    stopPolling,
    refresh,
    select,
    setStatus,
    remove,
    pruneDryRun,
    prune,
    isCollapsed,
    toggleCollapsed,
  }
}

export function resetTasksForTests(): void {
  stopPolling()
  ++requestSequence
  ++detailSequence
  ++repoKeysSequence
  repoKey.value = ''
  filter.value = DEFAULT_TASK_FILTER
  collapsedIds.value = []
  items.value = []
  repoKeys.value = []
  selectedId.value = null
  detail.value = null
  loading.value = false
  loaded.value = false
  error.value = null
  unavailable.value = false
}
