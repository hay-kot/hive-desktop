<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import IconArrowLeft from '~icons/lucide/arrow-left'
import IconPlus from '~icons/lucide/plus'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import IconRotateCw from '~icons/lucide/rotate-cw'
import IconTerminal from '~icons/lucide/terminal'
import IconX from '~icons/lucide/x'
import TerminalTab from './TerminalTab.vue'
import { useTerminalSessions } from '../composables/useTerminalSessions'
import { useTerminalWindows, type TerminalWindowTab, type UseTerminalWindows } from '../composables/useTerminalWindows'
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
  session.value?.dispose()
  activeSlug.value = slug
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

onMounted(() => { void probe() })
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

    <div v-else-if="!session" class="flex min-h-0 flex-1 flex-col" data-testid="terminal-session-picker">
      <div class="flex shrink-0 items-center gap-2 border-b border-border px-4 py-2.5">
        <span class="text-[12.5px] font-semibold">Sessions</span>
        <span class="flex-1" />
        <button
          type="button"
          class="flex size-7 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
          data-testid="terminal-sessions-refresh"
          aria-label="Reload sessions"
          @click="reloadSessions"
        ><IconRotateCw class="size-3.5" /></button>
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto p-3">
        <p v-if="sessionsError" class="px-1 py-2 text-xs text-severity-error" data-testid="terminal-sessions-error">{{ sessionsError }}</p>
        <p v-else-if="sessionsLoading && !sessionRows.length" class="px-1 py-2 font-mono text-xs text-text-4">Loading…</p>
        <p v-else-if="!sessionRows.length" class="px-1 py-2 text-xs text-text-3" data-testid="terminal-sessions-empty">
          No active sessions. Start one from the hub and it will appear here.
        </p>
        <button
          v-for="row in sessionRows"
          :key="row.id"
          type="button"
          class="flex w-full cursor-pointer flex-col items-start gap-0.5 rounded-md border border-transparent px-3 py-2 text-left hover:border-card hover:bg-chip"
          data-testid="terminal-session-row"
          :data-slug="row.slug"
          @click="openSession(row.slug)"
        >
          <span class="text-[12.5px] font-medium">{{ row.name }}</span>
          <span class="font-mono text-[11px] text-text-4">{{ row.repo }} · {{ row.slug }} · {{ row.state }}</span>
        </button>
      </div>
    </div>

    <template v-else>
      <div class="flex shrink-0 items-center gap-1 border-b border-border bg-raised px-2 py-1.5">
        <button
          type="button"
          class="flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
          data-testid="terminal-back"
          aria-label="Back to sessions"
          @click="closeSession"
        ><IconArrowLeft class="size-3.5" /></button>
        <span class="mx-0.5 h-[18px] w-px shrink-0 bg-border" />
        <div class="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          <div
            v-for="tab in tabs"
            :key="tab.uid"
            class="flex shrink-0 items-center gap-1 rounded-[7px] pl-2 pr-1"
            :class="tab.windowId === activeWindowId ? 'bg-accent-tint text-accent' : 'text-text-3 hover:bg-chip hover:text-text'"
            data-testid="terminal-tab"
            :data-window-id="tab.windowId"
            :data-active="tab.windowId === activeWindowId"
          >
            <input
              v-if="renamingId === tab.windowId"
              v-model="renameDraft"
              class="w-24 bg-transparent py-1 text-[11.5px] outline-none"
              data-testid="terminal-rename-input"
              autofocus
              @keydown.enter="commitRename"
              @keydown.esc="renamingId = ''"
              @blur="commitRename"
            >
            <button
              v-else
              type="button"
              class="cursor-pointer py-1 text-[11.5px]"
              @click="session?.select(tab.windowId)"
              @dblclick="startRename(tab)"
            >{{ tab.name || tab.windowId }}</button>
            <button
              type="button"
              class="flex size-4 cursor-pointer items-center justify-center rounded text-text-4 hover:bg-chip hover:text-text"
              data-testid="terminal-close-window"
              :aria-label="`Close ${tab.name || tab.windowId}`"
              @click="session?.closeWindow(tab.windowId)"
            ><IconX class="size-3" /></button>
          </div>
        </div>
        <button
          type="button"
          class="flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
          data-testid="terminal-new-window"
          aria-label="New window"
          title="New window"
          @click="session?.newWindow()"
        ><IconPlus class="size-3.5" /></button>
        <span class="ml-1 shrink-0 font-mono text-[11px] text-text-4">{{ activeSlug }}</span>
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
              data-testid="terminal-back-to-sessions"
              @click="closeSession"
            >Back to sessions</button>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>
