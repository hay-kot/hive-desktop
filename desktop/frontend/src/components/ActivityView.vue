<script setup lang="ts">
// The Activity view (design 15a — "ledger"): a flat, day-grouped chronology of
// what the app did — refreshes, sessions, automatic and manual actions, config
// reloads, and errors. A left time gutter anchors every row, severity reads by
// a colored rail rather than a filled icon, and one segmented control filters
// the stream. Events are recorded by backend subsystems through the
// activity.Recorder and by the frontend via ActivityService.Record; this view
// only reads and presents them. Reached from the titlebar Activity link.
import { computed, onMounted, ref } from 'vue'
import IconSearch from '~icons/lucide/search'
import { useActivity } from '../composables/useActivity'
import { useEscapeToClose } from '../composables/useEscapeToClose'
import ViewHeader from './settings/ViewHeader.vue'
import {
  ACTIVITY_FILTERS,
  eventStyleKey,
  filterCounts,
  groupEventsByDay,
  matchesFilter,
  matchesSearch,
  timeLabel,
  type ActivityFilterId,
  type ActivityStyleKey,
} from '../lib/activityPresentation'

const emit = defineEmits<{ close: [] }>()

const { events, loading, error, load, markSeen } = useActivity()

const activeFilter = ref<ActivityFilterId>('all')
const search = ref('')

// Opening the view clears the titlebar's unseen indicator.
markSeen()

const counts = computed(() => filterCounts(events.value))
const filtered = computed(() =>
  events.value.filter((e) => matchesFilter(e, activeFilter.value) && matchesSearch(e, search.value)),
)
const groups = computed(() => groupEventsByDay(filtered.value))

// Severity/category → the row's dot color and (for the two that warrant it) its
// emphasis rail + tint. Errors and auto-actions get a colored left rail because
// they are the events a reader scans for; everything else stays quiet and only
// lifts on hover. The rail is an inset shadow, not a border, so it never colors
// the row's divider on the sides it doesn't own.
const STYLES: Record<ActivityStyleKey, { dot: string; rail: string }> = {
  error: { dot: 'bg-severity-error', rail: 'bg-severity-error-tint shadow-[inset_2px_0_0_var(--hv-severity-error)]' },
  auto_action: { dot: 'bg-accent', rail: 'bg-severity-auto-tint shadow-[inset_2px_0_0_var(--hv-accent)]' },
  refresh: { dot: 'bg-text-4', rail: '' },
  session: { dot: 'bg-severity-success', rail: '' },
  action: { dot: 'bg-node-purple', rail: '' },
  config: { dot: 'bg-text-4', rail: '' },
  system: { dot: 'bg-severity-info', rail: '' },
}

const ledger = computed(() =>
  groups.value.map((group) => ({
    ...group,
    rows: group.events.map((event) => ({ event, style: STYLES[eventStyleKey(event)] })),
  })),
)

function countClass(filterId: ActivityFilterId): string {
  if (filterId === 'error' && counts.value.error > 0) return 'text-severity-error'
  return activeFilter.value === filterId ? 'text-text-2' : 'text-text-4'
}

useEscapeToClose(() => emit('close'))

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-1 flex-col" data-testid="activity-view">
    <ViewHeader close-testid="activity-close" @close="emit('close')">
      <template #title>
        <span class="text-[13px] font-semibold text-text">Activity</span>
        <span class="font-mono text-[11px] text-text-4">{{ events.length }} {{ events.length === 1 ? 'event' : 'events' }}</span>
      </template>
    </ViewHeader>

    <!-- toolbar: one segmented filter + search -->
    <div class="flex shrink-0 items-center gap-2.5 border-b border-row bg-sidebar px-5 py-2.5">
      <div class="flex items-center gap-0.5 rounded-lg border border-strong bg-app p-0.5">
        <button
          v-for="filter in ACTIVITY_FILTERS"
          :key="filter.id"
          type="button"
          class="flex h-[26px] cursor-pointer items-center gap-1.5 rounded-md px-2.5 text-[12.5px] transition-colors"
          :class="activeFilter === filter.id
            ? 'bg-chip font-semibold text-text'
            : 'text-text-2 hover:bg-row-hover hover:text-text'"
          :data-testid="`activity-filter-${filter.id}`"
          :aria-pressed="activeFilter === filter.id"
          @click="activeFilter = filter.id"
        >
          {{ filter.label }}
          <span class="font-mono text-[10.5px]" :class="countClass(filter.id)">{{ counts[filter.id] }}</span>
        </button>
      </div>
      <div class="flex-1" />
      <label class="flex w-[230px] items-center gap-2 rounded-lg border border-strong bg-app px-2.5 py-1.5 focus-within:border-text-3">
        <IconSearch class="size-3.5 shrink-0 text-text-4" />
        <input
          v-model="search"
          type="text"
          placeholder="Filter activity…"
          class="min-w-0 flex-1 bg-transparent text-[12.5px] text-text placeholder:text-text-4 focus:outline-none"
          data-testid="activity-search"
        />
      </label>
    </div>

    <!-- ledger -->
    <div class="hive-scroll min-h-0 flex-1 overflow-y-auto bg-app" data-testid="activity-log">
      <div v-if="error" class="flex flex-col items-center gap-3 px-6 py-16 text-center font-mono text-xs text-text-4">
        <span data-testid="activity-error">Couldn't load activity — {{ error }}</span>
        <button class="cursor-pointer rounded border border-strong px-3 py-1.5 text-text-2 hover:text-text" @click="load">Retry</button>
      </div>
      <div v-else-if="!events.length && loading" class="px-6 py-16 text-center font-mono text-xs text-text-4">Loading activity…</div>
      <div v-else-if="!groups.length" class="px-6 py-16 text-center font-mono text-xs text-text-4" data-testid="activity-empty">
        {{ events.length ? 'No activity matches this filter.' : 'No activity yet. Refreshes, sessions, and actions will show up here.' }}
      </div>

      <template v-for="group in ledger" v-else :key="group.key">
        <div class="sticky -top-px z-[1] flex items-center gap-3 border-b border-row bg-app px-5 py-2 pt-[9px]">
          <span class="font-mono text-[10.5px] uppercase tracking-[.14em] text-text-2">{{ group.label }}</span>
          <span v-if="group.isRelative" class="font-mono text-[10.5px] uppercase tracking-[.06em] text-text-4">{{ group.dateLabel }}</span>
          <div class="h-px flex-1 bg-row" />
          <span class="font-mono text-[10.5px] text-text-4">{{ group.events.length }} {{ group.events.length === 1 ? 'event' : 'events' }}</span>
        </div>
        <div class="divide-y divide-row">
          <div
            v-for="{ event, style } in group.rows"
            :key="event.id"
            class="flex px-5 py-2.5 transition-colors"
            :class="style.rail || 'hover:bg-row-hover'"
            data-testid="activity-row"
          >
            <span class="w-[72px] shrink-0 pt-px font-mono text-[11.5px] text-text-3">{{ timeLabel(event.createdAt) }}</span>
            <span class="flex w-4 shrink-0 justify-center pt-[7px]"><span class="size-1.5 rounded-full" :class="style.dot" /></span>
            <div class="min-w-0 flex-1 pl-3">
              <div class="text-[13px] leading-normal text-text">{{ event.title }}</div>
              <div v-if="event.body || event.source" class="mt-0.5 flex flex-wrap items-baseline gap-x-3 gap-y-1 text-[11.5px] text-text-3">
                <span v-if="event.body">{{ event.body }}</span>
                <span v-if="event.source" class="font-mono text-text-4">{{ event.source }}</span>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- status strip -->
    <div
      v-if="events.length && !error"
      class="flex h-[30px] shrink-0 items-center gap-3.5 border-t border-row bg-sidebar px-5 font-mono text-[11px] text-text-3"
      data-testid="activity-status"
    >
      <span class="flex items-center gap-1.5"><span class="size-1.5 rounded-full bg-severity-success" style="animation: hivePulse 2s infinite" />live</span>
      <span>{{ events.length }} {{ events.length === 1 ? 'event' : 'events' }} loaded</span>
    </div>
  </div>
</template>
