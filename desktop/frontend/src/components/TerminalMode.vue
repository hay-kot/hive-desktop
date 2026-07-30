<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useStorage } from '@vueuse/core'
import IconArrowDown from '~icons/lucide/arrow-down'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconChevronRight from '~icons/lucide/chevron-right'
import IconEllipsis from '~icons/lucide/ellipsis'
import IconInfo from '~icons/lucide/info'
import IconPlus from '~icons/lucide/plus'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import IconRotateCw from '~icons/lucide/rotate-cw'
import IconTerminal from '~icons/lucide/terminal'
import IconTrash from '~icons/lucide/trash-2'
import IconX from '~icons/lucide/x'
import AppMenu from './AppMenu.vue'
import ConfirmationDialog from './ConfirmationDialog.vue'
import PanelResizeHandle from './PanelResizeHandle.vue'
import SessionDetailDialog from './SessionDetailDialog.vue'
import SessionRenameDialog from './SessionRenameDialog.vue'
import SessionRowMenu from './SessionRowMenu.vue'
import TerminalTab from './TerminalTab.vue'
import { useTerminalAvailability } from '../composables/useTerminalAvailability'
import { groupTerminalSessions, useTerminalSessions, type TerminalSessionGroup, type TerminalSessionRow } from '../composables/useTerminalSessions'
import { useTerminalShowWindows } from '../composables/useTerminalShowWindows'
import { useTerminalWindowListings } from '../composables/useTerminalWindowListings'
import { useTerminalWindows, type TerminalWindowTab, type UseTerminalWindows } from '../composables/useTerminalWindows'
import { useNewSession } from '../composables/useNewSession'
import { useResizablePanel } from '../composables/useResizablePanel'
import { useSessionActions } from '../composables/useSessionActions'
import { useWailsEvent } from '../composables/useWailsEvent'
import { createTerminalClient, getTerminalEndpoint, type WindowState } from '../lib/terminalClient'
import { appErrorMessage } from '../lib/appError'
import { Available } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice'
import type { MenuEntry } from '../types/menu'
import '@xterm/xterm/css/xterm.css'

const { checking, available, reason, client } = useTerminalAvailability()
const session = shallowRef<UseTerminalWindows | null>(null)
const activeSlug = ref('')
const renamingId = ref('')
const renameDraft = ref('')

const route = useRoute()
const router = useRouter()

// The URL is the attach state: /terminal/:slug is the attached session,
// ?window its active window. Sidebar clicks push (history traverses session
// switches); window changes replace (tab flips must not pile up entries).
const routeSlug = computed(() => (route.name === 'terminal' && typeof route.params.slug === 'string' ? route.params.slug : ''))
const routeWindow = computed(() => (typeof route.query.window === 'string' ? route.query.window : ''))

// The resume snapshot: entering bare /terminal re-attaches this instead of
// landing on the picker. Cleared when the session is closed on purpose or no
// longer exists.
const restore = useStorage('hive.terminal.restore', { slug: '', window: '' })

const {
  sessions: sessionRows, loading: sessionsLoading, error: sessionsError, reload: reloadSessions,
} = useTerminalSessions()
const { openBlank: openNewSession, prefetch: prefetchNewSession } = useNewSession()
// The tree is the attach surface, so only an active session belongs in it — a
// recycled or corrupted one has no tmux session behind it. They still arrive in
// the listing, which is what the header's prune entry counts and acts on.
const attachable = computed(() => sessionRows.value.filter((row) => row.state === 'active'))
const sessionGroups = computed(() => groupTerminalSessions(attachable.value))
const prunableCount = computed(() => sessionRows.value.length - attachable.value.length)

const openRowMenu = ref('')
const rowMenuFlip = ref(false)
const rowMenuToggles = new Map<string, HTMLElement>()
const sidebarMenuOpen = ref(false)
const sidebarMenuToggle = ref<HTMLElement | null>(null)
const sidebarMenuEntries = computed<MenuEntry[]>(() => [{
  kind: 'action',
  id: 'prune',
  label: prunableCount.value ? `Prune ${prunableCount.value} recycled…` : 'Nothing to prune',
  icon: IconTrash,
  testid: 'terminal-sessions-prune',
}])

const {
  confirmation,
  detail: sessionDetail,
  openDetail: openSessionDetail,
  closeDetail: closeSessionDetail,
  renaming, renameBusy, renameError,
  requestRename, cancelRename, submitRename, requestDelete, requestRecycle, requestPrune,
} = useSessionActions({ onChanged: () => { void reloadSessions() } })
const {
  open: confirmOpen, options: confirmOptions, busy: confirmBusy, error: confirmError,
  cancel: cancelConfirm, confirm: runConfirm,
} = confirmation

function setRowMenuToggle(id: string, el: unknown): void {
  if (el instanceof HTMLElement) rowMenuToggles.set(id, el)
  else rowMenuToggles.delete(id)
}

// The sidebar is a scroll container, so an overflowing menu is clipped rather
// than allowed to hang outside it: open upward near the bottom of the window.
function toggleRowMenu(row: TerminalSessionRow, event?: MouseEvent): void {
  if (openRowMenu.value === row.id && !event) {
    openRowMenu.value = ''
    return
  }
  const rect = (event?.currentTarget instanceof HTMLElement ? event.currentTarget : rowMenuToggles.get(row.id))?.getBoundingClientRect()
  rowMenuFlip.value = rect != null && window.innerHeight - rect.bottom < 200 && rect.top > 200
  openRowMenu.value = row.id
}

function onSidebarMenuSelect(id: string): void {
  sidebarMenuOpen.value = false
  if (id === 'prune' && prunableCount.value) requestPrune(prunableCount.value)
}

function groupAttached(group: TerminalSessionGroup): boolean {
  return group.sessions.some((row) => row.slug === activeSlug.value)
}

// Expand/collapse is transient view state, not configuration — localStorage,
// same as the hub sidebar's folder collapse.
const collapsedRepos = useStorage<string[]>('hive.terminal.sidebar.collapsed', [])
function toggleGroup(key: string): void {
  collapsedRepos.value = collapsedRepos.value.includes(key)
    ? collapsedRepos.value.filter((other) => other !== key)
    : [...collapsedRepos.value, key]
}

// Windows are only known live through an attach, so every other active
// session's come from a one-shot listing per session — fetched only while the
// Settings ▸ Appearance ▸ Terminal option is on, and refreshed whenever the
// session list or the attached slug changes.
const { showWindows: showAllWindows } = useTerminalShowWindows()
const { listings: sessionWindows, refresh: refreshListings } = useTerminalWindowListings()
watch([showAllWindows, attachable, activeSlug, client], () => {
  const transport = client.value
  if (!showAllWindows.value || !transport) return
  void refreshListings(transport, attachable.value)
})

// The attached session's live tab set is fresher than its listing — but while
// the attach is still in flight, the cached listing stands in so selecting a
// session does not collapse its subtree.
function listedWindows(row: TerminalSessionRow): WindowState[] {
  if (!showAllWindows.value) return []
  if (row.slug === activeSlug.value && status.value !== 'connecting') return []
  return sessionWindows.value[row.slug] ?? []
}

function openWindow(row: TerminalSessionRow, windowId: string): void {
  void router.push({ name: 'terminal', params: { slug: row.slug }, query: { window: windowId } })
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
const activeScrolledUp = computed(() => tabs.value.some((tab) => tab.windowId === activeWindowId.value && tab.scrolledUp))
const status = computed(() => session.value?.status.value ?? 'connecting')
// The cached listing stands in for the tab strip while the attach is in
// flight, so selecting a session swaps the strip's contents in place instead
// of emptying and rebuilding it.
const placeholderTabs = computed<WindowState[]>(() =>
  (status.value === 'connecting' && !tabs.value.length ? sessionWindows.value[activeSlug.value] ?? [] : []))
const endReason = computed(() => session.value?.endReason.value ?? null)
const sessionError = computed(() => session.value?.error.value ?? '')
const actionError = computed(() => session.value?.actionError.value ?? '')
const sizeConstraint = computed(() => session.value?.sizeConstraint.value ?? null)

// The toggle into this mode is always live, so the gate is a panel here
// rather than a disabled button in the title bar. The gate only shows on the
// first visit: with a cached client the last-known tree renders immediately,
// the resume and the listing revalidation start at once, and the probe below
// only re-checks availability.
async function probe(): Promise<void> {
  if (client.value) {
    if (attachable.value.length) restoreLastSession()
    void reloadSessions().then(restoreLastSession)
  } else {
    checking.value = true
  }
  try {
    const availability = await Available()
    available.value = availability.available
    reason.value = availability.reason
    if (!availability.available) return
    if (!client.value) {
      client.value = createTerminalClient(await getTerminalEndpoint())
      await reloadSessions()
      restoreLastSession()
    }
  } catch (e) {
    available.value = false
    reason.value = appErrorMessage(e) || (e instanceof Error && e.message) || 'The terminal is unavailable.'
  } finally {
    checking.value = false
  }
}

// A remembered session that no longer exists — or that has since been recycled,
// which leaves no tmux session to attach to — is forgotten rather than attached
// blind; the picker shows, same as a first visit.
function restoreLastSession(): void {
  if (routeSlug.value || !restore.value.slug) return
  if (!attachable.value.some((row) => row.slug === restore.value.slug)) {
    restore.value = { slug: '', window: '' }
    return
  }
  void router.replace({
    name: 'terminal',
    params: { slug: restore.value.slug },
    query: restore.value.window ? { window: restore.value.window } : {},
  })
}

// The route is what attaches: rows and restores only navigate, and this
// watcher is the single path into openSession, so back/forward re-attach
// exactly like a click. Immediate because the cached client can already be
// live at mount, in which case a deep-linked slug fires no change at all.
watch([client, routeSlug], ([ready, slug]) => {
  if (!ready) return
  if (slug) openSession(slug)
  else detachSession()
}, { immediate: true })

// Mirror the attached window into the URL and the resume snapshot. Guarded to
// the live route so a navigation away cannot claw the history entry back.
watch([activeSlug, () => session.value?.activeWindowId.value ?? ''], ([slug, windowId]) => {
  if (!slug || route.name !== 'terminal' || route.params.slug !== slug) return
  restore.value = { slug, window: windowId }
  if (windowId && routeWindow.value !== windowId) {
    void router.replace({ name: 'terminal', params: { slug }, query: { window: windowId } })
  }
})

// The attached slug can stop being attachable two ways, and a list reload is how
// we find out about either: the session was deleted or recycled (both run as
// jobs, and a recycled session leaves the attachable set), or it was renamed —
// hive re-slugs on rename, so the same session reappears under a new slug.
// Following the id is what tells the two apart, and it works whether the change
// came from this window or from the hive CLI.
const attachedId = ref('')
watch([attachable, activeSlug], ([rows, slug]) => {
  if (!slug || sessionsError.value) return
  const attached = rows.find((row) => row.slug === slug)
  if (attached) {
    attachedId.value = attached.id
    return
  }
  // Attached before the list ever loaded, so which session this is was never
  // learned; the attach reports its own failure rather than being guessed at.
  if (!attachedId.value) return
  const renamed = rows.find((row) => row.id === attachedId.value)
  if (!renamed) {
    closeSession()
    return
  }
  // The slug is the tmux target, so the attach follows the rename.
  restore.value = { slug: renamed.slug, window: '' }
  void router.replace({ name: 'terminal', params: { slug: renamed.slug } })
})

function selectSession(slug: string): void {
  if (slug === activeSlug.value) {
    // Same URL, so the route watcher stays silent — but after the session
    // ended the row is as valid a way back in as the overlay's Reconnect,
    // and reselecting a live one is an intent to type into it.
    if (session.value?.status.value === 'ended') openSession(slug)
    else session.value?.focusActive()
    return
  }
  void router.push({ name: 'terminal', params: { slug } })
}

function openSession(slug: string): void {
  if (!client.value) return
  if (slug === activeSlug.value && session.value && session.value.status.value !== 'ended') return
  session.value?.dispose()
  activeSlug.value = slug
  renamingId.value = ''
  const opened = useTerminalWindows(slug, client.value)
  session.value = opened
  // Captured before attach: the mirror watcher rewrites ?window to tmux's
  // active the moment windows land, and the wanted one must survive that.
  const wanted = routeWindow.value
  void opened.start().then(() => {
    if (session.value !== opened || !wanted) return
    // A window that no longer exists falls through to tmux's own active.
    if (opened.tabs.value.some((tab) => tab.windowId === wanted)) void opened.select(wanted)
  })
}

function detachSession(): void {
  session.value?.dispose()
  session.value = null
  activeSlug.value = ''
  renamingId.value = ''
}

function closeSession(): void {
  detachSession()
  restore.value = { slug: '', window: '' }
  void reloadSessions()
  // Replace, not push: the closed session's entry points at a session that is
  // gone, so Back must not walk into it.
  if (routeSlug.value) void router.replace({ name: 'terminal' })
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
           group headers, sessions under them, and tmux windows nested beneath.
           The attached session's windows are its live tab set; the rest render
           only with "Always show windows" on, from a one-shot listing. -->
      <aside
        class="relative flex shrink-0 flex-col border-r border-border bg-sidebar"
        :style="{ width: sidebarWidth + 'px' }"
        data-testid="terminal-session-sidebar"
      >
        <div class="flex h-9 shrink-0 items-center gap-2 border-b border-border px-3">
          <span class="text-[15px] font-semibold">Sessions</span>
          <span v-if="attachable.length" class="font-mono text-[12px] text-text-3">{{ attachable.length }}</span>
          <span class="flex-1" />
          <!-- The refresh control doubles as the staleness indicator: the
               cached tree renders instantly, and the spin is what says a
               revalidation is still in flight. -->
          <button
            type="button"
            class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text disabled:cursor-default"
            data-testid="terminal-sessions-refresh"
            aria-label="Reload sessions"
            :aria-busy="sessionsLoading"
            :disabled="sessionsLoading"
            @click="reloadSessions"
          ><IconRotateCw class="size-3.5" :class="{ 'animate-spin': sessionsLoading }" /></button>
          <button
            type="button"
            class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
            data-testid="terminal-new-session"
            aria-label="New session"
            title="New session"
            @click="openNewSession"
          ><IconPlus class="size-3.5" /></button>
          <!-- List-wide operations; a session's own live on its row. -->
          <div class="relative flex">
            <button
              ref="sidebarMenuToggle"
              type="button"
              class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
              data-testid="terminal-sessions-menu-toggle"
              aria-label="Session list actions"
              aria-haspopup="menu"
              :aria-expanded="sidebarMenuOpen"
              @click="sidebarMenuOpen = !sidebarMenuOpen"
            ><IconEllipsis class="size-3.5" /></button>
            <AppMenu
              v-if="sidebarMenuOpen"
              :entries="sidebarMenuEntries"
              :ignore="[sidebarMenuToggle]"
              testid="terminal-sessions-menu"
              @close="sidebarMenuOpen = false"
              @select="onSidebarMenuSelect"
            />
          </div>
        </div>
        <div class="hive-scroll min-h-0 flex-1 overflow-y-auto pb-4">
          <p v-if="sessionsError" class="px-3 py-2 text-xs text-severity-error" data-testid="terminal-sessions-error">{{ sessionsError }}</p>
          <p v-else-if="sessionsLoading && !attachable.length" class="px-3 py-2 font-mono text-xs text-text-4">Loading…</p>
          <p v-else-if="!attachable.length" class="px-3 py-2 text-xs text-text-3" data-testid="terminal-sessions-empty">
            No active sessions. Start one from the hub and it will appear here.
          </p>
          <!-- One repository reads as one block: the header keeps the sidebar's
               own surface and its sessions sit in a recessed panel under it, so
               a long run of sessions cannot bleed into the next repo's. -->
          <div v-for="group in sessionGroups" :key="group.key" class="border-t border-border first:border-t-0">
            <button
              type="button"
              class="flex h-9 w-full cursor-pointer items-center gap-2 px-3 text-left hover:bg-chip"
              data-testid="terminal-repo-group"
              :data-repo="group.key"
              :aria-expanded="!collapsedRepos.includes(group.key)"
              @click="toggleGroup(group.key)"
            >
              <span class="min-w-0 truncate font-mono text-[13.5px] font-semibold tracking-[.02em] text-text">{{ group.name }}</span>
              <span class="ml-auto shrink-0 font-mono text-[11.5px]" :class="groupAttached(group) ? 'text-accent' : 'text-text-4'">{{ group.sessions.length }}</span>
              <component :is="collapsedRepos.includes(group.key) ? IconChevronRight : IconChevronDown" class="size-3 shrink-0 text-text-4" />
            </button>
            <div v-if="!collapsedRepos.includes(group.key)" class="flex flex-col border-t border-border bg-app py-1">
              <div v-for="row in group.sessions" :key="row.id">
                <!-- Not a <button>: the row's menu toggle is a real button, and
                     nesting one inside another is invalid. -->
                <div
                  class="session-row"
                  :class="{ 'session-row-attached': row.slug === activeSlug, 'menu-open': openRowMenu === row.id }"
                  role="button"
                  tabindex="0"
                  data-testid="terminal-session-row"
                  :data-slug="row.slug"
                  :data-attached="row.slug === activeSlug"
                  :title="row.slug"
                  @click="selectSession(row.slug)"
                  @keydown.enter.self.prevent="selectSession(row.slug)"
                  @keydown.space.self.prevent="selectSession(row.slug)"
                  @contextmenu.prevent="toggleRowMenu(row, $event)"
                >
                  <span class="min-w-0 flex-1 truncate text-[13.5px]">{{ row.name }}</span>
                  <!-- No `relative` here: AppMenu anchors to the nearest
                       positioned ancestor, and that has to be the row so the
                       panel spans it. Clicks stay inside the wrapper so choosing
                       an entry never also selects the row. -->
                  <div class="flex shrink-0" @click.stop>
                    <button
                      :ref="(el) => setRowMenuToggle(row.id, el)"
                      type="button"
                      class="row-action"
                      title="Session actions"
                      aria-label="Session actions"
                      aria-haspopup="menu"
                      :aria-expanded="openRowMenu === row.id"
                      data-testid="terminal-session-menu-toggle"
                      @click="toggleRowMenu(row)"
                    ><IconEllipsis class="size-3" /></button>
                    <SessionRowMenu
                      v-if="openRowMenu === row.id"
                      :session="row"
                      :flip="rowMenuFlip"
                      :ignore="[rowMenuToggles.get(row.id) ?? null]"
                      @close="openRowMenu = ''"
                      @detail="openSessionDetail(row)"
                      @rename="requestRename(row)"
                      @recycle="requestRecycle(row)"
                      @delete="requestDelete(row)"
                    />
                  </div>
                </div>
                <div v-if="row.slug === activeSlug && session && tabs.length" class="flex flex-col pb-1">
                  <button
                    v-for="(tab, index) in tabs"
                    :key="tab.uid"
                    type="button"
                    class="window-row"
                    :class="{ 'window-row-last': index === tabs.length - 1, 'window-row-active': tab.windowId === activeWindowId }"
                    data-testid="terminal-window-row"
                    :data-window-id="tab.windowId"
                    :data-active="tab.windowId === activeWindowId"
                    @click="session?.select(tab.windowId)"
                  >
                    <span class="min-w-0 flex-1 truncate font-mono text-[12.5px]">{{ tab.name || tab.windowId }}</span>
                  </button>
                </div>
                <div v-else-if="listedWindows(row).length" class="flex flex-col pb-1">
                  <button
                    v-for="(win, index) in listedWindows(row)"
                    :key="win.windowId"
                    type="button"
                    class="window-row"
                    :class="{ 'window-row-last': index === listedWindows(row).length - 1 }"
                    data-testid="terminal-listed-window-row"
                    :data-window-id="win.windowId"
                    @click="openWindow(row, win.windowId)"
                  >
                    <span class="min-w-0 flex-1 truncate font-mono text-[12.5px]">{{ win.name || win.windowId }}</span>
                  </button>
                </div>
              </div>
            </div>
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
                class="flex w-[150px] shrink-0 items-center gap-2 border-r border-border px-3"
                :class="tab.windowId === activeWindowId ? 'bg-app shadow-[inset_0_1px_0_var(--color-accent)]' : 'hover:bg-chip'"
                data-testid="terminal-tab"
                :data-window-id="tab.windowId"
                :data-active="tab.windowId === activeWindowId"
              >
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
              <!-- Inert stand-ins from the cached listing while the attach is
                   in flight; the live tabs replace them in place. -->
              <div
                v-for="win in placeholderTabs"
                :key="win.windowId"
                class="flex w-[150px] shrink-0 items-center gap-2 border-r border-border px-3"
                :class="win.active ? 'bg-app shadow-[inset_0_1px_0_var(--color-accent)]' : ''"
                data-testid="terminal-placeholder-tab"
                :data-window-id="win.windowId"
              >
                <span class="min-w-0 flex-1 truncate font-mono text-[12.5px]" :class="win.active ? 'font-medium text-text' : 'text-text-2'">{{ win.name || win.windowId }}</span>
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

          <!-- Names tmux's rule rather than reporting a fault: the grid is
               smaller (or larger) than the pane because another client attached
               to this session is the one deciding its size. -->
          <div
            v-if="sizeConstraint"
            class="flex shrink-0 items-start gap-2 border-b border-border bg-raised px-3 py-2"
            data-testid="terminal-size-constraint"
          >
            <IconInfo class="mt-px size-3.5 shrink-0 text-text-4" />
            <p class="min-w-0 flex-1 text-[11.5px] leading-relaxed text-text-3">
              tmux is drawing this window at
              <span class="font-mono text-text-2">{{ sizeConstraint.granted.cols }}×{{ sizeConstraint.granted.rows }}</span>,
              not the <span class="font-mono text-text-2">{{ sizeConstraint.voted.cols }}×{{ sizeConstraint.voted.rows }}</span>
              this pane fits. Every client attached to a session shares one grid per window, so another attached
              client is deciding the size. Detach it, or change tmux's
              <span class="font-mono text-text-2">window-size</span> option, to use the whole pane.
            </p>
            <button
              type="button"
              class="flex size-4 shrink-0 cursor-pointer items-center justify-center rounded text-text-4 hover:bg-chip hover:text-text"
              data-testid="terminal-size-constraint-dismiss"
              aria-label="Dismiss"
              @click="session?.dismissSizeConstraint()"
            ><IconX class="size-3" /></button>
          </div>

          <div class="relative flex min-h-0 min-w-0 flex-1 flex-col">
            <TerminalTab
              v-for="tab in tabs"
              :key="tab.uid"
              :tab="tab"
              :active="tab.windowId === activeWindowId"
              @mount="onTabMount"
            />
            <div v-if="!tabs.length && status !== 'ended'" class="flex flex-1 items-center justify-center font-mono text-xs text-text-4">Attaching…</div>

            <!-- New output keeps landing below the fold while the viewport is
                 scrolled up; this is the way back to the live tail. -->
            <button
              v-if="activeScrolledUp && status !== 'ended'"
              type="button"
              class="absolute bottom-3 right-5 z-10 flex cursor-pointer items-center gap-1.5 rounded-full border border-strong bg-raised/95 px-3 py-1.5 text-[11.5px] text-text-2 shadow-lg hover:text-text"
              data-testid="terminal-scroll-to-bottom"
              @click="session?.scrollToBottom()"
            ><IconArrowDown class="size-3" />Scroll to bottom</button>

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

    <SessionDetailDialog v-if="sessionDetail" :detail="sessionDetail" @close="closeSessionDetail" />
    <SessionRenameDialog
      v-if="renaming"
      :name="renaming.name"
      :busy="renameBusy"
      :error="renameError"
      @close="cancelRename"
      @save="submitRename"
    />
    <ConfirmationDialog
      v-if="confirmOpen && confirmOptions"
      :title="confirmOptions.title"
      :description="confirmOptions.description"
      :confirm-label="confirmOptions.confirmLabel"
      :busy="confirmBusy"
      :error="confirmError"
      testid="session-confirmation"
      @confirm="runConfirm"
      @cancel="cancelConfirm"
    />
  </div>
</template>

<style scoped>
.session-row { position: relative; display: flex; height: 30px; width: 100%; align-items: center; gap: 8px; padding-left: 20px; padding-right: 12px; text-align: left; color: var(--color-text); cursor: pointer; }
.session-row:hover, .session-row.menu-open { background: var(--color-chip); }
.session-row:focus-visible { outline: 2px solid var(--color-accent); outline-offset: -2px; }
/* No fill: the rail and the accent are enough to find the attached row, and
   leaving the surface alone also lets it keep its hover feedback. */
.session-row-attached { font-weight: 500; color: var(--color-accent); box-shadow: inset 2px 0 0 var(--color-accent); }
/* The tree connector is drawn, not typed: a box-drawing glyph is only as tall as
   its font size, so stacked rows would show a gap where the TUI's cell grid
   shows an unbroken line. ::before is the vertical, stopped at the elbow on the
   last row; ::after is the tick into the name. */
.window-row { position: relative; display: flex; height: 28px; width: 100%; align-items: center; padding-left: 40px; padding-right: 12px; text-align: left; color: var(--color-text-2); cursor: pointer; }
.window-row:hover { background: var(--color-chip); }
.window-row:focus-visible { outline: 2px solid var(--color-accent); outline-offset: -2px; }
.window-row-active { font-weight: 500; color: var(--color-accent); box-shadow: inset 2px 0 0 var(--color-accent); }
.window-row::before { content: ''; position: absolute; left: 26px; top: 0; bottom: 0; border-left: 1px solid var(--color-strong); }
.window-row::after { content: ''; position: absolute; left: 26px; top: 50%; width: 9px; border-top: 1px solid var(--color-strong); }
.window-row-last::before { bottom: 50%; }

/* Revealed by opacity so the kebab's column is always reserved — hovering a row
   never reflows the session name. Same affordance as the hub sidebar's rows. */
.row-action { display: inline-flex; align-items: center; justify-content: center; width: 18px; height: 18px; border-radius: 5px; color: var(--color-text-4); cursor: pointer; opacity: 0; }
.row-action:hover, .row-action[aria-expanded="true"] { background: var(--color-app); color: var(--color-text); }
.session-row:hover .row-action, .row-action:focus-visible, .session-row.menu-open .row-action { opacity: 1; }
</style>
