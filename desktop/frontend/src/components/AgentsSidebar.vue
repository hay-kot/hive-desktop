<script setup lang="ts">
// The Agents area's sidebar: one tree in one scroll region — workspaces as
// expandable parent rows, their chats nested beneath — drawn in the Code
// view's row language (full-bleed rows, a traveling accent rail on the open
// chat, a trailing status slot that swaps to an ellipsis menu toggle on
// hover, the same activity marks, the same drawn tree connector). Position
// is what states which workspace a chat belongs to, so it holds for every
// workspace at once; focusing a workspace no longer hides the others.
//
// It diverges from the Code view's tree in one place on purpose: the chevron
// leads the row rather than trailing it, because a workspace row is two or
// three lines tall and a trailing chevron on a tall row does not read as the
// handle for what is under it.
//
// This component owns no pane state and no router — those stay in
// AgentsMode.vue. Every action that reaches the pane (resuming, closing,
// starting a chat) or the manifest (create/edit workspace — delete lives in
// the editor) is an emitted event; only tree state, the chat row menus, and
// the chat delete confirmation live here.
import { computed, nextTick, ref, watch, type Component } from 'vue'
import { useStorage } from '@vueuse/core'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconChevronRight from '~icons/lucide/chevron-right'
import IconCircleAlert from '~icons/lucide/circle-alert'
import IconEllipsisVertical from '~icons/lucide/ellipsis-vertical'
import IconFolderPlus from '~icons/lucide/folder-plus'
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
  recents, recentsError, reloadRecents,
} = useAgentSessionsAll()
// Pinning is what puts a chat in the Code view's own sidebar; this row's menu is
// where it is turned on and off, and the mark below is how a row says it is on.
// It stays a Code-view arrangement rather than an ordering rule here — a pin
// changes which sidebar lists the chat, not where this one puts it.
const { isPinned, togglePin } = useTerminalPinnedChats()

// Both lists are module singletons (ADR a-workspace-declares-its-own-authority's shared-composable pattern),
// so this and AgentsMode's own workspaces reload can race harmlessly on
// activation — last response wins, and both fetch the same idempotent read.
watch(() => props.active, (active) => {
  if (!active) return
  void reloadWorkspaces()
  void reloadRecents()
}, { immediate: true })

// ── The tree ─────────────────────────────────────────────────────────────
// One node per workspace, carrying its own chats and the rollup its collapsed
// row summarises. Chats keep the order the cross-workspace read hands over
// (newest record first, stable under a resume — internal/app/store/queries:
// ListAllAgentWorkspaceSessions), so grouping costs no ordering.
interface WorkspaceNode {
  dir: string
  name: string
  /** null for a directory the manifest listing no longer knows about. */
  workspace: AgentWorkspace | null
  sessions: AgentSession[]
  live: boolean
  waiting: boolean
}

const tree = computed<WorkspaceNode[]>(() => {
  const byWorkspace = new Map<string, AgentSession[]>()
  for (const session of recents.value) {
    const bucket = byWorkspace.get(session.workspace)
    if (bucket) bucket.push(session)
    else byWorkspace.set(session.workspace, [session])
  }

  const nodes: WorkspaceNode[] = []
  function claim(dir: string, workspace: AgentWorkspace | null): void {
    const sessions = byWorkspace.get(dir) ?? []
    byWorkspace.delete(dir)
    nodes.push({
      dir,
      name: workspace?.name || dir,
      workspace,
      sessions,
      live: sessions.some((session) => !!session.terminalId),
      waiting: sessions.some((session) => props.sessionActivity[session.id] === 'approval'),
    })
  }

  for (const workspace of workspaces.value) claim(workspace.dir, workspace)
  // A directory removed outside the app leaves its chats in the database with
  // nowhere to hang. They get a row of their own rather than vanishing from a
  // list whose whole claim is that every chat is on it.
  for (const dir of [...byWorkspace.keys()]) claim(dir, null)
  return nodes
})

// Expand/collapse is transient view state, not configuration — localStorage,
// exactly as the Code view's group collapse. A workspace the user has never
// toggled has no entry and takes the default, which is open only where
// something is live or the open chat sits: a root's worth of dormant
// workspaces would otherwise bury the one being worked in. An explicit toggle
// always wins, including over the open chat, which is what makes a deliberate
// fold stay folded.
const expansion = useStorage<Record<string, boolean>>('hive.agents.sidebar.workspaces', {})

function expanded(node: WorkspaceNode): boolean {
  return expansion.value[node.dir]
    ?? (node.live || node.sessions.some((session) => session.id === props.openSessionId))
}

function toggleExpanded(node: WorkspaceNode): void {
  expansion.value[node.dir] = !expanded(node)
}

function editWorkspace(node: WorkspaceNode): void {
  if (node.workspace) emit('edit-workspace', node.workspace)
}

// The row body focuses; the chevron folds. Focus is what regenerates the
// workspace's files and fills the missing-capability strips above the pane, so
// it survived the filter it used to double as — but it only ever moves now.
// Clearing it would change nothing on screen except silently dropping those
// strips.
function focusWorkspace(node: WorkspaceNode): void {
  // A directory the listing cannot see cannot be opened, so its row is a
  // container and nothing more.
  if (!node.workspace) {
    toggleExpanded(node)
    return
  }
  unfold(node.dir)
  emit('select-workspace', node.dir)
}

// Only a workspace that is actually folded gets an entry written, so focusing
// one the default already opened leaves it on the default rather than pinning
// it open for good.
function unfold(dir: string): void {
  const node = tree.value.find((candidate) => candidate.dir === dir)
  if (node && !expanded(node)) expansion.value[dir] = true
}

// The route sets the focus too — the Code view deep-links into a chat by
// workspace — so unfolding rides the prop rather than only the click.
watch(() => props.selectedWorkspace, (dir) => {
  if (dir) unfold(dir)
}, { immediate: true })

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

// The meta line answers "what is it doing": the activity word for a live chat,
// or how long ago a stopped one was last opened. Where it is, the row's
// position under its workspace already says.
function sessionMeta(session: AgentSession): string {
  switch (props.sessionActivity[session.id]) {
    case 'approval': return 'needs approval'
    case 'active': return 'working'
    case 'ready': return 'ready'
  }
  if (session.terminalId) return 'live'
  const age = relativeAge(session.lastOpenedAt)
  return age === 'now' ? 'just now' : `${age} ago`
}

// ── Chat row menus (the Code view's ellipsis + AppMenu shape) ────────────
// One menu is ever open, keyed 's:<id>'. The toggle sits in the same fixed
// slot the status indicator occupies, so every trailing element shares the
// header +'s vertical axis. The menu anchors to the row element and
// teleports (AppMenu's anchored mode): the tree scrolls, and an absolute
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

// ── Panel sizing ─────────────────────────────────────────────────────────
// The sidebar's width, persisted like every other panel (useResizablePanel).
// There is no second handle: the workspaces/chats divider existed only because
// two lists competed for the same height, and one region has nothing to split.
const { size: sidebarWidth, startResize: startSidebarResize, step: stepSidebar } = useResizablePanel({
  storageKey: 'hive.panel.agents.sidebar', defaultSize: 260, min: 180, max: 400, edge: 'right',
})

// ── The selection rail (the Code view's traveling mark, TerminalMode.vue) ─
// One rail for the whole tree, measured off the open chat's row rather than
// drawn by it, so changing the selection reads as the same mark relocating.
// The focused workspace states itself in accent instead — two rails in one
// scroll region would read as two competing selections.
interface SelectionRail { y: number; height: number; shown: boolean }
const treeContent = ref<HTMLElement | null>(null)
const rail = ref<SelectionRail>({ y: 0, height: 0, shown: false })

// A rail with no row to sit on fades out where it stands rather than
// resetting, so it does not travel from a stale origin when one reappears.
function measureRail(): void {
  const content = treeContent.value
  const row = content?.querySelector<HTMLElement>('[data-testid="agents-sidebar-session-row"][data-open="true"]')
  if (!content || !row) {
    rail.value = { ...rail.value, shown: false }
    return
  }
  rail.value = {
    y: row.getBoundingClientRect().top - content.getBoundingClientRect().top,
    height: row.offsetHeight,
    shown: true,
  }
}

// The stored expansion map mutates in place, so the fold state reaches this
// watch as a key rather than by identity.
const foldKey = computed(() => tree.value.map((node) => `${node.dir}:${expanded(node) ? 1 : 0}`).join('|'))

watch(
  () => [props.openSessionId, tree.value, foldKey.value] as const,
  () => void nextTick(measureRail),
  { immediate: true },
)

// Rows also move without a selection change — a notice line appearing, a
// workspace refilling after a reload — and a content resize is every one of
// those.
watch(treeContent, (el, _previous, onCleanup) => {
  if (!el || typeof ResizeObserver === 'undefined') return
  const observer = new ResizeObserver(() => measureRail())
  observer.observe(el)
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
    <div class="hive-scroll min-h-9 flex-1 overflow-y-auto pb-4" data-testid="agents-sidebar-tree">
      <!-- One header for one region. Both + actions live here because a
           workspace and a chat are created at different depths of the same
           tree, and the tree has one top. -->
      <div class="flex items-center gap-1 px-3 pt-2.5 pb-1">
        <span class="font-mono text-[10.5px] uppercase tracking-[0.12em] text-text-4">Workspaces</span>
        <button
          type="button"
          class="ml-auto flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-chip hover:text-text"
          title="New workspace"
          aria-label="New workspace"
          data-testid="agents-sidebar-new-workspace"
          @click="emit('create-workspace')"
        ><IconFolderPlus class="size-3.5" /></button>
        <button
          type="button"
          class="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-chip hover:text-text disabled:cursor-default disabled:opacity-40"
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
      <p v-else-if="!tree.length" class="px-3 py-2 text-xs text-text-3" data-testid="agents-sidebar-workspaces-empty">
        No workspaces yet. Create one with +.
      </p>
      <template v-else>
        <!-- The chat read can fail on its own, which leaves every workspace row
             correct and every count wrong; say so rather than draw an empty
             tree. -->
        <p v-if="recentsError" class="px-3 py-1.5 text-[11px] text-severity-error" data-testid="agents-sidebar-sessions-error">{{ recentsError }}</p>
        <div ref="treeContent" class="relative flex flex-col">
          <template v-for="node in tree" :key="node.dir">
            <div
              class="group flex items-center hover:bg-chip"
              data-testid="agents-sidebar-workspace-row"
              :data-dir="node.dir"
              :data-focused="node.dir === selectedWorkspace"
              :data-expanded="expanded(node)"
              @contextmenu.prevent="editWorkspace(node)"
            >
              <button
                type="button"
                class="flex size-5 shrink-0 cursor-pointer items-center justify-center self-start rounded-[5px] text-text-4 hover:bg-app hover:text-text ml-1.5 mt-1.5"
                :title="expanded(node) ? `Collapse ${node.name}` : `Expand ${node.name}`"
                :aria-label="expanded(node) ? `Collapse ${node.name}` : `Expand ${node.name}`"
                :aria-expanded="expanded(node)"
                data-testid="agents-sidebar-workspace-toggle"
                @click="toggleExpanded(node)"
              ><component :is="expanded(node) ? IconChevronDown : IconChevronRight" class="size-3" /></button>
              <button
                type="button"
                class="flex min-w-0 flex-1 cursor-pointer flex-col items-start gap-0.5 py-1.5 pr-1 text-left"
                data-testid="agents-sidebar-workspace-select"
                @click="focusWorkspace(node)"
              >
                <span
                  class="w-full truncate text-[13px]"
                  :class="node.dir === selectedWorkspace ? 'font-medium text-accent' : 'text-text'"
                >{{ node.name }}</span>
                <span v-if="node.workspace" class="w-full truncate font-mono text-[10.5px] text-text-4">{{ node.workspace.agent }} · {{ node.workspace.autonomy || '—' }}</span>
                <span v-else class="w-full truncate text-[11px] text-severity-error" data-testid="agents-sidebar-workspace-missing">Directory is no longer in the workspace root.</span>
                <span v-if="node.workspace?.problem" class="w-full truncate text-[11px] text-severity-error" data-testid="agents-sidebar-workspace-problem">{{ node.workspace.problem }}</span>
                <span v-else-if="node.workspace?.notice" class="w-full text-[11px] leading-snug text-severity-warning" data-testid="agents-sidebar-workspace-notice">{{ node.workspace.notice }}</span>
              </button>
              <!-- The rollup a collapsed row summarises with: how many chats,
                   and whether any of them is live or is waiting on the user. -->
              <div class="flex shrink-0 items-center gap-1.5 pr-3" @click.stop>
                <span
                  v-if="node.sessions.length"
                  class="font-mono text-[11.5px]"
                  :class="node.dir === selectedWorkspace ? 'text-accent' : 'text-text-4'"
                >{{ node.sessions.length }}</span>
                <span class="flex size-5 items-center justify-center" :class="{ 'group-hover:hidden': node.workspace }">
                  <span
                    v-if="node.live || node.waiting"
                    class="size-2.5 rounded-full"
                    :class="node.waiting ? 'bg-severity-warning' : 'bg-severity-success'"
                    :title="node.waiting ? 'Needs approval' : 'Live chats'"
                  />
                </span>
                <button
                  v-if="node.workspace"
                  type="button"
                  class="hidden size-5 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-app hover:text-text group-hover:flex"
                  title="Edit workspace"
                  aria-label="Edit workspace"
                  data-testid="agents-sidebar-workspace-edit"
                  @click="emit('edit-workspace', node.workspace)"
                ><IconPencil class="size-3" /></button>
              </div>
            </div>

            <template v-if="expanded(node)">
              <p
                v-if="!node.sessions.length"
                class="chat-row-indent py-1.5 text-[11.5px] text-text-4"
                data-testid="agents-sidebar-workspace-no-chats"
              >No chats yet.</p>
              <template v-else>
              <div
                v-for="(session, index) in node.sessions"
                :key="session.id"
                :ref="(el) => trackRowEl(menuAnchors, `s:${session.id}`, el)"
                class="group chat-row flex items-center hover:bg-chip"
                :class="{ 'bg-chip': openMenu === `s:${session.id}`, 'chat-row-last': index === node.sessions.length - 1 }"
                data-testid="agents-sidebar-session-row"
                :data-session-id="session.id"
                :data-workspace="node.dir"
                :data-open="session.id === openSessionId"
                @contextmenu.prevent="toggleMenu(`s:${session.id}`)"
              >
                <button
                  type="button"
                  class="chat-row-indent flex min-w-0 flex-1 cursor-pointer flex-col items-start gap-0.5 py-1.5 pr-1 text-left"
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
              </template>
            </template>
          </template>
          <span
            class="agents-rail"
            :class="{ 'agents-rail-shown': rail.shown }"
            :style="{ transform: `translateY(${rail.y}px)`, height: `${rail.height}px` }"
            data-testid="agents-sidebar-session-rail"
            :data-shown="rail.shown"
            aria-hidden="true"
          />
        </div>
      </template>
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

/* A chat's name lines up past its workspace's, which starts at 26px — the 6px
   inset plus the 20px chevron. */
.chat-row-indent { padding-left: 34px; }

/* The tree connector is drawn, not typed (TerminalMode.vue's .window-row):
   a box-drawing glyph is only as tall as its font size, so stacked rows would
   show a gap where the TUI's cell grid shows an unbroken line. ::before is the
   vertical, stopped at the elbow on the last chat; ::after is the tick into the
   name. Both hang off the chevron's own centre line so the fold handle and the
   subtree it opens share an axis, and the tick is pinned to the name's line
   rather than the row's middle — a chat row is two or three lines tall, so its
   middle is under the meta text. */
.chat-row { position: relative; }
.chat-row::before { content: ''; position: absolute; left: 16px; top: 0; bottom: 0; border-left: 1px solid var(--color-strong); }
.chat-row::after { content: ''; position: absolute; left: 16px; top: 16px; width: 10px; border-top: 1px solid var(--color-strong); }
.chat-row-last::before { bottom: auto; height: 16px; }
</style>
