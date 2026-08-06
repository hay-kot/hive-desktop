<script setup lang="ts">
// The Agents area's sidebar: two flat sections on one plane — Workspaces,
// then Chats, drawn in the Code view's row language (full-bleed rows, a
// traveling accent rail plus accent-colored name on the active one, a
// trailing status slot that swaps to an ellipsis menu toggle on hover, the
// same activity marks). Focusing a workspace filters the chat list to it;
// the lit row is the whole statement of that scope.
//
// This component owns no pane state and no router — those stay in
// AgentsMode.vue. Every action that reaches the pane (resuming, closing,
// starting a chat) or the manifest (create/edit workspace — delete lives in
// the editor) is an emitted event; only list state, the chat row menus, and
// the chat delete confirmation live here.
import { computed, nextTick, ref, watch, type Component } from 'vue'
import IconCircleAlert from '~icons/lucide/circle-alert'
import IconEllipsisVertical from '~icons/lucide/ellipsis-vertical'
import IconLoaderCircle from '~icons/lucide/loader-circle'
import IconPencil from '~icons/lucide/pencil'
import IconPin from '~icons/lucide/pin'
import IconPinOff from '~icons/lucide/pin-off'
import IconPlus from '~icons/lucide/plus'
import IconPower from '~icons/lucide/power'
import IconTrash2 from '~icons/lucide/trash-2'
import AppMenu from './AppMenu.vue'
import ConfirmationDialog from './ConfirmationDialog.vue'
import PanelResizeHandle from './PanelResizeHandle.vue'
import { useAgentWorkspaces } from '../composables/useAgentWorkspaces'
import { useAgentSessionsAll } from '../composables/useAgentSessionsAll'
import { useResizablePanel } from '../composables/useResizablePanel'
import { useTerminalPinnedChats } from '../composables/useTerminalPinnedChats'
import { relativeAge } from '../lib/age'
import type { AgentSession, AgentWorkspace } from '../lib/agentWorkspacesClient'
import type { MenuEntry } from '../types/menu'

const props = withDefaults(defineProps<{
  active?: boolean
  /** The workspace the route currently has focused, '' when none. */
  selectedWorkspace?: string
  /** The chat currently attached to the pane, for highlighting its row. */
  openSessionId?: number | null
  /** True while AgentsMode is launching a chat into the pane. */
  startingSession?: boolean
  /** Per-chat activity status ('approval' | 'active' | 'ready') across every workspace. */
  sessionActivity?: Record<number, string>
}>(), {
  active: false,
  selectedWorkspace: '',
  openSessionId: null,
  startingSession: false,
  sessionActivity: () => ({}),
})

const emit = defineEmits<{
  'select-session': [session: AgentSession]
  'request-new-session': []
  /** '' clears the focus filter back to all workspaces. */
  'select-workspace': [dir: string]
  'create-workspace': []
  'edit-workspace': [workspace: AgentWorkspace]
  'close-session': [session: AgentSession]
  'rename-session': [session: AgentSession]
  'delete-session': [session: AgentSession]
}>()

const {
  workspaces, workspacesLoaded, workspacesError, rootProblem, reloadWorkspaces,
} = useAgentWorkspaces()
const {
  recents, recentsLoaded, recentsError, reloadRecents,
} = useAgentSessionsAll()
// Pinning is what puts a chat in the Code view's own sidebar; this row's menu is
// where it is turned on and off, and the mark below is how a row says it is on.
const { isPinned, togglePin } = useTerminalPinnedChats()

// Both lists are module singletons (ADR a-workspace-declares-its-own-authority's shared-composable pattern),
// so this and AgentsMode's own workspaces reload can race harmlessly on
// activation — last response wins, and both fetch the same idempotent read.
watch(() => props.active, (active) => {
  if (!active) return
  void reloadWorkspaces()
  void reloadRecents()
}, { immediate: true })

function workspaceName(dir: string): string {
  return workspaces.value.find((w) => w.dir === dir)?.name || dir
}

// ── Focus filter ─────────────────────────────────────────────────────────
const visibleSessions = computed(() =>
  (props.selectedWorkspace ? recents.value.filter((s) => s.workspace === props.selectedWorkspace) : recents.value))

function toggleWorkspace(dir: string): void {
  emit('select-workspace', dir === props.selectedWorkspace ? '' : dir)
}

// ── Row indicators, in the Code view's vocabulary (TerminalMode.vue): a
// spinning loader while the agent works, an alert while it waits on approval.
// Any other live chat gets the green session-liveness dot the Code view puts
// on a running session — ready-and-waiting is still live — and an idle chat
// an explicit hollow ring the same size, so live-vs-idle is always stated
// rather than implied by absence. ─────────────────────────────────────────
interface StatusIndicator {
  icon: Component
  cls: string
  label: string
  animated?: boolean
}

const sessionIndicators = computed<Record<number, StatusIndicator>>(() => {
  const out: Record<number, StatusIndicator> = {}
  for (const [id, status] of Object.entries(props.sessionActivity)) {
    switch (status) {
      case 'active':
        out[Number(id)] = { icon: IconLoaderCircle, cls: 'text-severity-success', label: 'Agent is working', animated: true }
        break
      case 'approval':
        out[Number(id)] = { icon: IconCircleAlert, cls: 'text-severity-warning', label: 'Agent needs approval' }
        break
    }
  }
  return out
})

const workspaceStats = computed<Record<string, { count: number; live: boolean; waiting: boolean }>>(() => {
  const stats: Record<string, { count: number; live: boolean; waiting: boolean }> = {}
  for (const session of recents.value) {
    const entry = (stats[session.workspace] ??= { count: 0, live: false, waiting: false })
    entry.count += 1
    if (session.terminalId) entry.live = true
    if (props.sessionActivity[session.id] === 'approval') entry.waiting = true
  }
  return stats
})

// The meta line answers "where is it and what is it doing": the workspace
// (dropped when the focus filter already says it) plus the activity word for
// a live chat, or how long ago a stopped one was last opened.
function sessionStatusText(session: AgentSession): string {
  switch (props.sessionActivity[session.id]) {
    case 'approval': return 'needs approval'
    case 'active': return 'working'
    case 'ready': return 'ready'
  }
  if (session.terminalId) return 'live'
  const age = relativeAge(session.lastOpenedAt)
  return age === 'now' ? 'just now' : `${age} ago`
}

function sessionMeta(session: AgentSession): string {
  const parts = props.selectedWorkspace ? [] : [workspaceName(session.workspace)]
  parts.push(sessionStatusText(session))
  return parts.join(' · ')
}

// ── Chat row menus (the Code view's ellipsis + AppMenu shape) ────────────
// One menu is ever open, keyed 's:<id>'. The toggle sits in the same fixed
// slot the status indicator occupies, so every trailing element shares the
// header +'s vertical axis. The menu anchors to the row element and
// teleports (AppMenu's anchored mode): both sections scroll, and an absolute
// panel inside a scroll region would extend it instead of floating over the
// sidebar. Workspace rows carry no menu — the editor is a workspace's whole
// management surface, delete included, so their hover slot is a single edit
// button.
const openMenu = ref('')
const menuToggles = new Map<string, HTMLElement>()
const menuAnchors = new Map<string, HTMLElement>()

function trackRowEl(map: Map<string, HTMLElement>, key: string, el: unknown): void {
  if (el instanceof HTMLElement) map.set(key, el)
  else map.delete(key)
}

function toggleMenu(key: string): void {
  openMenu.value = openMenu.value === key ? '' : key
}

function sessionMenuEntries(session: AgentSession): MenuEntry[] {
  const entries: MenuEntry[] = [
    { kind: 'action', id: 'rename', label: 'Rename…', icon: IconPencil, testid: 'agents-sidebar-session-rename' },
    isPinned(session.id)
      ? { kind: 'action', id: 'pin', label: 'Unpin from Code', icon: IconPinOff, testid: 'agents-sidebar-session-unpin' }
      : { kind: 'action', id: 'pin', label: 'Pin to Code', icon: IconPin, testid: 'agents-sidebar-session-pin' },
  ]
  if (session.terminalId) {
    entries.push({ kind: 'action', id: 'stop', label: 'Stop agent', icon: IconPower, testid: 'agents-sidebar-session-close' })
  }
  entries.push(
    { kind: 'separator' },
    { kind: 'action', id: 'delete', label: 'Delete', icon: IconTrash2, testid: 'agents-sidebar-session-delete' },
  )
  return entries
}

function onSessionMenuSelect(session: AgentSession, id: string): void {
  openMenu.value = ''
  if (id === 'rename') emit('rename-session', session)
  else if (id === 'pin') togglePin(session.id)
  else if (id === 'stop') emit('close-session', session)
  else if (id === 'delete') pendingDeleteSession.value = session
}

// ── Chat delete confirmation ─────────────────────────────────────────────
// A stacked ConfirmationDialog, deliberately not InlineConfirm: the inline
// strip is reserved for surfaces the user already opened (the workspace
// editor); a row menu has no such surface to expand inside.
const pendingDeleteSession = ref<AgentSession | null>(null)

function confirmDeleteSession(): void {
  if (!pendingDeleteSession.value) return
  emit('delete-session', pendingDeleteSession.value)
  pendingDeleteSession.value = null
}

// ── Panel sizing: the sidebar's width and the Workspaces/Chats divider, both
// persisted like every other panel (useResizablePanel). The divider sets the
// Workspaces section's height outright — DetailPane's reading-pane semantics:
// taller than the list leaves room, shorter scrolls it — so the split is the
// user's, not derived from content. Both sections floor at their header row
// (min-h-9) and the Workspaces section may flex-shrink on a short window;
// syncing the stored height to the rendered one when a drag starts keeps the
// handle live from the first pixel instead of spending travel un-storing a
// height the window could not show. ──────────────────────────────────────
const { size: sidebarWidth, startResize: startSidebarResize, step: stepSidebar } = useResizablePanel({
  storageKey: 'hive.panel.agents.sidebar', defaultSize: 260, min: 180, max: 400, edge: 'right',
})
const { size: workspacesHeight, startResize: startWorkspacesResize, step: stepWorkspaces } = useResizablePanel({
  storageKey: 'hive.panel.agents.workspaces', defaultSize: 240, min: 36, max: 800, edge: 'bottom',
})

const workspacesSection = ref<HTMLElement | null>(null)

function syncWorkspacesHeight(): void {
  const rendered = workspacesSection.value?.getBoundingClientRect().height
  if (rendered && Math.abs(rendered - workspacesHeight.value) > 1) workspacesHeight.value = Math.round(rendered)
}

function startWorkspacesDrag(event: PointerEvent): void {
  syncWorkspacesHeight()
  startWorkspacesResize(event)
}

function stepWorkspacesDivider(deltaPx: number): void {
  syncWorkspacesHeight()
  stepWorkspaces(deltaPx)
}

// ── Selection rails (the Code view's traveling mark, TerminalMode.vue) ────
// One accent rail per section, measured off the active row rather than drawn
// by it, so changing the selection reads as the same mark relocating.
interface SelectionRail { y: number; height: number; shown: boolean }
const workspacesContent = ref<HTMLElement | null>(null)
const sessionsContent = ref<HTMLElement | null>(null)
const workspaceRail = ref<SelectionRail>({ y: 0, height: 0, shown: false })
const sessionRail = ref<SelectionRail>({ y: 0, height: 0, shown: false })

// A rail with no row to sit on fades out where it stands rather than
// resetting, so it does not travel from a stale origin when one reappears.
function measureRail(content: HTMLElement | null, rail: SelectionRail, selector: string): SelectionRail {
  const row = content?.querySelector<HTMLElement>(selector)
  if (!content || !row) return { ...rail, shown: false }
  return {
    y: row.getBoundingClientRect().top - content.getBoundingClientRect().top,
    height: row.offsetHeight,
    shown: true,
  }
}

function measureRails(): void {
  workspaceRail.value = measureRail(workspacesContent.value, workspaceRail.value, '[data-testid="agents-sidebar-workspace-row"][data-focused="true"]')
  sessionRail.value = measureRail(sessionsContent.value, sessionRail.value, '[data-testid="agents-sidebar-session-row"][data-open="true"]')
}

watch(
  () => [props.selectedWorkspace, props.openSessionId, workspaces.value, visibleSessions.value] as const,
  () => void nextTick(measureRails),
  { immediate: true },
)

// Rows also move without a selection change — a notice line appearing, a list
// refilling after a reload — and a content resize is every one of those.
watch([workspacesContent, sessionsContent], (els, _previous, onCleanup) => {
  if (typeof ResizeObserver === 'undefined') return
  const observer = new ResizeObserver(() => measureRails())
  for (const el of els) if (el) observer.observe(el)
  onCleanup(() => observer.disconnect())
})

// ── Focus handle for the global keymap (agents.focus-sidebar) ────────────
const rootEl = ref<HTMLElement | null>(null)
defineExpose({ focus: () => rootEl.value?.focus() })
</script>

<template>
  <aside
    ref="rootEl"
    class="relative flex shrink-0 flex-col border-r border-border bg-sidebar"
    :style="{ width: `${sidebarWidth}px` }"
    data-testid="agents-workspace-sidebar"
    tabindex="-1"
  >
    <!-- Workspaces: a fixed-height scroll region — as tall or short as the
         divider says, whatever the list's own length, so a long chat list
         below never pushes it out of view and vice versa. -->
    <div
      ref="workspacesSection"
      class="relative min-h-9 border-b border-border"
      :style="{ height: `${workspacesHeight}px` }"
      data-testid="agents-sidebar-workspaces"
    >
      <div class="hive-scroll h-full overflow-y-auto pb-1.5">
        <div class="flex items-center px-3 pt-2.5 pb-1">
          <span class="font-mono text-[10.5px] uppercase tracking-[0.12em] text-text-4">Workspaces</span>
          <button
            type="button"
            class="ml-auto flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-chip hover:text-text"
            title="New workspace"
            aria-label="New workspace"
            data-testid="agents-sidebar-new-workspace"
            @click="emit('create-workspace')"
          ><IconPlus class="size-3.5" /></button>
        </div>
        <p v-if="workspacesError" class="px-3 py-2 text-xs text-severity-error" data-testid="agents-sidebar-workspaces-error">{{ workspacesError }}</p>
        <div
          v-else-if="rootProblem"
          class="flex flex-col gap-2 px-3 py-3 text-xs text-text-3"
          data-testid="agents-sidebar-root-missing"
        >
          <p class="leading-relaxed">The configured workspace root is unavailable:</p>
          <p class="font-mono text-[11px] text-severity-error">{{ rootProblem }}</p>
          <p class="leading-relaxed">Point <code>agent_workspaces.dir</code> in settings.yaml at a reachable folder; Settings ▸ Agents shows where it resolves.</p>
        </div>
        <p v-else-if="!workspacesLoaded" class="px-3 py-2 font-mono text-xs text-text-4" data-testid="agents-sidebar-workspaces-loading">Loading…</p>
        <p v-else-if="!workspaces.length" class="px-3 py-2 text-xs text-text-3" data-testid="agents-sidebar-workspaces-empty">
          No workspaces yet. Create one with +.
        </p>
        <div v-else ref="workspacesContent" class="relative flex flex-col">
          <div
            v-for="ws in workspaces"
            :key="ws.dir"
            class="group flex items-center hover:bg-chip"
            data-testid="agents-sidebar-workspace-row"
            :data-dir="ws.dir"
            :data-focused="ws.dir === selectedWorkspace"
            @contextmenu.prevent="emit('edit-workspace', ws)"
          >
            <button
              type="button"
              class="flex min-w-0 flex-1 cursor-pointer flex-col items-start gap-0.5 py-1.5 pl-3 pr-1 text-left"
              data-testid="agents-sidebar-workspace-select"
              @click="toggleWorkspace(ws.dir)"
            >
              <span
                class="w-full truncate text-[13px]"
                :class="ws.dir === selectedWorkspace ? 'font-medium text-accent' : 'text-text'"
              >{{ ws.name || ws.dir }}</span>
              <span class="w-full truncate font-mono text-[10.5px] text-text-4">{{ ws.agent }} · {{ ws.autonomy || '—' }}</span>
              <span v-if="ws.problem" class="w-full truncate text-[11px] text-severity-error" data-testid="agents-sidebar-workspace-problem">{{ ws.problem }}</span>
              <span v-else-if="ws.notice" class="w-full text-[11px] leading-snug text-severity-warning" data-testid="agents-sidebar-workspace-notice">{{ ws.notice }}</span>
            </button>
            <div class="flex shrink-0 items-center gap-1.5 pr-3" @click.stop>
              <span
                v-if="workspaceStats[ws.dir]?.count"
                class="font-mono text-[11.5px]"
                :class="ws.dir === selectedWorkspace ? 'text-accent' : 'text-text-4'"
              >{{ workspaceStats[ws.dir].count }}</span>
              <span class="flex size-5 items-center justify-center group-hover:hidden">
                <span
                  v-if="workspaceStats[ws.dir]?.live || workspaceStats[ws.dir]?.waiting"
                  class="size-2.5 rounded-full"
                  :class="workspaceStats[ws.dir].waiting ? 'bg-severity-warning' : 'bg-severity-success'"
                  :title="workspaceStats[ws.dir].waiting ? 'Needs approval' : 'Live chats'"
                />
              </span>
              <button
                type="button"
                class="hidden size-5 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-app hover:text-text group-hover:flex"
                title="Edit workspace"
                aria-label="Edit workspace"
                data-testid="agents-sidebar-workspace-edit"
                @click="emit('edit-workspace', ws)"
              ><IconPencil class="size-3" /></button>
            </div>
          </div>
          <span
            class="agents-rail"
            :class="{ 'agents-rail-shown': workspaceRail.shown }"
            :style="{ transform: `translateY(${workspaceRail.y}px)`, height: `${workspaceRail.height}px` }"
            data-testid="agents-sidebar-workspace-rail"
            :data-shown="workspaceRail.shown"
            aria-hidden="true"
          />
        </div>
      </div>
      <PanelResizeHandle edge="bottom" name="agents-workspaces" :start="startWorkspacesDrag" :step="stepWorkspacesDivider" />
    </div>

    <!-- Chats: a flat list across every workspace, narrowed to the focused
         one — the lit workspace row is the scope indicator. -->
    <div class="hive-scroll min-h-9 flex-1 overflow-y-auto pb-4" data-testid="agents-sidebar-sessions">
      <div class="flex items-center px-3 pt-2.5 pb-1">
        <span class="font-mono text-[10.5px] uppercase tracking-[0.12em] text-text-4">Chats</span>
        <button
          type="button"
          class="ml-auto flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-chip hover:text-text disabled:cursor-default disabled:opacity-40"
          title="New chat"
          aria-label="New chat"
          data-testid="agents-sidebar-new-session"
          :disabled="startingSession"
          :aria-busy="startingSession"
          @click="emit('request-new-session')"
        >
          <IconLoaderCircle v-if="startingSession" class="size-3.5 animate-spin" />
          <IconPlus v-else class="size-3.5" />
        </button>
      </div>
      <p v-if="recentsError" class="px-3 py-2 text-xs text-severity-error" data-testid="agents-sidebar-sessions-error">{{ recentsError }}</p>
      <p v-else-if="!recentsLoaded" class="px-3 py-2 font-mono text-xs text-text-4" data-testid="agents-sidebar-sessions-loading">Loading…</p>
      <p v-else-if="!visibleSessions.length" class="px-3 py-2 text-xs text-text-3" data-testid="agents-sidebar-sessions-empty">
        {{ selectedWorkspace ? 'No chats in this workspace yet.' : 'No chats yet. Start one with +.' }}
      </p>
      <div v-else ref="sessionsContent" class="relative flex flex-col">
        <div
          v-for="session in visibleSessions"
          :key="session.id"
          :ref="(el) => trackRowEl(menuAnchors, `s:${session.id}`, el)"
          class="group flex items-center hover:bg-chip"
          :class="{ 'bg-chip': openMenu === `s:${session.id}` }"
          data-testid="agents-sidebar-session-row"
          :data-session-id="session.id"
          :data-open="session.id === openSessionId"
          @contextmenu.prevent="toggleMenu(`s:${session.id}`)"
        >
          <button
            type="button"
            class="flex min-w-0 flex-1 cursor-pointer flex-col items-start gap-0.5 py-1.5 pl-3 pr-1 text-left"
            data-testid="agents-sidebar-session-select"
            @click="emit('select-session', session)"
          >
            <!-- The pin mark rides the name rather than the trailing slot, which
                 is a fixed grid the activity indicator and the menu toggle
                 already share. -->
            <span class="flex w-full min-w-0 items-center gap-1">
              <span
                class="min-w-0 flex-1 truncate text-[13px]"
                :class="session.id === openSessionId ? 'font-medium text-accent' : 'text-text'"
              >{{ session.name }}</span>
              <IconPin
                v-if="isPinned(session.id)"
                class="size-2.5 shrink-0 text-text-4"
                title="Pinned to Code"
                data-testid="agents-sidebar-session-pinned"
              />
            </span>
            <span class="w-full truncate font-mono text-[10.5px] text-text-4">{{ sessionMeta(session) }}</span>
            <span v-if="session.notice" class="w-full truncate text-[11px] text-severity-warning" :title="session.notice" data-testid="agents-sidebar-session-notice">{{ session.notice }}</span>
          </button>
          <div class="flex shrink-0 items-center justify-end pr-3" @click.stop>
            <span
              v-if="openMenu !== `s:${session.id}`"
              class="flex size-5 items-center justify-center group-hover:hidden"
            >
              <component
                :is="sessionIndicators[session.id].icon"
                v-if="sessionIndicators[session.id]"
                class="size-3"
                :class="[sessionIndicators[session.id].cls, { 'animate-spin': sessionIndicators[session.id].animated }]"
                :title="sessionIndicators[session.id].label"
                aria-hidden="true"
              />
              <span
                v-else-if="session.terminalId"
                class="size-2.5 rounded-full bg-severity-success"
                title="Agent running"
                data-testid="agents-sidebar-session-liveness"
              />
              <span
                v-else
                class="size-2.5 rounded-full border border-text-4"
                title="Not running"
                data-testid="agents-sidebar-session-idle"
              />
            </span>
            <button
              :ref="(el) => trackRowEl(menuToggles, `s:${session.id}`, el)"
              type="button"
              class="size-5 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-app hover:text-text"
              :class="openMenu === `s:${session.id}` ? 'flex bg-app text-text' : 'hidden group-hover:flex'"
              title="Chat actions"
              aria-label="Chat actions"
              aria-haspopup="menu"
              :aria-expanded="openMenu === `s:${session.id}`"
              data-testid="agents-sidebar-session-menu"
              @click="toggleMenu(`s:${session.id}`)"
            ><IconEllipsisVertical class="size-3.5" /></button>
          </div>
          <AppMenu
            v-if="openMenu === `s:${session.id}`"
            :entries="sessionMenuEntries(session)"
            :anchor="menuAnchors.get(`s:${session.id}`) ?? null"
            :ignore="[menuToggles.get(`s:${session.id}`) ?? null]"
            testid="agents-sidebar-session-menu-panel"
            @select="onSessionMenuSelect(session, $event)"
            @close="openMenu = ''"
          />
        </div>
        <span
          class="agents-rail"
          :class="{ 'agents-rail-shown': sessionRail.shown }"
          :style="{ transform: `translateY(${sessionRail.y}px)`, height: `${sessionRail.height}px` }"
          data-testid="agents-sidebar-session-rail"
          :data-shown="sessionRail.shown"
          aria-hidden="true"
        />
      </div>
    </div>
    <PanelResizeHandle edge="right" name="agents-sidebar" :start="startSidebarResize" :step="stepSidebar" />
  </aside>

  <ConfirmationDialog
    v-if="pendingDeleteSession"
    title="Delete chat?"
    :description="`Any live agent in ${pendingDeleteSession.name} is stopped and the chat is removed from the list.`"
    confirm-label="Delete chat"
    testid="agents-sidebar-delete-session-confirmation"
    @confirm="confirmDeleteSession"
    @cancel="pendingDeleteSession = null"
  />
</template>

<style scoped>
/* The Code view's tree-rail, verbatim: square ends, and motion fast enough to
   read as the same mark relocating rather than a second one appearing. */
.agents-rail {
  position: absolute; left: 0; top: 0; z-index: 1; width: 3px;
  background: var(--color-accent);
  opacity: 0;
  pointer-events: none;
  transition: transform .2s cubic-bezier(.2, 0, 0, 1), height .2s cubic-bezier(.2, 0, 0, 1), opacity .12s ease;
}
.agents-rail-shown { opacity: 1; }
</style>
