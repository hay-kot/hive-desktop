// The Agents area's control-plane transport: plain fetch calls against
// /api/terminal/agents/*, using the bearer token AgentsService.Endpoint()
// hands the webview (internal/adapter/httpapi/ctrl_agent_workspaces.go). The
// data plane is the tmux stream terminal mode uses (ADR 0063) — a session is
// a tmux session, just not a hive one — so this module reuses
// terminalClient's openStream(name)/decodeFrame/encodeInputFrames as-is
// rather than reimplementing the windowed wire: openStream is a stateless
// closure over whichever endpoint it was built from, and
// TerminalEndpoint/AgentsEndpoint are the same shape. Start/Resume attach
// server-side (AgentWorkspacesService) and return windowId, so this module
// never calls the generic /api/terminal/attach control route, which is gated
// by experimental.terminal — a flag the Agents area must not depend on.

import { createTerminalClient, decodeFrame, encodeInputFrames } from './terminalClient'
import type { TerminalEndpoint } from './terminalClient'
import { Endpoint } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice'
import type { AgentsEndpoint } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'

export type { AgentsEndpoint }
export { decodeFrame, encodeInputFrames }
export type { TerminalFrame } from './terminalClient'

/** One row of the area's workspace list. */
export interface AgentWorkspace {
  dir: string
  name: string
  agent: string
  autonomy: string
  mcps: string[]
  problem: string
  /** An unbounded-MCP or missing-manifest explanation, empty when neither applies. */
  notice: string
}

export interface AgentWorkspacesPayload {
  root: string
  /** Non-empty when the configured root itself could not be created or opened. */
  rootProblem: string
  available: boolean
  error: string
  workspaces: AgentWorkspace[]
  /** The agent keys this build can launch — the workspace editor's choices. */
  agents: string[]
}

/** The manifest fields the in-app workspace editor writes. */
export interface WorkspaceEditRequest {
  dir: string
  name: string
  agent: string
  autonomy: string
}

/** One row of a workspace's session list. */
export interface AgentSession {
  id: number
  workspace: string
  name: string
  agent: string
  lastOpenedAt: number
  /** The tmux session name on the shared terminal stream; empty when nothing is running. */
  terminalId: string
  /**
   * terminalId's active tmux window, needed to frame input/output on the
   * windowed tmux wire. Set only by startSession/resumeSession, which attach;
   * a plain sessions() listing leaves it empty even for a live session.
   */
  windowId: string
  /**
   * tmux's own size for that window at attach — the grid the pane must open
   * at, which may differ from the cols/rows voted (tmux's window-size option
   * picks whose size wins). 0 when tmux has not reported one. Set only by
   * startSession/resumeSession, like windowId.
   */
  cols: number
  rows: number
  resumeAttempted: boolean
  notice: string
}

/** One live session's detected activity — ready, active, or approval. */
export interface AgentSessionActivity {
  id: number
  status: 'ready' | 'active' | 'approval' | string
}

export interface AgentWorkspaceOpenResult {
  workspace: AgentWorkspace
  sessions: AgentSession[]
  missingMcps: string[]
}

export interface StartSessionRequest {
  workspace: string
  name: string
  cols?: number
  rows?: number
}

export interface ResumeSessionRequest {
  id: number
  cols?: number
  rows?: number
}

/**
 * A control-plane failure carrying the core's own classification, mirroring
 * PopupTerminalRequestError so a caller can tell "no such workspace" from
 * "unavailable" without reading the message.
 */
export class AgentRequestError extends Error {
  constructor(message: string, readonly kind: string) {
    super(message)
    this.name = 'AgentRequestError'
  }
}

export interface AgentWorkspacesClient {
  workspaces(): Promise<AgentWorkspacesPayload>
  openWorkspace(dir: string): Promise<AgentWorkspaceOpenResult>
  createWorkspace(request: WorkspaceEditRequest): Promise<AgentWorkspace>
  updateWorkspace(request: WorkspaceEditRequest): Promise<AgentWorkspace>
  deleteWorkspace(dir: string): Promise<void>
  sessions(workspace: string): Promise<AgentSession[]>
  /** Polled while the area is active; '' spans every workspace. Omits a session with no live tmux session. */
  activity(workspace: string): Promise<AgentSessionActivity[]>
  /** Votes a size for a live session's pane; tmux answers with a window 'resized' frame on the stream. */
  resizeSession(id: number, cols: number, rows: number): Promise<void>
  /** Sets a session's display name; the live terminal, if any, is untouched. */
  renameSession(id: number, name: string): Promise<void>
  /** Every session across every workspace, newest first in stable creation order. */
  allSessions(): Promise<AgentSession[]>
  startSession(request: StartSessionRequest): Promise<AgentSession>
  resumeSession(request: ResumeSessionRequest): Promise<AgentSession>
  closeSession(id: number): Promise<{ closed: boolean }>
  deleteSession(id: number): Promise<void>
  /** The shared tmux stream a session's terminalId addresses (ADR 0063). */
  openStream(name: string): WebSocket
}

/** Resolves the transport the webview was handed, or rejects with the reason. */
export async function getAgentsEndpoint(): Promise<AgentsEndpoint> {
  return await Endpoint()
}

export function createAgentWorkspacesClient(endpoint: AgentsEndpoint): AgentWorkspacesClient {
  async function post<T>(path: string, body: unknown): Promise<T | null> {
    const response = await fetch(`${endpoint.httpBaseURL}/api/terminal/agents${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${endpoint.token}` },
      body: JSON.stringify(body),
    })
    if (!response.ok) throw await failure(response)
    if (response.status === 204) return null
    return (await response.json()) as T
  }

  const { openStream } = createTerminalClient(endpoint as TerminalEndpoint)

  return {
    async workspaces() {
      const body = await post<AgentWorkspacesPayload>('/workspaces', {})
      if (!body) return { root: '', rootProblem: '', available: false, error: '', workspaces: [], agents: [] }
      return { ...body, workspaces: (body.workspaces ?? []).map(normalizeWorkspace), agents: body.agents ?? [] }
    },
    async createWorkspace(request) {
      const body = await post<AgentWorkspace>('/workspaces/create', request)
      if (!body) throw new AgentRequestError('the workspace was not created', '')
      return normalizeWorkspace(body)
    },
    async updateWorkspace(request) {
      const body = await post<AgentWorkspace>('/workspaces/update', request)
      if (!body) throw new AgentRequestError('the workspace was not updated', '')
      return normalizeWorkspace(body)
    },
    async openWorkspace(dir) {
      const body = await post<AgentWorkspaceOpenResult>('/workspaces/open', { dir })
      if (!body) return { workspace: emptyWorkspace(dir), sessions: [], missingMcps: [] }
      return { workspace: normalizeWorkspace(body.workspace), sessions: body.sessions ?? [], missingMcps: body.missingMcps ?? [] }
    },
    async deleteWorkspace(dir) {
      await post('/workspaces/delete', { dir })
    },
    async sessions(workspace) {
      const body = await post<{ sessions: AgentSession[] | null }>('/sessions', { workspace })
      return body?.sessions ?? []
    },
    async activity(workspace) {
      const body = await post<{ items: AgentSessionActivity[] | null }>('/sessions/activity', { workspace })
      return body?.items ?? []
    },
    async resizeSession(id, cols, rows) {
      await post('/sessions/resize', { id, cols, rows })
    },
    async renameSession(id, name) {
      await post('/sessions/rename', { id, name })
    },
    async allSessions() {
      const body = await post<{ sessions: AgentSession[] | null }>('/sessions/all', {})
      return body?.sessions ?? []
    },
    async startSession(request) {
      const body = await post<AgentSession>('/sessions/start', request)
      if (!body) throw new AgentRequestError('the session did not start', '')
      return body
    },
    async resumeSession(request) {
      const body = await post<AgentSession>('/sessions/resume', request)
      if (!body) throw new AgentRequestError('the session did not resume', '')
      return body
    },
    async closeSession(id) {
      const body = await post<{ closed: boolean }>('/sessions/close', { id })
      return { closed: !!body?.closed }
    },
    async deleteSession(id) {
      await post('/sessions/delete', { id })
    },
    openStream,
  }
}

function emptyWorkspace(dir: string): AgentWorkspace {
  return { dir, name: '', agent: '', autonomy: '', mcps: [], problem: '', notice: '' }
}

// normalizeWorkspace guards against a null mcps array on the wire: the Go
// side now always sends [], but this is the client boundary, so a template
// or composable can trust AgentWorkspace.mcps is iterable without its own
// null check regardless.
function normalizeWorkspace(w: AgentWorkspace): AgentWorkspace {
  return { ...w, mcps: w.mcps ?? [] }
}

async function failure(response: Response): Promise<AgentRequestError> {
  try {
    const body = await response.json() as { message?: string; kind?: string }
    if (body?.message) return new AgentRequestError(body.message, body.kind ?? '')
  } catch {
    // fall through to the status line
  }
  return new AgentRequestError(`${response.status} ${response.statusText}`.trim(), '')
}
