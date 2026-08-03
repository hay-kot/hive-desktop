<script setup lang="ts">
// The Agents area: a plain two-level list — workspaces, then a selected
// workspace's sessions — beside a pane rendering whichever session is open.
// Unlike terminal mode's three-level session tree there is no keyboard walk
// to maintain; what is borrowed from TerminalMode.vue is narrower — the
// aside/main split and row shapes — and the pane itself borrows PopupTerminal
// .vue's xterm wiring, since a session rides the identical ptyterm wire
// protocol a pop-up terminal does (ADR 0060).
import { computed, markRaw, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Browser } from '@wailsio/runtime'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { Terminal, type IDisposable, type ILinkHandler } from '@xterm/xterm'
import IconBot from '~icons/lucide/bot'
import IconPlay from '~icons/lucide/play'
import IconPlus from '~icons/lucide/plus'
import IconPower from '~icons/lucide/power'
import IconRotateCw from '~icons/lucide/rotate-cw'
import IconTrash2 from '~icons/lucide/trash-2'
import ConfirmationDialog from './ConfirmationDialog.vue'
import { useAgentWorkspaces } from '../composables/useAgentWorkspaces'
import { useTerminalFont } from '../composables/useTerminalFont'
import { useTheme } from '../composables/useTheme'
import { xtermTheme } from '../lib/terminalTheme'
import { decodeFrame, encodeInputFrames } from '../lib/popupTerminalClient'
import { loadTerminalFaces, terminalFontStack } from '../lib/terminalFaces'
import { claimAtlasRenderer } from '../lib/terminalRenderer'
import { setAgentsTreeHandles } from '../lib/agentsTree'
import type { AgentSession } from '../lib/agentWorkspacesClient'
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{ active?: boolean }>()

const {
  checking, available, reason,
  workspaces, workspacesLoading, workspacesLoaded, workspacesError,
  root, rootProblem,
  sessions, sessionsLoading, sessionsLoaded, sessionsError, missingMCPs,
  client, ready,
  reloadWorkspaces, openWorkspace, reloadSessions, deleteWorkspace,
  startSession, resumeSession, closeSession, deleteSession, resetSessions,
} = useAgentWorkspaces()

const {
  px: fontSizePx, family: fontFamily, weight: fontWeight, weightBold: fontWeightBold, lineHeight, letterSpacing,
} = useTerminalFont()
const { theme } = useTheme()

// The session pane's state lives here, ahead of the route watcher below: that
// watcher's immediate call reaches into teardownPane on first run, and a
// `let`/`const` referenced before its own declaration line has executed is a
// ReferenceError even though the function that closes over it is hoisted.
const RESIZE_DEBOUNCE_MS = 80

const paneHost = ref<HTMLElement | null>(null)
const term = shallowRef<Terminal | null>(null)
const openSessionId = ref<number | null>(null)
const paneStatus = ref<'idle' | 'opening' | 'live' | 'ended'>('idle')
const paneError = ref('')
const endedReason = ref('')

let socket: WebSocket | null = null
let fit: FitAddon | null = null
let observer: ResizeObserver | null = null
let resizeTimer: ReturnType<typeof setTimeout> | undefined
let rendered = false
const disposers: IDisposable[] = []

const openSession = computed(() => sessions.value.find((s) => s.id === openSessionId.value) ?? null)
const paneLaidOut = computed(() => paneStatus.value === 'opening' || term.value !== null)

// ── Route-driven workspace selection ────────────────────────────────────────
const route = useRoute()
const router = useRouter()
const selectedWorkspace = computed(() =>
  (route.name === 'agents' && typeof route.params.workspace === 'string' ? route.params.workspace : ''))

function selectWorkspace(dir: string): void {
  if (dir === selectedWorkspace.value) return
  void router.push({ name: 'agents', params: { workspace: dir } })
}

function backToWorkspaces(): void {
  void router.push({ name: 'agents' })
}

const selectedWorkspaceRow = computed(() => workspaces.value.find((w) => w.dir === selectedWorkspace.value) ?? null)

watch(() => props.active, (active) => {
  if (!active) return
  void loadWorkspaces()
}, { immediate: true })

async function loadWorkspaces(): Promise<void> {
  await ready()
  if (available.value) void reloadWorkspaces()
}

watch(selectedWorkspace, async (dir, previous) => {
  if (dir === previous) return
  teardownPane()
  paneStatus.value = 'idle'
  openSessionId.value = null
  resetSessions()
  if (!dir) return
  await openWorkspace(dir)
}, { immediate: true })

// ── New session ──────────────────────────────────────────────────────────────
const newSessionName = ref('')
const startingSession = ref(false)

async function submitNewSession(): Promise<void> {
  const workspace = selectedWorkspace.value
  const name = newSessionName.value.trim()
  if (!workspace || !name || startingSession.value) return
  startingSession.value = true
  try {
    await launchIntoPane((size) => startSession({ workspace, name, ...size }))
    newSessionName.value = ''
  } finally {
    startingSession.value = false
  }
}

async function resumeRow(session: AgentSession): Promise<void> {
  await launchIntoPane((size) => resumeSession({ id: session.id, ...size }))
}

async function closeRow(session: AgentSession): Promise<void> {
  if (openSessionId.value === session.id) {
    teardownPane()
    paneStatus.value = 'idle'
    openSessionId.value = null
  }
  await closeSession(session.id)
}

async function removeRow(session: AgentSession): Promise<void> {
  if (openSessionId.value === session.id) {
    teardownPane()
    paneStatus.value = 'idle'
    openSessionId.value = null
  }
  await deleteSession(session.id)
}

// ── Workspace deletion ───────────────────────────────────────────────────────
const deleteWorkspaceOpen = ref(false)
const deletingWorkspace = ref(false)

function requestDeleteWorkspace(): void {
  deleteWorkspaceOpen.value = true
}

async function confirmDeleteWorkspace(): Promise<void> {
  if (!selectedWorkspace.value) return
  deletingWorkspace.value = true
  try {
    await deleteWorkspace(selectedWorkspace.value)
    deleteWorkspaceOpen.value = false
    backToWorkspaces()
  } finally {
    deletingWorkspace.value = false
  }
}

// ── The session pane ─────────────────────────────────────────────────────────
// Mirrors PopupTerminal.vue's xterm wiring over the same wire protocol
// (internal/adapter/httpapi/pty_stream.go); only the launch call and the
// control-plane base differ, per the shared client this composable resolves.

async function launchIntoPane(action: (size: { cols?: number; rows?: number }) => Promise<AgentSession>): Promise<void> {
  if (!client.value || paneStatus.value === 'opening') return
  teardownPane()
  openSessionId.value = null
  paneStatus.value = 'opening'
  paneError.value = ''
  try {
    await loadTerminalFaces(fontFamily.value, fontSizePx.value, fontWeight.value, fontWeightBold.value)
    await nextTick()
    const created = buildPane()
    if (!created) {
      paneError.value = 'The session pane could not be rendered.'
      paneStatus.value = 'idle'
      return
    }
    const size = measurePane()
    const result = await action(size ?? {})
    openSessionId.value = result.id
    if (!result.terminalId) {
      // An immediate exit, or a resume with no live terminal to reattach and
      // no resume form to relaunch through -- the row's own notice explains
      // why; there is nothing here to attach the pane to.
      teardownPane()
      paneStatus.value = 'idle'
      return
    }
    attachStream(created, result.terminalId)
    paneStatus.value = 'live'
    created.focus()
  } catch (failure) {
    teardownPane()
    paneStatus.value = 'idle'
    paneError.value = failure instanceof Error ? failure.message : 'The session could not be opened.'
  }
}

function buildPane(): Terminal | null {
  if (!paneHost.value) return null
  const created = markRaw(new Terminal({
    fontFamily: terminalFontStack(fontFamily.value),
    fontSize: fontSizePx.value,
    fontWeight: fontWeight.value,
    fontWeightBold: fontWeightBold.value,
    lineHeight: lineHeight.value,
    letterSpacing: letterSpacing.value,
    scrollback: 5000,
    theme: xtermTheme(),
    linkHandler,
  }))
  const fitAddon = markRaw(new FitAddon())
  created.loadAddon(fitAddon)
  created.loadAddon(markRaw(new WebLinksAddon((_event, uri) => openLink(uri))))
  created.open(paneHost.value)
  loadRenderer(created)
  term.value = created
  fit = fitAddon
  return created
}

function measurePane(): { cols: number; rows: number } | undefined {
  if (!paneHost.value?.clientWidth || !paneHost.value.clientHeight) return undefined
  const proposed = fit?.proposeDimensions()
  if (!proposed?.cols || !proposed.rows) return undefined
  return { cols: proposed.cols, rows: proposed.rows }
}

// Sessions have no live-resize endpoint in M1 -- ptyterm.Resize is reached
// through the pop-up surface only. A workspace session's grid is fixed at
// launch and re-measured on its next start or resume; onResize below just
// keeps xterm's own layout in step with the host box.
function attachStream(created: Terminal, terminalId: string): void {
  if (!client.value || !paneHost.value) return

  disposers.push(created.onData((data) => send(data)))

  const opened = client.value.openStream(terminalId)
  opened.onmessage = (event: MessageEvent<ArrayBuffer>) => {
    const frame = decodeFrame(event.data)
    if (!frame) return
    if (frame.type === 'output') created.write(frame.data)
    else exited()
  }
  opened.onerror = () => fail('The session connection dropped.')
  opened.onclose = () => { if (paneStatus.value === 'live') fail('The session connection closed.') }
  socket = opened

  observer = new ResizeObserver(() => scheduleFit())
  observer.observe(paneHost.value)
}

function send(data: string): void {
  if (socket?.readyState !== WebSocket.OPEN) return
  for (const frame of encodeInputFrames(data)) socket.send(frame)
}

function scheduleFit(): void {
  clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    try {
      fit?.fit()
    } catch {
      // A pane mid-transition can measure to nothing; the next observation fits.
    }
  }, RESIZE_DEBOUNCE_MS)
}

// The agent exited, so the pane goes with it; the session list is refreshed
// so its row drops the stale terminalId.
function exited(): void {
  teardownPane()
  paneStatus.value = 'idle'
  if (selectedWorkspace.value) void reloadSessions(selectedWorkspace.value)
}

// A stream that dropped for any other reason keeps the last screen readable
// rather than vanishing, the same rule PopupTerminal.vue follows.
function fail(why: string): void {
  if (paneStatus.value === 'ended') return
  paneStatus.value = 'ended'
  endedReason.value = why
  teardownStream()
}

function teardownStream(): void {
  if (socket) {
    socket.onmessage = null
    socket.onerror = null
    socket.onclose = null
    socket.close()
    socket = null
  }
}

function teardownPane(): void {
  teardownStream()
  clearTimeout(resizeTimer)
  observer?.disconnect()
  observer = null
  for (const disposer of disposers.splice(0)) disposer.dispose()
  term.value?.dispose()
  term.value = null
  fit = null
  rendered = false
  endedReason.value = ''
  paneError.value = ''
}

function openLink(uri: string): void {
  void Browser.OpenURL(uri).catch(() => {})
}

const linkHandler: ILinkHandler = { activate: (_event, uri) => openLink(uri) }

function loadRenderer(target: Terminal): void {
  claimAtlasRenderer(target, (addon) => disposers.push(addon), (claimed) => { rendered = claimed })
}

watch(theme, () => { if (term.value) term.value.options.theme = xtermTheme() })
watch(
  [fontSizePx, fontFamily, fontWeight, fontWeightBold, lineHeight, letterSpacing],
  async ([px, family, weight, weightBold, height, spacing]) => {
    await loadTerminalFaces(family, px, weight, weightBold)
    if (!term.value) return
    term.value.options.fontFamily = terminalFontStack(family)
    term.value.options.fontSize = px
    term.value.options.fontWeight = weight
    term.value.options.fontWeightBold = weightBold
    term.value.options.lineHeight = height
    term.value.options.letterSpacing = spacing
    scheduleFit()
  },
)

// ── Focus handles for the global keymap (agents.focus-sidebar/-pane) ─────────
const sidebarEl = ref<HTMLElement | null>(null)

onMounted(() => {
  setAgentsTreeHandles({
    focusList: () => sidebarEl.value?.focus(),
    focusPane: () => term.value?.focus(),
  })
})
onBeforeUnmount(() => {
  setAgentsTreeHandles(null)
  teardownPane()
})
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col bg-app" data-testid="agents-mode">
    <div v-if="checking" class="flex flex-1 items-center justify-center font-mono text-xs text-text-4">Checking…</div>

    <div
      v-else-if="!available"
      class="flex flex-1 flex-col items-center justify-center gap-3 px-10 text-center"
      data-testid="agents-unavailable"
    >
      <IconBot class="size-6 text-text-4" />
      <div class="text-[13.5px] font-semibold">Agents area unavailable</div>
      <p class="max-w-[420px] text-xs leading-relaxed text-text-3" data-testid="agents-unavailable-reason">
        {{ reason || 'The Agents area is not available in this build.' }}
      </p>
      <button
        type="button"
        class="mt-1 cursor-pointer rounded border border-strong px-3 py-1.5 text-xs text-text-2 hover:text-text"
        data-testid="agents-retry"
        @click="loadWorkspaces"
      >Try again</button>
    </div>

    <div v-else class="flex min-h-0 min-w-0 flex-1">
      <!-- Level one: workspaces. A plain list, not a tree -- no group headers,
           no keyboard walk. -->
      <aside
        ref="sidebarEl"
        class="flex w-[260px] shrink-0 flex-col border-r border-border bg-sidebar"
        data-testid="agents-workspace-sidebar"
        tabindex="-1"
      >
        <div class="flex h-9 shrink-0 items-center gap-2 border-b border-border px-3">
          <IconBot class="size-3 shrink-0 text-text-4" />
          <span class="min-w-0 flex-1 truncate text-[12.5px] text-text-3" :title="root">Workspaces</span>
          <button
            type="button"
            class="flex size-6 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text disabled:cursor-default"
            data-testid="agents-workspaces-refresh"
            aria-label="Reload workspaces"
            :aria-busy="workspacesLoading"
            :disabled="workspacesLoading"
            @click="reloadWorkspaces"
          ><IconRotateCw class="size-3.5" :class="{ 'animate-spin': workspacesLoading }" /></button>
        </div>
        <div class="hive-scroll min-h-0 flex-1 overflow-y-auto pb-4">
          <p v-if="workspacesError" class="px-3 py-2 text-xs text-severity-error" data-testid="agents-workspaces-error">{{ workspacesError }}</p>
          <div
            v-else-if="rootProblem"
            class="flex flex-col gap-2 px-3 py-3 text-xs text-text-3"
            data-testid="agents-root-missing"
          >
            <p class="leading-relaxed">The configured workspace root is unavailable:</p>
            <p class="font-mono text-[11px] text-severity-error">{{ rootProblem }}</p>
            <p class="leading-relaxed">Point Settings ▸ System ▸ Agent workspaces at a reachable folder.</p>
          </div>
          <p v-else-if="!workspacesLoaded" class="px-3 py-2 font-mono text-xs text-text-4" data-testid="agents-workspaces-loading">Loading…</p>
          <p v-else-if="!workspaces.length" class="px-3 py-2 text-xs text-text-3" data-testid="agents-workspaces-empty">
            No workspaces yet. Author one under {{ root }}.
          </p>
          <div v-else class="flex flex-col py-1">
            <button
              v-for="ws in workspaces"
              :key="ws.dir"
              type="button"
              class="flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left hover:bg-chip"
              :class="{ 'bg-chip': ws.dir === selectedWorkspace }"
              data-testid="agents-workspace-row"
              :data-dir="ws.dir"
              @click="selectWorkspace(ws.dir)"
            >
              <span class="flex w-full items-center gap-1.5">
                <span class="min-w-0 flex-1 truncate text-[13px] text-text">{{ ws.name || ws.dir }}</span>
                <span class="shrink-0 rounded-full border border-card px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide text-text-3">{{ ws.autonomy || '—' }}</span>
              </span>
              <span class="w-full truncate font-mono text-[11px] text-text-4">{{ ws.agent }}</span>
              <span v-if="ws.problem" class="w-full truncate text-[11px] text-severity-error" data-testid="agents-workspace-problem">{{ ws.problem }}</span>
              <span v-else-if="ws.notice" class="w-full text-[11px] leading-snug text-severity-warning" data-testid="agents-workspace-notice">{{ ws.notice }}</span>
            </button>
          </div>
        </div>
      </aside>

      <!-- Level two: the selected workspace's sessions, plus the pane. -->
      <div class="flex min-h-0 min-w-0 flex-1 flex-col">
        <div v-if="!selectedWorkspace" class="flex flex-1 items-center justify-center font-mono text-xs text-text-4">
          Select a workspace to see its sessions.
        </div>
        <template v-else>
          <div class="flex min-h-9 shrink-0 flex-wrap items-center gap-2 border-b border-border px-3 py-1.5">
            <span class="text-[12.5px] font-semibold text-text">{{ selectedWorkspaceRow?.name || selectedWorkspace }}</span>
            <span v-if="missingMCPs.length" class="text-[11px] text-severity-warning" data-testid="agents-missing-mcps">
              Missing MCP servers: {{ missingMCPs.join(', ') }}
            </span>
            <button
              type="button"
              class="ml-auto flex shrink-0 cursor-pointer items-center gap-1 rounded-[6px] px-2 py-1 text-[11.5px] text-text-3 hover:bg-chip hover:text-severity-error"
              data-testid="agents-workspace-delete"
              @click="requestDeleteWorkspace"
            ><IconTrash2 class="size-3.5" />Delete workspace</button>
          </div>

          <div class="hive-scroll flex shrink-0 flex-wrap items-center gap-1.5 border-b border-border px-3 py-2">
            <p v-if="sessionsError" class="text-xs text-severity-error" data-testid="agents-sessions-error">{{ sessionsError }}</p>
            <p v-else-if="!sessionsLoaded" class="font-mono text-xs text-text-4" data-testid="agents-sessions-loading">Loading sessions…</p>
            <template v-else>
              <div
                v-for="session in sessions"
                :key="session.id"
                class="flex items-center gap-1.5 rounded-[7px] border border-card px-2 py-1"
                :class="{ 'border-accent': session.id === openSessionId }"
                data-testid="agents-session-row"
                :data-session-id="session.id"
              >
                <span class="max-w-[160px] truncate text-[12px] text-text">{{ session.name }}</span>
                <span v-if="session.terminalId" class="size-1.5 rounded-full bg-severity-success" title="Running" />
                <span v-if="session.notice" class="max-w-[220px] truncate text-[11px] text-severity-warning" :title="session.notice" data-testid="agents-session-notice">{{ session.notice }}</span>
                <button
                  type="button"
                  class="flex size-5 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-chip hover:text-text"
                  title="Resume"
                  aria-label="Resume session"
                  data-testid="agents-session-resume"
                  @click="resumeRow(session)"
                ><IconPlay class="size-3" /></button>
                <button
                  v-if="session.terminalId"
                  type="button"
                  class="flex size-5 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-chip hover:text-severity-error"
                  title="Close"
                  aria-label="Close session"
                  data-testid="agents-session-close"
                  @click="closeRow(session)"
                ><IconPower class="size-3" /></button>
                <button
                  type="button"
                  class="flex size-5 cursor-pointer items-center justify-center rounded-[5px] text-text-3 hover:bg-chip hover:text-severity-error"
                  title="Delete"
                  aria-label="Delete session"
                  data-testid="agents-session-delete"
                  @click="removeRow(session)"
                ><IconTrash2 class="size-3" /></button>
              </div>
            </template>
            <form class="ml-auto flex shrink-0 items-center gap-1.5" @submit.prevent="submitNewSession">
              <input
                v-model="newSessionName"
                type="text"
                placeholder="New session name…"
                aria-label="New session name"
                class="w-[160px] rounded-[6px] border border-card bg-app px-2 py-1 text-[12px] text-text outline-none placeholder:text-text-4"
                data-testid="agents-new-session-name"
              >
              <button
                type="submit"
                class="flex size-6 cursor-pointer items-center justify-center rounded-[6px] text-text-3 hover:bg-chip hover:text-text disabled:cursor-default disabled:opacity-40"
                :disabled="!newSessionName.trim() || startingSession"
                aria-label="Start session"
                data-testid="agents-new-session-start"
              ><IconPlus class="size-3.5" /></button>
            </form>
          </div>

          <div class="relative min-h-0 flex-1 bg-app">
            <div
              v-show="paneLaidOut"
              ref="paneHost"
              class="absolute inset-0 p-1.5"
              data-terminal-input-scope
              data-testid="agents-session-pane"
              @click="term?.focus()"
            />
            <div v-if="!term" class="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
              <template v-if="paneStatus === 'opening'">
                <p class="font-mono text-xs text-text-4">Opening…</p>
              </template>
              <template v-else>
                <p v-if="paneError" class="max-w-[420px] text-xs leading-relaxed text-severity-error" data-testid="agents-pane-error">{{ paneError }}</p>
                <p v-else class="max-w-[360px] text-xs leading-relaxed text-text-4">Start a session, or resume one, to open it here.</p>
              </template>
            </div>
            <div
              v-if="paneStatus === 'ended'"
              class="absolute inset-x-0 bottom-0 flex items-center gap-3 border-t border-row bg-raised px-3 py-2"
              data-testid="agents-pane-ended"
            >
              <span class="truncate font-mono text-[11px] text-text-4">{{ endedReason }}</span>
              <span v-if="openSession" class="ml-auto shrink-0 font-mono text-[11px] text-text-4">{{ openSession.name }}</span>
            </div>
          </div>
        </template>
      </div>
    </div>

    <ConfirmationDialog
      v-if="deleteWorkspaceOpen"
      title="Delete workspace?"
      :description="`Close every live session in ${selectedWorkspaceRow?.name || selectedWorkspace} and remove its session records. The workspace directory itself is left on disk.`"
      confirm-label="Delete workspace"
      :busy="deletingWorkspace"
      testid="agents-delete-workspace-confirmation"
      @confirm="confirmDeleteWorkspace"
      @cancel="deleteWorkspaceOpen = false"
    />
  </div>
</template>
