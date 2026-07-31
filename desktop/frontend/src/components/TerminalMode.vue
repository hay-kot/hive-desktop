<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowReactive, shallowRef, watch, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useStorage } from '@vueuse/core'
import IconArrowDown from '~icons/lucide/arrow-down'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconChevronRight from '~icons/lucide/chevron-right'
import IconCircle from '~icons/lucide/circle'
import IconCircleAlert from '~icons/lucide/circle-alert'
import IconCircleCheck from '~icons/lucide/circle-check'
import IconCircleOff from '~icons/lucide/circle-off'
import IconEllipsis from '~icons/lucide/ellipsis'
import IconEllipsisVertical from '~icons/lucide/ellipsis-vertical'
import IconInfo from '~icons/lucide/info'
import IconLoaderCircle from '~icons/lucide/loader-circle'
import IconPlay from '~icons/lucide/play'
import IconPlus from '~icons/lucide/plus'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import IconRotateCw from '~icons/lucide/rotate-cw'
import IconTerminal from '~icons/lucide/terminal'
import IconTrash from '~icons/lucide/trash-2'
import IconX from '~icons/lucide/x'
import AppMenu from './AppMenu.vue'
import BaseButton from './BaseButton.vue'
import ConfirmationDialog from './ConfirmationDialog.vue'
import PanelResizeHandle from './PanelResizeHandle.vue'
import SessionDetailDialog from './SessionDetailDialog.vue'
import SessionRenameDialog from './SessionRenameDialog.vue'
import SessionRowMenu from './SessionRowMenu.vue'
import TerminalTab from './TerminalTab.vue'
import { useTerminalAvailability } from '../composables/useTerminalAvailability'
import { groupTerminalSessions, useTerminalSessions, type TerminalSessionGroup, type TerminalSessionRow } from '../composables/useTerminalSessions'
import { useTerminalPoolSize } from '../composables/useTerminalPoolSize'
import { useTerminalShowWindows } from '../composables/useTerminalShowWindows'
import { useTerminalWindowListings } from '../composables/useTerminalWindowListings'
import { useTerminalWindows, type TerminalWindowTab, type UseTerminalWindows } from '../composables/useTerminalWindows'
import { useNewSession } from '../composables/useNewSession'
import { useResizablePanel } from '../composables/useResizablePanel'
import { useSessionActions } from '../composables/useSessionActions'
import { useSessionStatuses } from '../composables/useSessionStatuses'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getTerminalEndpoint, type TerminalEngine, type WindowState } from '../lib/terminalClient'
import { appErrorMessage } from '../lib/appError'
import { Available } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice'
import type { SessionStatus, SessionWindowStatus } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'
import type { MenuEntry } from '../types/menu'
import '@xterm/xterm/css/xterm.css'

const { checking, available, reason, ptyAvailable, engine, client, openTransport, useEngine } = useTerminalAvailability()

// Switching sessions must not blank the pane, so a switch no longer detaches:
// the last few attaches stay live in this pool — control client, stream and
// terminals intact, panes hidden — and snapping back to one is a v-show flip.
// Detach happens on eviction, explicit close, list removal, and unmount.
// The limit is Settings ▸ Appearance ▸ Terminal's warm-session count.
const { poolSize } = useTerminalPoolSize()
const pool = shallowReactive(new Map<string, UseTerminalWindows>())
const lastUsed: string[] = []
const activeSlug = ref('')
const current = computed(() => (activeSlug.value ? pool.get(activeSlug.value) ?? null : null))
// What the main area shows. It lags the selection during a cold attach: the
// outgoing session holds the pane until the incoming one has painted — or
// ended, or the hold cap fired — so a switch never shows a blank grid.
const displayed = shallowRef<UseTerminalWindows | null>(null)
const visible = computed(() => displayed.value ?? current.value)

const renamingId = ref('')
const renameDraft = ref('')

// How long the outgoing session may stand in for one that has not painted:
// long enough to cover a normal attach, short enough that a session with an
// empty screen — which sends no first paint at all — does not read as a dead
// click. Reached only on cold attaches; a pooled session reveals instantly.
const HOLD_MS = 300
const revealed = new WeakSet<UseTerminalWindows>()
let holdTimer: ReturnType<typeof setTimeout> | undefined

watch([current, () => current.value?.painted.value, () => current.value?.status.value], () => {
  clearTimeout(holdTimer)
  const incoming = current.value
  if (!incoming || incoming === displayed.value) return
  if (!displayed.value || revealed.has(incoming) || incoming.painted.value || incoming.status.value === 'ended') {
    reveal(incoming)
    return
  }
  holdTimer = setTimeout(() => {
    if (current.value === incoming) reveal(incoming)
  }, HOLD_MS)
}, { immediate: true })

function reveal(incoming: UseTerminalWindows): void {
  revealed.add(incoming)
  displayed.value = incoming
  void nextTick(() => {
    if (displayed.value === incoming) incoming.focusActive()
  })
}

function touchPool(slug: string): void {
  const at = lastUsed.indexOf(slug)
  if (at !== -1) lastUsed.splice(at, 1)
  lastUsed.push(slug)
  evictOverLimit()
}

function evictOverLimit(): void {
  for (const victim of [...lastUsed]) {
    if (pool.size <= poolSize.value) return
    if (victim !== activeSlug.value && pool.get(victim) !== displayed.value) dropSession(victim)
  }
}

// Shrinking the setting takes effect without a re-entry; growing it simply
// leaves room for the next attaches.
watch(poolSize, evictOverLimit)

// The only way out of the pool, so every exit funnels through here: eviction,
// explicit close, and sessions the listing no longer carries.
function dropSession(slug: string): void {
  const entry = pool.get(slug)
  if (!entry) return
  if (displayed.value === entry) displayed.value = null
  entry.dispose()
  pool.delete(slug)
  const at = lastUsed.indexOf(slug)
  if (at !== -1) lastUsed.splice(at, 1)
}

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
// recycled or corrupted one has no checkout left to open a terminal in, and
// attaching cannot start one. They still arrive in the listing, which is what
// the header's prune entry counts and acts on.
const attachable = computed(() => sessionRows.value.filter((row) => row.state === 'active'))
const sessionGroups = computed(() => groupTerminalSessions(attachable.value))
const prunableCount = computed(() => sessionRows.value.length - attachable.value.length)

const { statuses: sessionStatuses, startPolling: startStatusPolling, stopPolling: stopStatusPolling } = useSessionStatuses()
interface StatusIndicator {
  icon: Component
  color: string
  label: string
  animated?: boolean
}
const sessionLivenessIndicators = computed<Record<string, StatusIndicator>>(() => Object.fromEntries(
  Object.entries(sessionStatuses.value).map(([id, status]) => [id, {
    icon: IconCircle,
    color: status.running ? 'text-severity-success' : 'text-text-4',
    label: status.running ? 'Terminal running' : 'Terminal not running',
  }]),
))

function windowActivityIndicator(status: SessionWindowStatus): StatusIndicator {
  const tool = status.tool || 'Agent'
  switch (status.status) {
    case 'active':
      return { icon: IconLoaderCircle, color: 'text-severity-success', label: `${tool} is working`, animated: true }
    case 'approval':
      return { icon: IconCircleAlert, color: 'text-severity-warning', label: `${tool} needs approval` }
    case 'ready':
      return { icon: IconCircleCheck, color: 'text-text-2', label: `${tool} is ready` }
    default:
      return { icon: IconCircleOff, color: 'text-text-4', label: `${tool} status unavailable` }
  }
}

function windowIndicator(sessionID: string, windowID: string): StatusIndicator | null {
  const status = sessionStatuses.value[sessionID]?.windows?.find((window) => window.windowId === windowID)
  return status ? windowActivityIndicator(status) : null
}

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

function listedWindows(row: TerminalSessionRow): WindowState[] {
  if (!showAllWindows.value) return []
  return sessionWindows.value[row.slug] ?? []
}

// One keyed list feeds a session's subtree whichever source is fresher, so the
// listed → live swap on attach patches rows in place — window ids are stable
// across the swap — instead of unmounting one branch and mounting the other.
interface TreeWindowRow {
  windowId: string
  name: string
  active: boolean
  live: boolean
  indicator: StatusIndicator | null
}

// A pooled session's live tab set is fresher than its listing — but while its
// attach is still in flight, the cached listing stands in so selecting a
// session does not collapse its subtree.
function windowRowsFor(row: TerminalSessionRow): TreeWindowRow[] {
  const live = pool.get(row.slug)
  if (live?.tabs.value.length && (row.slug === activeSlug.value || showAllWindows.value)) {
    return live.tabs.value.map((tab) => ({
      windowId: tab.windowId,
      name: tab.name || tab.windowId,
      active: row.slug === activeSlug.value && tab.windowId === live.activeWindowId.value,
      live: true,
      indicator: windowIndicator(row.id, tab.windowId),
    }))
  }
  return listedWindows(row).map((win) => ({
    windowId: win.windowId,
    name: win.name || win.windowId,
    active: false,
    live: false,
    indicator: windowIndicator(row.id, win.windowId),
  }))
}

function openTreeWindow(row: TerminalSessionRow, win: TreeWindowRow): void {
  if (win.live && row.slug === activeSlug.value) void current.value?.select(win.windowId)
  else openWindow(row, win.windowId)
}

// Expand/collapse animates the measured height — the hooks only pin the start
// and end values, the .tree-expand-* classes carry the (fast) transition, and
// after-enter clears the inline height so an open panel resizes naturally.
function expandEnter(el: Element): void {
  const panel = el as HTMLElement
  panel.style.height = '0'
  panel.getBoundingClientRect() // commit the collapsed height before the target lands
  panel.style.height = `${panel.scrollHeight}px`
}

function expandAfterEnter(el: Element): void {
  (el as HTMLElement).style.height = ''
}

function expandLeave(el: Element): void {
  const panel = el as HTMLElement
  panel.style.height = `${panel.scrollHeight}px`
  panel.getBoundingClientRect()
  panel.style.height = '0'
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

const tabs = computed<TerminalWindowTab[]>(() => visible.value?.tabs.value ?? [])
const activeWindowId = computed(() => visible.value?.activeWindowId.value ?? '')
const activeScrolledUp = computed(() => tabs.value.some((tab) => tab.windowId === activeWindowId.value && tab.scrolledUp))
const status = computed(() => visible.value?.status.value ?? 'connecting')
// The cached listing stands in for the tab strip while the attach is in
// flight, so selecting a session swaps the strip's contents in place instead
// of emptying and rebuilding it.
const placeholderTabs = computed<WindowState[]>(() =>
  (status.value === 'connecting' && !tabs.value.length ? sessionWindows.value[activeSlug.value] ?? [] : []))
const endReason = computed(() => visible.value?.endReason.value ?? null)
// Not a failure: tmux is running nothing under this slug, and starting it runs
// the session's agent command — so it is offered, never done on selection.
const notStarted = computed(() => endReason.value === 'not-started')
const starting = ref('')
const startError = ref('')
const sessionError = computed(() => visible.value?.error.value ?? '')
const actionError = computed(() => visible.value?.actionError.value ?? '')
const sizeConstraint = computed(() => visible.value?.sizeConstraint.value ?? null)

// Every pooled session's panes stay mounted: a Terminal binds to one element
// for its lifetime, and an incoming session's first paint has to land while
// its panes are still hidden behind the held one.
const paneSessions = computed(() => [...pool.entries()].map(([slug, entry]) => ({
  slug,
  entry,
  tabs: entry.tabs.value,
  activeWindowId: entry.activeWindowId.value,
})))

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
    ptyAvailable.value = availability.ptyAvailable
    if (!availability.available) return
    if (!client.value) {
      openTransport(await getTerminalEndpoint())
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
watch([activeSlug, () => current.value?.activeWindowId.value ?? ''], ([slug, windowId]) => {
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
  if (sessionsError.value) return
  // A pooled session the listing stopped carrying was deleted or recycled out
  // from under its attach. The selected slug is handled below instead, because
  // telling its deletion apart from a rename needs the id.
  for (const pooledSlug of [...pool.keys()]) {
    if (pooledSlug !== slug && !rows.some((row) => row.slug === pooledSlug)) dropSession(pooledSlug)
  }
  if (!slug) return
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
    if (current.value?.status.value === 'ended') openSession(slug)
    else current.value?.focusActive()
    return
  }
  void router.push({ name: 'terminal', params: { slug } })
}

// Starting is the user's move, never a side effect of selecting a row: it runs
// the session's agent command. A session already running is not respawned, so
// this doubles as "open it" from the row menu.
async function startSession(slug: string): Promise<void> {
  if (!client.value || starting.value) return
  starting.value = slug
  startError.value = ''
  try {
    await client.value.start(slug)
  } catch (e) {
    startError.value = e instanceof Error && e.message ? e.message : 'Could not start this session.'
    return
  } finally {
    starting.value = ''
  }
  if (slug !== activeSlug.value) {
    void router.push({ name: 'terminal', params: { slug } })
    return
  }
  const pooled = pool.get(slug)
  if (!pooled) openSession(slug)
  else if (pooled.status.value === 'ended') void pooled.reconnect()
  else pooled.focusActive()
}

// Killing ends the terminal and nothing else — the checkout, the record and the
// work stay — but it stops whatever is running inside, so it is confirmed like
// the session's own destructive operations.
function requestKill(row: TerminalSessionRow): void {
  confirmation.request({
    title: 'Kill this terminal?',
    description: `The ${engine} session behind ${row.name} is killed, stopping the agent and anything else running in it. Its checkout and its work are untouched, and you can start it again from here.`,
    confirmLabel: 'Kill',
    onConfirm: () => killSession(row.slug),
  })
}

async function killSession(slug: string): Promise<void> {
  if (!client.value) return
  await client.value.kill(slug)
  // Re-attaching the killed session is what lands it on the start panel; a
  // pooled one that is not on screen is simply let go.
  if (slug === activeSlug.value) void pool.get(slug)?.reconnect()
  else dropSession(slug)
  if (showAllWindows.value) void refreshListings(client.value, attachable.value)
}

const ENGINE_OPTIONS: { id: TerminalEngine, label: string, title: string }[] = [
  { id: 'tmux', label: 'tmux', title: 'tmux control mode. Sessions live in the tmux server, so they survive Hive restarting and are shared with any terminal attached to them.' },
  { id: 'pty', label: 'pty', title: 'Process-managed. Hive owns the shells directly — no multiplexer, no size negotiation — but the sessions end when Hive exits.' },
]

// Switching backends releases every attach: window ids, sessions and streams
// belong to one engine, and nothing the old client opened means anything to the
// new one. What was running is left running — this drops the view of it, not
// the sessions themselves.
function switchEngine(next: TerminalEngine): void {
  if (next === engine.value || !client.value) return
  const slug = activeSlug.value
  for (const pooledSlug of [...pool.keys()]) dropSession(pooledSlug)
  detachSession()
  useEngine(next)
  void reloadSessions()
  if (slug) openSession(slug)
}

function openSession(slug: string): void {
  if (!client.value) return
  renamingId.value = ''
  startError.value = ''
  activeSlug.value = slug
  const pooled = pool.get(slug)
  if (pooled && pooled.status.value !== 'ended') {
    touchPool(slug)
    const wanted = routeWindow.value
    if (wanted && wanted !== pooled.activeWindowId.value && pooled.tabs.value.some((tab) => tab.windowId === wanted)) {
      void pooled.select(wanted)
    }
    return
  }
  if (pooled) dropSession(slug)
  const opened = useTerminalWindows(slug, client.value)
  pool.set(slug, opened)
  touchPool(slug)
  // Captured before attach: the mirror watcher rewrites ?window to tmux's
  // active the moment windows land, and the wanted one must survive that.
  const wanted = routeWindow.value
  void opened.start().then(() => {
    if (pool.get(slug) !== opened || !wanted) return
    // A window that no longer exists falls through to tmux's own active.
    if (opened.tabs.value.some((tab) => tab.windowId === wanted)) void opened.select(wanted)
  })
}

// Leaving for the picker keeps the pool warm; only closeSession and the
// listing pruning actually let an attach go.
function detachSession(): void {
  activeSlug.value = ''
  renamingId.value = ''
  startError.value = ''
  displayed.value = null
}

function closeSession(): void {
  dropSession(activeSlug.value)
  detachSession()
  restore.value = { slug: '', window: '' }
  void reloadSessions()
  // Replace, not push: the closed session's entry points at a session that is
  // gone, so Back must not walk into it.
  if (routeSlug.value) void router.replace({ name: 'terminal' })
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
  if (name) void visible.value?.rename(windowId, name)
}

onMounted(() => {
  void probe()
  startStatusPolling()
  prefetchNewSession()
})
onBeforeUnmount(() => {
  clearTimeout(holdTimer)
  stopStatusPolling()
  for (const slug of [...pool.keys()]) dropSession(slug)
})
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
           A pooled session's windows are its live tab set; the rest render
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
        <!-- The backend switch (ADR 0045). It renders only while both engines
             are mounted, because it is a comparison control rather than a
             setting: there is nothing to choose between when one is missing. -->
        <div
          v-if="ptyAvailable"
          class="flex h-8 shrink-0 items-center gap-2 border-b border-border px-3"
          data-testid="terminal-engine-switch"
        >
          <span class="font-mono text-[11px] uppercase tracking-[.08em] text-text-4">Engine</span>
          <div class="ml-auto flex gap-0.5 rounded-[7px] bg-chip p-0.5" role="radiogroup" aria-label="Terminal backend">
            <button
              v-for="option in ENGINE_OPTIONS"
              :key="option.id"
              type="button"
              role="radio"
              class="cursor-pointer rounded-[5px] px-2 py-0.5 font-mono text-[11.5px]"
              :class="engine === option.id ? 'bg-app text-text' : 'text-text-3 hover:text-text'"
              :data-testid="`terminal-engine-${option.id}`"
              :aria-checked="engine === option.id"
              :title="option.title"
              @click="switchEngine(option.id)"
            >{{ option.label }}</button>
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
            <Transition name="tree-expand" @enter="expandEnter" @after-enter="expandAfterEnter" @leave="expandLeave">
              <div v-if="!collapsedRepos.includes(group.key)" class="relative flex flex-col border-t border-border bg-app py-1">
                <TransitionGroup name="tree">
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
                      <!-- AppMenu must anchor to the positioned row so its panel
                           spans the row. Grid overlap avoids making this slot a
                           positioning ancestor while keeping its width fixed. -->
                      <div class="row-trailing" data-testid="terminal-session-trailing" @click.stop>
                        <span
                          v-if="sessionLivenessIndicators[row.id]"
                          class="row-status"
                          :class="sessionLivenessIndicators[row.id].color"
                          :title="sessionLivenessIndicators[row.id].label"
                          data-testid="terminal-session-liveness"
                          :data-status="sessionStatuses[row.id].running ? 'running' : 'inactive'"
                        >
                          <span
                            v-if="sessionStatuses[row.id].running"
                            class="size-2.5 rounded-full bg-current"
                            aria-hidden="true"
                          />
                          <component
                            :is="sessionLivenessIndicators[row.id].icon"
                            v-else
                            class="size-3"
                            aria-hidden="true"
                          />
                          <span class="sr-only">{{ sessionLivenessIndicators[row.id].label }}</span>
                        </span>
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
                        ><IconEllipsisVertical class="size-3" /></button>
                        <SessionRowMenu
                          v-if="openRowMenu === row.id"
                          :session="row"
                          :flip="rowMenuFlip"
                          :ignore="[rowMenuToggles.get(row.id) ?? null]"
                          @close="openRowMenu = ''"
                          @start="startSession(row.slug)"
                          @kill="requestKill(row)"
                          @detail="openSessionDetail(row)"
                          @rename="requestRename(row)"
                          @recycle="requestRecycle(row)"
                          @delete="requestDelete(row)"
                        />
                      </div>
                    </div>
                    <Transition name="tree-expand" @enter="expandEnter" @after-enter="expandAfterEnter" @leave="expandLeave">
                      <div v-if="windowRowsFor(row).length" class="relative flex flex-col pb-1">
                        <TransitionGroup name="tree">
                          <button
                            v-for="(win, index) in windowRowsFor(row)"
                            :key="win.windowId"
                            type="button"
                            class="window-row"
                            :class="{ 'window-row-last': index === windowRowsFor(row).length - 1, 'window-row-active': win.active }"
                            :data-testid="win.live ? 'terminal-window-row' : 'terminal-listed-window-row'"
                            :data-window-id="win.windowId"
                            :data-active="win.live ? win.active : undefined"
                            @click="openTreeWindow(row, win)"
                          >
                            <span class="min-w-0 flex-1 truncate font-mono text-[12.5px]">{{ win.name }}</span>
                            <span
                              v-if="win.indicator"
                              class="window-status"
                              :class="win.indicator.color"
                              :title="win.indicator.label"
                              data-testid="terminal-window-status"
                              :data-status="sessionStatuses[row.id]?.windows?.find((status) => status.windowId === win.windowId)?.status"
                            >
                              <component :is="win.indicator.icon" class="size-3" :class="{ 'animate-spin': win.indicator.animated }" aria-hidden="true" />
                              <span class="sr-only">{{ win.indicator.label }}</span>
                            </span>
                          </button>
                        </TransitionGroup>
                      </div>
                    </Transition>
                  </div>
                </TransitionGroup>
              </div>
            </Transition>
          </div>
        </div>
        <PanelResizeHandle edge="right" name="terminal-sidebar" :start="startResize" :step="step" />
      </aside>

      <div class="flex min-h-0 min-w-0 flex-1 flex-col">
        <div
          v-if="!visible"
          class="flex flex-1 flex-col items-center justify-center gap-2 px-10 text-center"
          data-testid="terminal-no-session"
        >
          <IconTerminal class="size-6 text-text-4" />
          <p class="text-xs text-text-3">Select a session to attach.</p>
        </div>

        <!-- A session with no tmux session behind it has no tabs to show and
             nothing to attach to yet, so the chrome stays out of the way and
             the panel below does the talking. -->
        <template v-if="visible && !notStarted">
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
                  @click="visible?.select(tab.windowId)"
                  @dblclick="startRename(tab)"
                >{{ tab.name || tab.windowId }}</button>
                <button
                  type="button"
                  class="flex size-4 shrink-0 cursor-pointer items-center justify-center rounded text-text-4 hover:bg-chip hover:text-text"
                  data-testid="terminal-close-window"
                  :aria-label="`Close ${tab.name || tab.windowId}`"
                  @click="visible?.closeWindow(tab.windowId)"
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
                @click="visible?.newWindow()"
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
              @click="visible?.dismissSizeConstraint()"
            ><IconX class="size-3" /></button>
          </div>

        </template>

        <!-- Rendered outside the v-if and merely hidden without a session: a
             pooled session's terminals must keep their elements, and unmounting
             the hosts would cost every one of them its screen. -->
        <div class="relative min-h-0 min-w-0 flex-1 flex-col" :class="visible ? 'flex' : 'hidden'">
          <template v-for="pane in paneSessions" :key="pane.slug">
            <TerminalTab
              v-for="tab in pane.tabs"
              :key="tab.uid"
              :tab="tab"
              :active="pane.entry === visible && tab.windowId === pane.activeWindowId"
              @mount="pane.entry.attachTab"
            />
          </template>
          <template v-if="visible">
            <div v-if="!tabs.length && status !== 'ended'" class="flex flex-1 items-center justify-center font-mono text-xs text-text-4">Attaching…</div>

            <!-- New output keeps landing below the fold while the viewport is
                 scrolled up; this is the way back to the live tail. -->
            <button
              v-if="activeScrolledUp && status !== 'ended'"
              type="button"
              class="absolute bottom-3 right-5 z-10 flex cursor-pointer items-center gap-1.5 rounded-full border border-strong bg-raised/95 px-3 py-1.5 text-[11.5px] text-text-2 shadow-lg hover:text-text"
              data-testid="terminal-scroll-to-bottom"
              @click="visible?.scrollToBottom()"
            ><IconArrowDown class="size-3" />Scroll to bottom</button>

            <!-- Selecting a session never starts it: starting runs the
                 session's own agent command, so it is offered here and taken
                 on a click. -->
            <div
              v-if="notStarted"
              class="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-app/95 px-10 text-center"
              data-testid="terminal-session-not-started"
            >
              <IconTerminal class="size-6 text-text-4" />
              <div class="text-[13.5px] font-semibold">Session not started</div>
              <p v-if="engine === 'pty'" class="max-w-[420px] text-xs leading-relaxed text-text-3">
                No terminal is running for <span class="font-mono text-text-2">{{ activeSlug }}</span> yet.
                Starting it opens one shell in this session's checkout. It runs no agent command, and it ends when Hive exits.
              </p>
              <p v-else class="max-w-[420px] text-xs leading-relaxed text-text-3">
                No terminal is running for <span class="font-mono text-text-2">{{ activeSlug }}</span> yet.
                Starting it opens this session's configured windows and runs its agent command.
              </p>
              <p v-if="startError" class="max-w-[420px] text-xs text-severity-error" data-testid="terminal-start-error">{{ startError }}</p>
              <div class="mt-1 flex items-center gap-2">
                <BaseButton
                  size="sm"
                  :busy="starting === activeSlug"
                  data-testid="terminal-start-session"
                  @click="startSession(activeSlug)"
                >
                  <template #icon><IconPlay class="size-3.5" /></template>
                  {{ starting === activeSlug ? 'Starting…' : 'Start session' }}
                </BaseButton>
                <BaseButton variant="secondary" size="sm" data-testid="terminal-close-session" @click="closeSession">Close</BaseButton>
              </div>
            </div>

            <div
              v-else-if="status === 'ended'"
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
                  @click="visible?.reconnect()"
                ><IconRefreshCw class="size-3" />Reconnect</button>
                <button
                  type="button"
                  class="cursor-pointer rounded border border-strong px-3 py-1.5 text-xs text-text-2 hover:text-text"
                  data-testid="terminal-close-session"
                  @click="closeSession"
                >Close session</button>
              </div>
            </div>
          </template>
        </div>
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

/* Tree motion, fast enough to read as instant: rows fade/slide over 150ms, a
   leaving row drops out of flow so its neighbors glide up through .tree-move
   (FLIP) instead of snapping, and expand/collapse animates the measured height
   set by the expand* hooks. */
.tree-enter-active, .tree-leave-active, .tree-move { transition: opacity .15s ease, transform .15s ease; }
.tree-enter-from, .tree-leave-to { opacity: 0; transform: translateY(-4px); }
.tree-leave-active { position: absolute; left: 0; right: 0; }
.tree-expand-enter-active, .tree-expand-leave-active { overflow: hidden; transition: height .15s ease; }
@media (prefers-reduced-motion: reduce) {
  .tree-enter-active, .tree-leave-active, .tree-move,
  .tree-expand-enter-active, .tree-expand-leave-active { transition: none; }
}

/* The shared slot keeps session names aligned while swapping liveness for actions. */
.row-trailing { display: grid; width: 18px; height: 18px; flex: none; align-self: center; }
.row-status, .row-action { grid-area: 1 / 1; }
.row-status, .window-status { display: flex; width: 18px; height: 18px; flex: none; align-items: center; justify-content: center; }
.row-status { pointer-events: none; }
.row-action { display: inline-flex; align-items: center; justify-content: center; border-radius: 5px; color: var(--color-text-4); cursor: pointer; opacity: 0; }
.row-action:hover, .row-action[aria-expanded="true"] { background: var(--color-app); color: var(--color-text); }
.session-row:hover .row-action, .row-action:focus-visible, .session-row.menu-open .row-action { opacity: 1; }
.session-row:hover .row-status, .session-row.menu-open .row-status, .row-trailing:focus-within .row-status { opacity: 0; }
</style>
