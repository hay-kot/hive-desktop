<script setup lang="ts">
// Tasks hub view (design modeled on ActivityView): a tree+detail split over
// hc's issue tracker. This owns the segmented filter, repo scope, refresh,
// and prune; TaskDetailPane is self-contained via the same useTasks()
// singleton and owns everything about the selection (see its own header
// comment). Reached from the titlebar, like ActivityView.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import IconEraser from '~icons/lucide/eraser'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import AppSelect, { type AppSelectOption } from './AppSelect.vue'
import ConfirmationDialog from './ConfirmationDialog.vue'
import TaskDetailPane from './TaskDetailPane.vue'
import TaskTreeRow from './TaskTreeRow.vue'
import ViewHeader from './settings/ViewHeader.vue'
import { useEscapeToClose } from '../composables/useEscapeToClose'
import { useTasks } from '../composables/useTasks'
import { errorText } from '../lib/appError'
import { buildTaskTree, filterCounts, TASK_FILTERS, type TaskTreeNode } from '../lib/tasksPresentation'

const emit = defineEmits<{ close: [] }>()

const {
  repoKey, filter, items, repoKeys, selectedId, loading, loaded, error, unavailable,
  startPolling, stopPolling, refresh, select, isCollapsed, toggleCollapsed,
  pruneDryRun, prune,
} = useTasks()

const counts = computed(() => filterCounts(items.value))

interface FlatRow { node: TaskTreeNode; depth: number }

// buildTaskTree already decides per-node visibility (an ancestor of a match
// always renders); this walk turns that tree into the flat, indent-annotated
// list the template renders, skipping a node's children while it's collapsed.
function flatten(nodes: TaskTreeNode[], depth: number, out: FlatRow[]): void {
  for (const node of nodes) {
    if (!node.visible) continue
    out.push({ node, depth })
    if (node.children.length && !isCollapsed(node.item.id)) flatten(node.children, depth + 1, out)
  }
}

const rows = computed(() => {
  const out: FlatRow[] = []
  flatten(buildTaskTree(items.value, filter.value), 0, out)
  return out
})

const repoOptions = computed<AppSelectOption[]>(() => [
  { value: '', label: 'All repositories' },
  ...repoKeys.value.map((key) => ({ value: key, label: key })),
])

// ── Prune ────────────────────────────────────────────────────────────────
// 30 days is a guess absent a documented convention: old enough that a fresh
// task never gets swept, short enough that a stale root doesn't linger
// indefinitely before anyone notices.
const PRUNE_OLDER_THAN_DAYS = 30

const pruneConfirmOpen = ref(false)
const pruneDryRunCount = ref<number | null>(null)
const pruneBusy = ref(false)
const pruneError = ref<string | null>(null)

async function requestPrune(): Promise<void> {
  pruneError.value = null
  try {
    pruneDryRunCount.value = await pruneDryRun(PRUNE_OLDER_THAN_DAYS, repoKey.value)
    pruneConfirmOpen.value = true
  } catch (err) {
    pruneError.value = errorText(err, 'Could not check what pruning would remove.')
  }
}

async function confirmPrune(): Promise<void> {
  pruneBusy.value = true
  pruneError.value = null
  try {
    await prune(PRUNE_OLDER_THAN_DAYS, repoKey.value)
    pruneConfirmOpen.value = false
  } catch (err) {
    pruneError.value = errorText(err, 'Could not prune tasks.')
  } finally {
    pruneBusy.value = false
  }
}

const pruneDescription = computed(() => {
  const count = pruneDryRunCount.value ?? 0
  const scope = repoKey.value ? ` in ${repoKey.value}` : ''
  return `This removes ${count} task${count === 1 ? '' : 's'} older than ${PRUNE_OLDER_THAN_DAYS} days${scope}. Pruning removes everything nested under a pruned root, regardless of its own status.`
})

useEscapeToClose(() => emit('close'))

onMounted(() => { startPolling() })
onUnmounted(() => { stopPolling() })
</script>

<template>
  <div class="flex h-full min-h-0 flex-1 flex-col" data-testid="tasks-view">
    <ViewHeader>
      <template #title>
        <span class="text-[13px] font-semibold text-text">Tasks</span>
        <span class="font-mono text-[11px] text-text-4">{{ items.length }} {{ items.length === 1 ? 'item' : 'items' }}</span>
      </template>
    </ViewHeader>

    <!-- toolbar: segmented filter + repo scope + refresh + prune -->
    <div class="flex shrink-0 flex-wrap items-center gap-2.5 border-b border-row bg-sidebar px-5 py-2.5">
      <div class="flex items-center gap-0.5 rounded-lg border border-strong bg-app p-0.5">
        <button
          v-for="taskFilter in TASK_FILTERS"
          :key="taskFilter.id"
          type="button"
          class="flex h-[26px] cursor-pointer items-center gap-1.5 rounded-md px-2.5 text-[12.5px] transition-colors"
          :class="filter === taskFilter.id ? 'bg-chip font-semibold text-text' : 'text-text-2 hover:bg-row-hover hover:text-text'"
          :data-testid="`tasks-filter-${taskFilter.id}`"
          :aria-pressed="filter === taskFilter.id"
          @click="filter = taskFilter.id"
        >
          {{ taskFilter.label }}
          <span class="font-mono text-[10.5px] text-text-4">{{ counts[taskFilter.id] }}</span>
        </button>
      </div>

      <div class="w-[220px]">
        <AppSelect
          :model-value="repoKey"
          :options="repoOptions"
          size="sm"
          aria-label="Repository"
          testid="tasks-repo-select"
          @update:model-value="repoKey = $event"
        />
      </div>

      <div class="flex-1" />

      <button
        type="button"
        class="flex cursor-pointer items-center gap-1.5 rounded-lg border border-card px-3 py-1.5 text-[12.5px] font-medium text-text-2 hover:border-strong hover:text-text disabled:cursor-not-allowed disabled:opacity-50"
        :disabled="loading"
        data-testid="tasks-refresh"
        @click="refresh"
      ><IconRefreshCw class="size-3.5" :class="loading ? 'animate-spin' : ''" />Refresh</button>

      <button
        type="button"
        class="flex cursor-pointer items-center gap-1.5 rounded-lg border border-card px-3 py-1.5 text-[12.5px] font-medium text-text-2 hover:border-strong hover:text-text"
        data-testid="tasks-prune"
        @click="requestPrune"
      ><IconEraser class="size-3.5" />Prune</button>
    </div>

    <!-- A transient load failure keeps the last-seen items on screen (see
         useTasks.ts) rather than blanking the tree, so this is a banner, not
         a replacement for it. -->
    <div v-if="error" class="shrink-0 border-b border-severity-error/30 bg-severity-error-tint px-5 py-2 text-xs text-severity-error" data-testid="tasks-error">
      Couldn't refresh tasks — {{ error }}
    </div>
    <!-- A dry-run failure never opens the confirm dialog (there's nothing to
         confirm), so its error has nowhere to show but here — the dialog only
         carries pruneError for a failure once it's already open. -->
    <div v-if="pruneError && !pruneConfirmOpen" class="shrink-0 border-b border-severity-error/30 bg-severity-error-tint px-5 py-2 text-xs text-severity-error" data-testid="tasks-prune-error">
      Couldn't prune tasks — {{ pruneError }}
    </div>

    <!-- tree + detail split -->
    <div class="flex min-h-0 flex-1">
      <div class="hive-scroll min-h-0 flex-1 overflow-y-auto bg-app" data-testid="tasks-tree">
        <div v-if="unavailable" class="flex h-full flex-col items-center justify-center gap-3 px-10 text-center" data-testid="tasks-unavailable">
          <div class="text-[13.5px] font-semibold text-text">Tasks are unavailable</div>
          <!-- useTasks() only tracks unavailability as a boolean (the hc store
               is not reachable at all), never a message for this specific
               state — error.value stays unset here, so the copy is fixed. -->
          <p class="max-w-[380px] text-xs leading-relaxed text-text-3" data-testid="tasks-unavailable-reason">The hc task store is not reachable right now.</p>
          <button type="button" class="cursor-pointer rounded border border-strong px-3 py-1.5 text-xs text-text-2 hover:text-text" data-testid="tasks-retry" @click="refresh">Try again</button>
        </div>
        <div v-else-if="!loaded" class="flex h-full items-center justify-center font-mono text-xs text-text-4">Loading tasks…</div>
        <div v-else-if="!items.length" class="flex h-full flex-col items-center justify-center gap-2 px-10 text-center" data-testid="tasks-empty">
          <div class="text-[13px] text-text-3">No tasks yet.</div>
          <p class="text-xs text-text-4">Agents create tasks with <code class="rounded bg-chip px-1 py-0.5 font-mono text-[11px] text-text-3">hive hc create</code>.</p>
        </div>
        <div v-else-if="!rows.length" class="flex h-full items-center justify-center font-mono text-xs text-text-4" data-testid="tasks-empty-filter">No tasks match this filter.</div>
        <template v-else>
          <TaskTreeRow
            v-for="row in rows"
            :key="row.node.item.id"
            :node="row.node"
            :depth="row.depth"
            :selected="row.node.item.id === selectedId"
            :collapsed="isCollapsed(row.node.item.id)"
            @select="select"
            @toggle="toggleCollapsed"
          />
        </template>
      </div>

      <TaskDetailPane />
    </div>

    <ConfirmationDialog
      v-if="pruneConfirmOpen"
      title="Prune old tasks"
      :description="pruneDescription"
      confirm-label="Prune"
      :busy="pruneBusy"
      :error="pruneError"
      testid="tasks-prune-confirm"
      @confirm="confirmPrune"
      @cancel="pruneConfirmOpen = false"
    />
  </div>
</template>
