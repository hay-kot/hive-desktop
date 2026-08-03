import { ref, shallowRef, type Ref, type ShallowRef } from 'vue'
import {
  createAgentWorkspacesClient,
  getAgentsEndpoint,
  type AgentSession,
  type AgentWorkspace,
  type AgentWorkspacesClient,
  type ResumeSessionRequest,
  type StartSessionRequest,
} from '../lib/agentWorkspacesClient'
import { Available as AgentsAvailable } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice'

// Module singletons, mirroring useTerminalSessions.ts's shape: the rows are
// the area's workspace/session sets, not one view's, so the lists render the
// last-known rows while a reload revalidates rather than emptying and popping
// back in. `loaded` is distinct from `loading` for the same reason it is
// there — false both before the first load and after it lands, and the list
// needs the difference to tell an empty result from one it has not read yet.

const checking = ref(true)
const available = ref(false)
const reason = ref('')
const client: ShallowRef<AgentWorkspacesClient | null> = shallowRef(null)
let probe: Promise<void> | null = null

const workspaces = ref<AgentWorkspace[]>([])
const workspacesLoading = ref(false)
const workspacesLoaded = ref(false)
const workspacesError = ref<string | null>(null)
const root = ref('')
// rootProblem is the one signal from the workspaces payload this composable
// tracks separately from the top-level available/reason: it is the
// configured root path itself being unreachable (spec §14), a distinct axis
// from payload.available/error, which repeats the same ptyterm build/platform
// check AgentsService.Available() already gates the whole mode on.
const rootProblem = ref('')

const sessions = ref<AgentSession[]>([])
const sessionsLoading = ref(false)
const sessionsLoaded = ref(false)
const sessionsError = ref<string | null>(null)
const missingMCPs = ref<string[]>([])

// The availability answer and the transport are resolved once per run: like
// the pop-up terminal's probe, there is no program to install and nothing
// about either can change while the app is running.
function ensureProbed(): Promise<void> {
  probe ??= (async () => {
    try {
      const availability = await AgentsAvailable()
      available.value = availability.available
      reason.value = availability.reason
      if (availability.available) client.value = createAgentWorkspacesClient(await getAgentsEndpoint())
    } catch (error) {
      available.value = false
      reason.value = error instanceof Error ? error.message : 'The Agents area is unavailable.'
    } finally {
      checking.value = false
    }
  })()
  return probe
}

async function reloadWorkspaces(): Promise<void> {
  await ensureProbed()
  if (!client.value) return
  workspacesLoading.value = true
  workspacesError.value = null
  try {
    const payload = await client.value.workspaces()
    root.value = payload.root
    rootProblem.value = payload.rootProblem
    workspaces.value = payload.workspaces
  } catch (e) {
    // Keep the last-good rows: a failed revalidation reports itself without
    // collapsing the list it could not refresh.
    workspacesError.value = e instanceof Error && e.message ? e.message : 'Could not list workspaces.'
  } finally {
    workspacesLoading.value = false
    workspacesLoaded.value = true
  }
}

/** Regenerates dir's disposable artifacts and loads its sessions. */
async function openWorkspace(dir: string): Promise<void> {
  await ensureProbed()
  if (!client.value) return
  sessionsLoading.value = true
  sessionsError.value = null
  try {
    const result = await client.value.openWorkspace(dir)
    sessions.value = result.sessions
    missingMCPs.value = result.missingMcps
    // Fold the freshly regenerated view of this one workspace back into the
    // list rather than requiring a second round trip.
    const idx = workspaces.value.findIndex((w) => w.dir === dir)
    if (idx >= 0) workspaces.value = [...workspaces.value.slice(0, idx), result.workspace, ...workspaces.value.slice(idx + 1)]
  } catch (e) {
    sessionsError.value = e instanceof Error && e.message ? e.message : 'Could not open the workspace.'
  } finally {
    sessionsLoading.value = false
    sessionsLoaded.value = true
  }
}

/** Reloads workspace's sessions without regenerating its artifacts. */
async function reloadSessions(workspace: string): Promise<void> {
  if (!client.value) return
  sessionsLoading.value = true
  sessionsError.value = null
  try {
    sessions.value = await client.value.sessions(workspace)
  } catch (e) {
    sessionsError.value = e instanceof Error && e.message ? e.message : 'Could not list sessions.'
  } finally {
    sessionsLoading.value = false
    sessionsLoaded.value = true
  }
}

async function deleteWorkspace(dir: string): Promise<void> {
  if (!client.value) return
  await client.value.deleteWorkspace(dir)
  workspaces.value = workspaces.value.filter((w) => w.dir !== dir)
}

function replaceSession(next: AgentSession): void {
  const idx = sessions.value.findIndex((s) => s.id === next.id)
  sessions.value = idx >= 0
    ? [...sessions.value.slice(0, idx), next, ...sessions.value.slice(idx + 1)]
    : [...sessions.value, next]
}

async function startSession(request: StartSessionRequest): Promise<AgentSession> {
  if (!client.value) throw new Error('The Agents area is unavailable.')
  const session = await client.value.startSession(request)
  replaceSession(session)
  return session
}

async function resumeSession(request: ResumeSessionRequest): Promise<AgentSession> {
  if (!client.value) throw new Error('The Agents area is unavailable.')
  const session = await client.value.resumeSession(request)
  replaceSession(session)
  return session
}

async function closeSession(id: number): Promise<boolean> {
  if (!client.value) return false
  const result = await client.value.closeSession(id)
  if (result.closed) sessions.value = sessions.value.map((s) => (s.id === id ? { ...s, terminalId: '' } : s))
  return result.closed
}

async function deleteSession(id: number): Promise<void> {
  if (!client.value) return
  await client.value.deleteSession(id)
  sessions.value = sessions.value.filter((s) => s.id !== id)
}

/** Clears the session list — called when the open workspace changes. */
function resetSessions(): void {
  sessions.value = []
  sessionsLoaded.value = false
  sessionsError.value = null
  missingMCPs.value = []
}

export function useAgentWorkspaces(): {
  checking: Ref<boolean>
  available: Ref<boolean>
  reason: Ref<string>
  client: ShallowRef<AgentWorkspacesClient | null>
  workspaces: Ref<AgentWorkspace[]>
  workspacesLoading: Ref<boolean>
  workspacesLoaded: Ref<boolean>
  workspacesError: Ref<string | null>
  root: Ref<string>
  rootProblem: Ref<string>
  sessions: Ref<AgentSession[]>
  sessionsLoading: Ref<boolean>
  sessionsLoaded: Ref<boolean>
  sessionsError: Ref<string | null>
  missingMCPs: Ref<string[]>
  ready: () => Promise<void>
  reloadWorkspaces: () => Promise<void>
  openWorkspace: (dir: string) => Promise<void>
  reloadSessions: (workspace: string) => Promise<void>
  deleteWorkspace: (dir: string) => Promise<void>
  startSession: (request: StartSessionRequest) => Promise<AgentSession>
  resumeSession: (request: ResumeSessionRequest) => Promise<AgentSession>
  closeSession: (id: number) => Promise<boolean>
  deleteSession: (id: number) => Promise<void>
  resetSessions: () => void
} {
  return {
    checking, available, reason, client,
    workspaces, workspacesLoading, workspacesLoaded, workspacesError,
    root, rootProblem,
    sessions, sessionsLoading, sessionsLoaded, sessionsError, missingMCPs,
    ready: ensureProbed,
    reloadWorkspaces, openWorkspace, reloadSessions, deleteWorkspace,
    startSession, resumeSession, closeSession, deleteSession, resetSessions,
  }
}

export function resetAgentWorkspacesForTests(): void {
  checking.value = true
  available.value = false
  reason.value = ''
  client.value = null
  probe = null
  workspaces.value = []
  workspacesLoading.value = false
  workspacesLoaded.value = false
  workspacesError.value = null
  root.value = ''
  rootProblem.value = ''
  sessions.value = []
  sessionsLoading.value = false
  sessionsLoaded.value = false
  sessionsError.value = null
  missingMCPs.value = []
}
