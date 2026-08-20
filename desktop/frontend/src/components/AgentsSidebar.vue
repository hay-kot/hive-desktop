<script setup lang="ts">
// The Agents area's sidebar: workspaces as parent rows with their chats nested
// beneath, in one scroll region. Position is what states which workspace a chat
// belongs to, so it holds for every workspace at once; focusing a workspace no
// longer hides the others.
//
// The row language is the hub sidebar's (SideBar.vue, SidebarFeedRow.vue), not
// the Code view's: single-line rounded rows, an 18px bordered icon chip leading
// each one, a tinted row for the selection, and actions revealed in a slot the
// row already reserves. A workspace row is SideBar's folder header — its chip
// swaps to a fold chevron on hover — and a chat row is a feed row. What the
// Code view still lends is the activity vocabulary: the spinner, the approval
// alert, and the liveness dot mean here exactly what they mean there.
//
// This component owns no pane state and no router — those stay in
// AgentsMode.vue. Every action that reaches the pane (resuming, closing,
// starting a chat) or the manifest (create/edit workspace — delete lives in
// the editor) is an emitted event; only tree state, the chat row menus, and
// the chat delete confirmation live here.
import { computed, ref, watch, type Component } from 'vue'
import { useStorage } from '@vueuse/core'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconChevronRight from '~icons/lucide/chevron-right'
import IconCircleAlert from '~icons/lucide/circle-alert'
import IconEllipsis from '~icons/lucide/ellipsis'
import IconFolderPlus from '~icons/lucide/folder-plus'
import IconLoaderCircle from '~icons/lucide/loader-circle'
import IconMessageSquare from '~icons/lucide/message-square'
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
// One node per workspace, carrying its own chats and the rollup its folded row
// summarises. Chats keep the order the cross-workspace read hands over (newest
// record first, stable under a resume — internal/app/store/queries:
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
// the same call the hub sidebar's folder collapse and the Code view's group
// collapse make. A workspace the user has never toggled has no entry and takes
// the default, which is open only where something is live or the open chat
// sits: a root's worth of dormant workspaces would otherwise bury the one being
// worked in. An explicit toggle always wins, including over the open chat,
// which is what makes a deliberate fold stay folded.
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

// The chip folds; the rest of the row focuses. Focus is what regenerates the
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

// ── What a row's tooltip says ────────────────────────────────────────────
// Everything a workspace row used to stack under its name. A brand mark stood
// in for the agent here for a while and earned nothing: the workspaces under
// one root normally run the same agent, so the column was one glyph repeated
// down the sidebar.
function workspaceTooltip(node: WorkspaceNode): string {
  if (!node.workspace) return `${node.dir}\nThis directory is no longer in the workspace root.`
  const parts = [node.name, `${node.workspace.agent} · ${node.workspace.autonomy || '—'}`]
  if (node.workspace.problem) parts.push(node.workspace.problem)
  else if (node.workspace.notice) parts.push(node.workspace.notice)
  return parts.join('\n')
}

// The Code view's vocabulary, unchanged: a spinning loader while the agent
// works, an alert while it waits on approval. Any other live chat gets the
// green liveness dot — ready-and-waiting is still live — and an idle chat an
// explicit hollow ring the same size, so live-vs-idle is always stated rather
// than implied by absence.
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

// A dormant chat says how long ago it was opened; a live one does not, because
// its mark in the trailing slot already says the only thing an age would
// compete with.
function chatAge(session: AgentSession): string {
  if (session.terminalId || sessionIndicators.value[session.id]) return ''
  const age = relativeAge(session.lastOpenedAt)
  return age === 'now' ? 'just now' : `${age} ago`
}

function chatTooltip(session: AgentSession): string {
  const parts = [session.name]
  const indicator = sessionIndicators.value[session.id]
  if (indicator) parts.push(indicator.label)
  else if (session.terminalId) parts.push('Agent running')
  if (session.notice) parts.push(session.notice)
  return parts.join('\n')
}

// ── Chat row menus (the hub sidebar's ellipsis + AppMenu shape) ──────────
// One menu is ever open, keyed 's:<id>'. The toggle shares the trailing slot
// with the status mark, so neither hovering a row nor opening its menu moves
// the name. The menu anchors to the row element and teleports (AppMenu's
// anchored mode): the tree scrolls, and an absolute panel inside a scroll
// region would extend it instead of floating over the sidebar. Workspace rows
// carry no menu — the editor is a workspace's whole management surface, delete
// included, so their reserved action is a single edit button.
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
    <div class="hive-scroll min-h-9 flex-1 overflow-y-auto px-2.5 pt-3 pb-4" data-testid="agents-sidebar-tree">
      <!-- One header for one region. Both + actions live here because a
           workspace and a chat are created at different depths of the same
           tree, and the tree has one top. -->
      <div class="section-label">
        <span>WORKSPACES</span>
        <button
          type="button"
          class="section-action ml-auto"
          title="New workspace"
          aria-label="New workspace"
          data-testid="agents-sidebar-new-workspace"
          @click="emit('create-workspace')"
        ><IconFolderPlus class="size-3" /></button>
        <button
          type="button"
          class="section-action"
          title="New chat"
          aria-label="New chat"
          data-testid="agents-sidebar-new-session"
          :disabled="startingSession"
          :aria-busy="startingSession"
          @click="emit('request-new-session')"
        >
          <IconLoaderCircle v-if="startingSession" class="size-3 animate-spin" />
          <IconPlus v-else class="size-3" />
        </button>
      </div>

      <p v-if="workspacesError" class="px-1.5 py-2 text-xs text-severity-error" data-testid="agents-sidebar-workspaces-error">{{ workspacesError }}</p>
      <div
        v-else-if="rootProblem"
        class="flex flex-col gap-2 px-1.5 py-2 text-xs text-text-3"
        data-testid="agents-sidebar-root-missing"
      >
        <p class="leading-relaxed">The configured workspace root is unavailable:</p>
        <p class="font-mono text-[11px] text-severity-error">{{ rootProblem }}</p>
        <p class="leading-relaxed">Point <code>agent_workspaces.dir</code> in settings.yaml at a reachable folder; Settings ▸ Agents shows where it resolves.</p>
      </div>
      <p v-else-if="!workspacesLoaded" class="px-1.5 py-2 font-mono text-xs text-text-4" data-testid="agents-sidebar-workspaces-loading">Loading…</p>
      <p v-else-if="!tree.length" class="px-1.5 py-2 text-xs text-text-3" data-testid="agents-sidebar-workspaces-empty">
        No workspaces yet. Create one with +.
      </p>
      <template v-else>
        <!-- The chat read can fail on its own, which leaves every workspace row
             correct and every count wrong; say so rather than draw an empty
             tree. -->
        <p v-if="recentsError" class="px-1.5 pb-1 text-[11px] text-severity-error" data-testid="agents-sidebar-sessions-error">{{ recentsError }}</p>

        <template v-for="(node, index) in tree" :key="node.dir">
          <!-- Not a <button>: the fold chip and the edit button are real
               buttons, which are invalid nested inside one. The div keeps the
               row focusable and Enter/Space focus the workspace like a button
               would (`.self`, so the chip's own keystrokes do not also focus). -->
          <div
            class="ws-row"
            :class="{ 'ws-row-focused': node.dir === selectedWorkspace, 'ws-row-first': index === 0 }"
            role="button"
            tabindex="0"
            data-testid="agents-sidebar-workspace-row"
            :data-dir="node.dir"
            :data-focused="node.dir === selectedWorkspace"
            :data-expanded="expanded(node)"
            :title="workspaceTooltip(node)"
            @click="focusWorkspace(node)"
            @keydown.enter.self.prevent="focusWorkspace(node)"
            @keydown.space.self.prevent="focusWorkspace(node)"
            @contextmenu.prevent="editWorkspace(node)"
          >
            <!-- A permanent disclosure triangle, not a glyph that becomes one
                 on hover: it is the one mark that separates a header from the
                 chats under it at rest, which is the whole job here. -->
            <button
              type="button"
              class="ws-toggle"
              :class="{
                'ws-toggle-problem': !node.workspace || !!node.workspace.problem,
                'ws-toggle-notice': !!node.workspace?.notice && !node.workspace?.problem,
              }"
              :aria-label="expanded(node) ? `Collapse ${node.name}` : `Expand ${node.name}`"
              :aria-expanded="expanded(node)"
              data-testid="agents-sidebar-workspace-toggle"
              @click.stop="toggleExpanded(node)"
            ><component :is="expanded(node) ? IconChevronDown : IconChevronRight" class="size-3.5" /></button>
            <span class="min-w-0 flex-1 truncate">{{ node.name }}</span>
            <button
              v-if="node.workspace"
              type="button"
              class="row-action"
              title="Edit workspace"
              aria-label="Edit workspace"
              data-testid="agents-sidebar-workspace-edit"
              @click.stop="editWorkspace(node)"
            ><IconPencil class="size-3" /></button>
            <!-- The rollup a folded row summarises with: how many chats, and
                 whether any of them is live or is waiting on the user. -->
            <span class="entry-count">{{ node.sessions.length || '' }}</span>
            <span class="entry-dot">
              <span
                v-if="node.live || node.waiting"
                class="size-2 rounded-full"
                :class="node.waiting ? 'bg-severity-warning' : 'bg-severity-success'"
                :title="node.waiting ? 'Needs approval' : 'Live chats'"
              />
            </span>
          </div>

          <template v-if="expanded(node)">
            <p
              v-if="!node.sessions.length"
              class="chat-empty"
              data-testid="agents-sidebar-workspace-no-chats"
            >No chats yet.</p>
            <template v-else>
              <div
                v-for="session in node.sessions"
                :key="session.id"
                :ref="(el) => trackRowEl(menuAnchors, `s:${session.id}`, el)"
                class="sidebar-entry"
                :class="{ 'sidebar-entry-selected': session.id === openSessionId, 'menu-open': openMenu === `s:${session.id}` }"
                role="button"
                tabindex="0"
                data-testid="agents-sidebar-session-row"
                :data-session-id="session.id"
                :data-workspace="node.dir"
                :data-open="session.id === openSessionId"
                :title="chatTooltip(session)"
                @click="emit('select-session', session)"
                @keydown.enter.self.prevent="emit('select-session', session)"
                @keydown.space.self.prevent="emit('select-session', session)"
                @contextmenu.prevent="toggleMenu(`s:${session.id}`)"
              >
                <span
                  class="nav-icon"
                  :class="{ 'nav-icon-notice': !!session.notice }"
                  :data-testid="session.notice ? 'agents-sidebar-session-notice' : undefined"
                ><IconMessageSquare class="size-3.5" /></span>
                <span class="min-w-0 flex-1 truncate">{{ session.name }}</span>
                <!-- The pin mark rides the name's line rather than the trailing
                     slot, which the status mark and the menu toggle already
                     share. -->
                <IconPin
                  v-if="isPinned(session.id)"
                  class="size-2.5 shrink-0 text-text-4"
                  title="Pinned to Code"
                  data-testid="agents-sidebar-session-pinned"
                />
                <span v-if="chatAge(session)" class="entry-age">{{ chatAge(session) }}</span>
                <!-- One cell, two occupants: the status is what the row says at
                     rest, the menu what it offers under the pointer. Neither
                     ever moves the name. -->
                <div class="entry-slot" @click.stop>
                  <span class="entry-status">
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
                      class="size-2 rounded-full bg-severity-success"
                      title="Agent running"
                      data-testid="agents-sidebar-session-liveness"
                    />
                    <span
                      v-else
                      class="size-2 rounded-full border border-text-4"
                      title="Not running"
                      data-testid="agents-sidebar-session-idle"
                    />
                  </span>
                  <button
                    :ref="(el) => trackRowEl(menuToggles, `s:${session.id}`, el)"
                    type="button"
                    class="entry-menu"
                    title="Chat actions"
                    aria-label="Chat actions"
                    aria-haspopup="menu"
                    :aria-expanded="openMenu === `s:${session.id}`"
                    data-testid="agents-sidebar-session-menu"
                    @click="toggleMenu(`s:${session.id}`)"
                  ><IconEllipsis class="size-3" /></button>
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
              </div>
            </template>
          </template>
        </template>
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
/* SidebarFeedRow's .sidebar-entry, worn by the chat rows: one line, a leading
   glyph, and trailing controls in reserved columns. The glyph is bare here
   rather than framed in that component's bordered tile — two levels of nesting
   means twice as many tiles down the column as the flat feed list has, and they
   read as a stack of boxes before they read as a list. */
.sidebar-entry { position: relative; display: flex; align-items: center; gap: 7px; padding: 6px 8px 6px 2px; border-radius: 7px; color: var(--color-text-2); font-size: 13px; cursor: pointer; }
.sidebar-entry:hover, .sidebar-entry.menu-open { background: var(--color-chip); color: var(--color-text); }
.sidebar-entry:focus-visible { outline: 2px solid var(--color-accent); outline-offset: -2px; }
/* Two strengths of mark, so they cannot be confused: the open chat fills its
   row, the focused workspace only goes accent. Focus scopes what the strips
   above the pane describe; the fill is what says "this one is in the pane". */
.sidebar-entry-selected { background: var(--color-hover); color: var(--color-accent); font-weight: 500; }
.sidebar-entry-selected .nav-icon { color: var(--color-accent); }

/* A workspace row is the section header for the chats under it, not a peer of
   them: a smaller, heavier, tracked label, a permanent disclosure triangle, and
   real space above each group. That carries the grouping on its own, which is
   why the chats are not indented under it — they share the header's columns
   instead, glyph under the triangle and name under the label. An indent as well
   would be the same statement made twice, and it costs the name its width in a
   sidebar this narrow. */
.ws-row { position: relative; display: flex; align-items: center; gap: 7px; margin-top: 12px; padding: 3px 8px 3px 2px; border-radius: 6px; color: var(--color-text-3); font-size: 11.5px; font-weight: 600; letter-spacing: .03em; cursor: pointer; }
.ws-row-first { margin-top: 0; }
.ws-row:hover { color: var(--color-text); }
.ws-row:focus-visible { outline: 2px solid var(--color-accent); outline-offset: -2px; }
.ws-row-focused { color: var(--color-accent); }

.ws-toggle { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 16px; height: 16px; border-radius: 4px; color: var(--color-text-4); cursor: pointer; }
.ws-toggle:hover { color: var(--color-accent); }
.ws-row-focused .ws-toggle { color: var(--color-accent); }
/* A problem or a notice used to be its own line of text under the name. It is
   the triangle's colour now, with the message on the row's tooltip — a warning
   is worth a glance, and its wording is worth a hover. */
.ws-toggle-notice, .ws-row-focused .ws-toggle-notice { color: var(--color-severity-warning); }
.ws-toggle-problem, .ws-row-focused .ws-toggle-problem { color: var(--color-severity-error); }

/* A fixed cell rather than a shrink-wrapped glyph, so the chat names line up
   down the column whatever mark a row is showing. */
.nav-icon { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 16px; height: 16px; color: var(--color-text-4); }
.nav-icon-notice { color: var(--color-severity-warning); }

/* Revealed by opacity, not display, so every trailing column stays reserved:
   hovering a row never reflows the name or hides the count. */
.row-action { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 18px; height: 18px; border-radius: 5px; color: var(--color-text-4); cursor: pointer; opacity: 0; }
.row-action:hover { background: var(--color-app); color: var(--color-text); }
.sidebar-entry:hover .row-action, .ws-row:hover .row-action, .row-action:focus-visible { opacity: 1; }

.entry-count { flex: none; min-width: 10px; text-align: right; font-family: var(--font-mono); font-size: 10.5px; font-weight: 400; letter-spacing: 0; color: var(--color-text-4); }
.ws-row-focused .entry-count { color: var(--color-accent); }
.entry-dot { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 10px; }
.entry-age { flex: none; font-family: var(--font-mono); font-size: 10.5px; color: var(--color-text-4); }

/* One 18px cell holding the status mark and the menu toggle, overlapped on the
   grid so swapping between them costs no layout anywhere on the row. */
.entry-slot { display: grid; flex: none; width: 18px; height: 18px; }
.entry-status, .entry-menu { grid-area: 1 / 1; }
.entry-status { display: inline-flex; align-items: center; justify-content: center; }
.entry-menu { display: inline-flex; align-items: center; justify-content: center; border-radius: 5px; color: var(--color-text-4); cursor: pointer; opacity: 0; }
.entry-menu:hover, .entry-menu[aria-expanded="true"] { background: var(--color-app); color: var(--color-text); }
.sidebar-entry:hover .entry-status, .sidebar-entry.menu-open .entry-status { opacity: 0; }
.sidebar-entry:hover .entry-menu, .entry-menu:focus-visible, .sidebar-entry.menu-open .entry-menu { opacity: 1; }

.section-label { display: flex; align-items: center; gap: 7px; padding: 0 6px 8px; color: var(--color-text-4); font-family: var(--font-mono); font-size: 10.5px; letter-spacing: .12em; }
.section-action { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 20px; height: 20px; border-radius: 6px; color: var(--color-text-3); cursor: pointer; }
.section-action:hover { background: var(--color-chip); color: var(--color-text); }
.section-action:disabled { cursor: default; opacity: .4; }

.chat-empty { padding: 6px 8px 6px 25px; font-size: 11.5px; font-style: italic; color: var(--color-text-4); }
</style>
