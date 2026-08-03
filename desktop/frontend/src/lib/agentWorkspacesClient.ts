// The Agents area's control-plane transport: plain fetch calls against
// /api/terminal/agents/*, using the bearer token AgentsService.Endpoint()
// hands the webview (internal/adapter/httpapi/ctrl_agent_workspaces.go). The
// PTY data plane is shared with the pop-up terminal (ADR 0060) — this module
// reuses popupTerminalClient's openStream(id) as-is over this endpoint rather
// than reimplementing it: it is a stateless closure over whichever endpoint it
// was built from, and PopupTerminalEndpoint/AgentsEndpoint are the same shape.

import { createPopupTerminalClient } from './popupTerminalClient'
import { Endpoint } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice'
import type { AgentsEndpoint } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'

export type { AgentsEndpoint }

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
}

/** One row of a workspace's session list. */
export interface AgentSession {
  id: number
  workspace: string
  name: string
  agent: string
  lastOpenedAt: number
  /** Addresses the live PTY on the shared stream; empty when nothing is running. */
  terminalId: string
  resumeAttempted: boolean
  notice: string
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
  deleteWorkspace(dir: string): Promise<void>
  sessions(workspace: string): Promise<AgentSession[]>
  startSession(request: StartSessionRequest): Promise<AgentSession>
  resumeSession(request: ResumeSessionRequest): Promise<AgentSession>
  closeSession(id: number): Promise<{ closed: boolean }>
  deleteSession(id: number): Promise<void>
  /** The shared ptyterm stream a session's terminalId addresses (ADR 0060). */
  openStream(id: string): WebSocket
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

  const { openStream } = createPopupTerminalClient(endpoint)

  return {
    async workspaces() {
      const body = await post<AgentWorkspacesPayload>('/workspaces', {})
      if (!body) return { root: '', rootProblem: '', available: false, error: '', workspaces: [] }
      return { ...body, workspaces: (body.workspaces ?? []).map(normalizeWorkspace) }
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
