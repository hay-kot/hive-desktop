<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useStorage } from '@vueuse/core'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconChevronRight from '~icons/lucide/chevron-right'
import IconPlus from '~icons/lucide/plus'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import IconRotateCw from '~icons/lucide/rotate-cw'
import IconTerminal from '~icons/lucide/terminal'
import IconX from '~icons/lucide/x'
import PanelResizeHandle from './PanelResizeHandle.vue'
import TerminalTab from './TerminalTab.vue'
import { groupTerminalSessions, useTerminalSessions, type TerminalSessionGroup } from '../composables/useTerminalSessions'
import { useTerminalWindows, type TerminalWindowTab, type UseTerminalWindows } from '../composables/useTerminalWindows'
import { useNewSession } from '../composables/useNewSession'
import { useResizablePanel } from '../composables/useResizablePanel'
import { useWailsEvent } from '../composables/useWailsEvent'
import { createTerminalClient, getTerminalEndpoint, type TerminalClient } from '../lib/terminalClient'
import { appErrorMessage } from '../lib/appError'
import { Available } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice'
import '@xterm/xterm/css/xterm.css'

const checking = ref(true)
const available = ref(false)
const reason = ref('')
const client = shallowRef<TerminalClient | null>(null)
const session = shallowRef<UseTerminalWindows | null>(null)
const activeSlug = ref('')
const renamingId = ref('')
const renameDraft = ref('')

const {
  sessions: sessionRows, loading: sessionsLoading, error: sessionsError, reload: reloadSessions,
} = useTerminalSessions()
const { openBlank: openNewSession, prefetch: prefetchNewSession } = useNewSession()
const sessionGroups = computed(() => groupTerminalSessions(sessionRows.value))
const liveCount = computed(() => sessionRows.value.filter((row) => row.state === 'active').length)

function groupAttached(group: TerminalSessionGroup): boolean {
  return group.sessions.some((row) => row.slug === activeSlug.value)
}

// Groups separate by a small gap — except after the attached session's window
// well, whose recessed edge is already a hard boundary.
function gapAbove(index: number): boolean {
  const prev = sessionGroups.value[index - 1]
  if (!prev) return false
  if (collapsedRepos.value.includes(prev.key)) return true
  return prev.sessions[prev.sessions.length - 1]?.slug !== activeSlug.value
}

// Expand/collapse is transient view state, not configuration — localStorage,
// same as the hub sidebar's folder collapse.
const collapsedRepos = useStorage<string[]>('hive.terminal.sidebar.collapsed', [])
function toggleGroup(key: string): void {
  collapsedRepos.value = collapsedRepos.value.includes(key)
    ? collapsedRepos.value.filter((other) => other !== key)
    : [...collapsedRepos.value, key]
}

const { size: sidebarWidth, startResize, step } = useResizablePanel({
  storageKey: 'hive.panel.terminal.sidebar', defaultSize: 250, min: 180, max: 400, edge: 'right',
})

// A launched session lands in the sidebar without a manual refresh: session
// creation runs as a job, and jobs:updated is the wake-up that fires when one
// finishes. Extra reloads are harmless — the list is small.
useWailsEvent('jobs:updated', () => { void reloadSessions() })

const tabs = computed<TerminalWindowTab[]>(() => session.value?.tabs.value ?? [])
const activeWindowId = computed(() => session.value?.activeWindowId.value ?? '')
const status = computed(() => session.value?.status.value ?? 'connecting')
const endReason = computed(() => session.value?.endReason.value ?? null)
const sessionError = computed(() => session.value?.error.value ?? '')
const actionError = computed(() => session.value?.actionError.value ?? '')

// The toggle into this mode is always live (decision D10), so the gate is a
// panel here rather than a disabled button in the title bar.
async function probe(): Promise<void> {
  checking.value = true
  try {
    const availability = await Available()
    available.value = availability.available
    reason.value = availability.reason
    if (!availability.available) return
    client.value = createTerminalClient(await getTerminalEndpoint())
    await reloadSessions()
  } catch (e) {
    available.value = false
    reason.value = appErrorMessage(e) || (e instanceof Error && e.message) || 'The terminal is unavailable.'
  } finally {
    checking.value = false
  }
}

function openSession(slug: string): void {
  if (!client.value) return
  // Re-clicking the attached row is a no-op — unless the session ended, where
  // the row is as valid a way back in as the overlay's Reconnect.
  if (slug === activeSlug.value && session.value && session.value.status.value !== 'ended') return
  session.value?.dispose()
  activeSlug.value = slug
  renamingId.value = ''
  const opened = useTerminalWindows(slug, client.value)
  session.value = opened
  void opened.start()
}

function closeSession(): void {
  session.value?.dispose()
  session.value = null
  activeSlug.value = ''
  renamingId.value = ''
  void reloadSessions()
}

function onTabMount(windowId: string, host: HTMLElement): void {
  session.value?.attachTab(windowId, host)
}

function startRename(tab: TerminalWindowTab): void {
  renamingId.value = tab.windowId
  renameDraft.value = tab.name
}

function commitRename(): void {
  const windowId = renamingId.value
  if (!windowId) return
  renamingId.value = ''
  const name = renameDraft.value.trim()
  if (name) void session.value?.rename(windowId, name)
}

onMounted(() => {
  void probe()
  prefetchNewSession()
})
onBeforeUnmount(() => session.value?.dispose())
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col bg-app" data-testid="terminal-mode">
    <div v-if="checking" class="flex flex-1 items-center justify-center font-mono text-xs text-text-4">Checking tmux…</div>

    <div
      v-else-if="!available"
      class="flex flex-1 flex-col items-center justify-center gap-3 px-10 text-center"
      data-testid="terminal-unavailable"
    >
      <IconTerminal class="size-6 text-text-4" />
      <div class="text-[13.5px] font-semibold">Terminal unavailable</div>
      <p class="max-w-[420px] text-xs leading-relaxed text-text-3" data-testid="terminal-unavailable-reason">
        {{ reason || 'The terminal is not available in this build.' }}
      </p>
      <button
        type="button"
        class="mt-1 cursor-pointer rounded border border-strong px-3 py-1.5 text-xs text-text-2 hover:text-text"
        data-testid="terminal-retry"
        @click="probe"
      >Try again</button>
    </div>

    <div v-else class="flex min-h-0 min-w-0 flex-1">
      <!-- The sidebar is persistent, like the TUI's session tree: repos as
           group headers, sessions under them, and the attached session's tmux
           windows nested beneath it. Only the attached session can expand —
           windows are only known through a live attach (lazy, by design). -->
      <aside
        class="relative flex shrink-0 flex-col border-r border-border bg-sidebar"
        :style="{ width: sidebarWidth + 'px' }"
        data-testid="terminal-session-sidebar"
      >
        <div class="flex h-9 shrink-0 items-center gap-2 border-b border-border px-3">
          <span class="text-[15px] font-semibold">Sessions</span>
          <span v-if="sessionRows.length" class="font-mono text-[12px] text-text-3">{{ sessionRows.length }} · {{ liveCount }} live</span>
          <span class="flex-1" />
          <button
            type="button"
            class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
            data-testid="terminal-sessions-refresh"
            aria-label="Reload sessions"
            @click="reloadSessions"
          ><IconRotateCw class="size-3.5" /></button>
          <button
            type="button"
            class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
            data-testid="terminal-new-session"
            aria-label="New session"
            title="New session"
            @click="openNewSession"
          ><IconPlus class="size-3.5" /></button>
        </div>
        <div class="hive-scroll min-h-0 flex-1 overflow-y-auto pt-3 pb-4">
          <p v-if="sessionsError" class="px-3 py-2 text-xs text-severity-error" data-testid="terminal-sessions-error">{{ sessionsError }}</p>
          <p v-else-if="sessionsLoading && !sessionRows.length" class="px-3 py-2 font-mono text-xs text-text-4">Loading…</p>
          <p v-else-if="!sessionRows.length" class="px-3 py-2 text-xs text-text-3" data-testid="terminal-sessions-empty">
            No active sessions. Start one from the hub and it will appear here.
          </p>
          <div v-for="(group, index) in sessionGroups" :key="group.key" :class="gapAbove(index) && 'mt-1.5'">
            <button
              type="button"
              class="flex h-7 w-full cursor-pointer items-center gap-2 px-3 text-left hover:bg-chip"
              data-testid="terminal-repo-group"
              :data-repo="group.key"
              :aria-expanded="!collapsedRepos.includes(group.key)"
              @click="toggleGroup(group.key)"
            >
              <component :is="collapsedRepos.includes(group.key) ? IconChevronRight : IconChevronDown" class="size-3 shrink-0 text-text-4" />
              <span class="min-w-0 truncate font-mono text-[12.5px] tracking-[.06em]" :class="groupAttached(group) ? 'text-text-2' : 'text-text-3'">{{ group.name }}</span>
              <span class="ml-auto shrink-0 font-mono text-[11.5px]" :class="groupAttached(group) ? 'text-accent' : 'text-text-4'">{{ group.sessions.length }}</span>
            </button>
            <template v-if="!collapsedRepos.includes(group.key)">
              <div v-for="row in group.sessions" :key="row.id">
                <button
                  type="button"
                  class="flex h-[34px] w-full cursor-pointer items-center gap-2 pl-[21px] pr-3 text-left"
                  :class="row.slug === activeSlug ? 'bg-selection font-semibold text-accent shadow-[inset_3px_0_0_var(--color-accent)]' : 'text-text hover:bg-chip'"
                  data-testid="terminal-session-row"
                  :data-slug="row.slug"
                  :data-attached="row.slug === activeSlug"
                  :title="row.slug"
                  @click="openSession(row.slug)"
                >
                  <!-- The wire only carries hive's session state today; agent
                       activity (the TUI's [●]/[>] pair) needs terminal.Status
                       plumbed through SessionSummary first. -->
                  <span
                    class="size-1.5 shrink-0 rounded-full"
                    :class="row.slug === activeSlug ? 'bg-accent [animation:hivePulse_2.4s_ease-in-out_infinite]' : row.state === 'active' ? 'bg-severity-success' : 'bg-text-4'"
                  />
                  <span class="min-w-0 truncate text-[15px]">{{ row.name }}</span>
                </button>
                <!-- The well sits on bg-app — the surface xtermTheme() renders
                     on — so the windows read as part of the terminal they
                     belong to rather than as sidebar chrome. -->
                <div
                  v-if="row.slug === activeSlug && session && tabs.length"
                  class="flex flex-col gap-px bg-app py-2 pl-[13px] pr-1"
                >
                  <button
                    v-for="tab in tabs"
                    :key="tab.uid"
                    type="button"
                    class="flex h-7 w-full cursor-pointer items-center gap-2 px-2 text-left"
                    :class="tab.windowId === activeWindowId ? 'bg-pane' : 'hover:bg-pane'"
                    data-testid="terminal-window-row"
                    :data-window-id="tab.windowId"
                    :data-active="tab.windowId === activeWindowId"
                    @click="session?.select(tab.windowId)"
                  >
                    <span class="shrink-0 font-mono text-[11.5px] leading-none" :class="tab.windowId === activeWindowId ? 'text-accent' : 'text-text-4'">&gt;_</span>
                    <span class="min-w-0 flex-1 truncate font-mono text-[13.5px]" :class="tab.windowId === activeWindowId ? 'text-text' : 'text-text-2'">{{ tab.name || tab.windowId }}</span>
                    <span v-if="tab.windowId === activeWindowId" class="size-[5px] shrink-0 rounded-full bg-severity-success" />
                  </button>
                </div>
              </div>
            </template>
          </div>
        </div>
        <PanelResizeHandle edge="right" name="terminal-sidebar" :start="startResize" :step="step" />
      </aside>

      <div class="flex min-h-0 min-w-0 flex-1 flex-col">
        <div
          v-if="!session"
          class="flex flex-1 flex-col items-center justify-center gap-2 px-10 text-center"
          data-testid="terminal-no-session"
        >
          <IconTerminal class="size-6 text-text-4" />
          <p class="text-xs text-text-3">Select a session to attach.</p>
        </div>

        <template v-else>
          <div class="flex h-9 shrink-0 items-stretch border-b border-border bg-raised">
            <div class="hive-scroll flex min-w-0 items-stretch overflow-x-auto">
              <div
                v-for="tab in tabs"
                :key="tab.uid"
                class="flex w-[180px] shrink-0 items-center gap-2 border-r border-border px-3"
                :class="tab.windowId === activeWindowId ? 'bg-app shadow-[inset_0_1px_0_var(--color-accent)]' : 'hover:bg-chip'"
                data-testid="terminal-tab"
                :data-window-id="tab.windowId"
                :data-active="tab.windowId === activeWindowId"
              >
                <span
                  class="size-1.5 shrink-0 rounded-full"
                  :class="tab.windowId === activeWindowId ? 'bg-severity-success [animation:hivePulse_2.4s_ease-in-out_infinite]' : 'bg-hover'"
                />
                <input
                  v-if="renamingId === tab.windowId"
                  v-model="renameDraft"
                  class="min-w-0 flex-1 bg-transparent font-mono text-[12.5px] text-text outline-none"
                  data-testid="terminal-rename-input"
                  autofocus
                  @keydown.enter="commitRename"
                  @keydown.esc="renamingId = ''"
                  @blur="commitRename"
                >
                <button
                  v-else
                  type="button"
                  class="min-w-0 flex-1 cursor-pointer truncate text-left font-mono text-[12.5px]"
                  :class="tab.windowId === activeWindowId ? 'font-medium text-text' : 'text-text-2'"
                  @click="session?.select(tab.windowId)"
                  @dblclick="startRename(tab)"
                >{{ tab.name || tab.windowId }}</button>
                <button
                  type="button"
                  class="flex size-4 shrink-0 cursor-pointer items-center justify-center rounded text-text-4 hover:bg-chip hover:text-text"
                  data-testid="terminal-close-window"
                  :aria-label="`Close ${tab.name || tab.windowId}`"
                  @click="session?.closeWindow(tab.windowId)"
                ><IconX class="size-3" /></button>
              </div>
              <button
                type="button"
                class="flex w-9 shrink-0 cursor-pointer items-center justify-center border-r border-border text-text-3 hover:bg-chip hover:text-text"
                data-testid="terminal-new-window"
                aria-label="New window"
                title="New window"
                @click="session?.newWindow()"
              ><IconPlus class="size-3.5" /></button>
            </div>
          </div>

          <p v-if="actionError" class="shrink-0 border-b border-border px-3 py-1.5 text-[11.5px] text-severity-error" data-testid="terminal-action-error">{{ actionError }}</p>

          <div class="relative flex min-h-0 min-w-0 flex-1 flex-col">
            <TerminalTab
              v-for="tab in tabs"
              :key="tab.uid"
              :tab="tab"
              :active="tab.windowId === activeWindowId"
              @mount="onTabMount"
            />
            <div v-if="!tabs.length && status !== 'ended'" class="flex flex-1 items-center justify-center font-mono text-xs text-text-4">Attaching…</div>

            <div
              v-if="status === 'ended'"
              class="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-app/95 px-10 text-center"
              data-testid="terminal-session-ended"
            >
              <div class="text-[13.5px] font-semibold">
                {{ endReason === 'disconnected' ? 'Terminal stream lost' : 'Session ended' }}
              </div>
              <p class="max-w-[420px] text-xs leading-relaxed text-text-3" data-testid="terminal-session-ended-reason">
                {{ sessionError }}
              </p>
              <div class="mt-1 flex items-center gap-2">
                <button
                  type="button"
                  class="flex cursor-pointer items-center gap-1.5 rounded border border-strong px-3 py-1.5 text-xs text-text-2 hover:text-text"
                  data-testid="terminal-reconnect"
                  @click="session?.reconnect()"
                ><IconRefreshCw class="size-3" />Reconnect</button>
                <button
                  type="button"
                  class="cursor-pointer rounded border border-strong px-3 py-1.5 text-xs text-text-2 hover:text-text"
                  data-testid="terminal-close-session"
                  @click="closeSession"
                >Close session</button>
              </div>
            </div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
