// The terminal transport. Control actions are plain fetch calls against the
// loopback HTTP API; the data plane is a native WebSocket carrying the binary
// frames defined in internal/adapter/httpapi/terminal_stream.go:
//
//   server -> client
//     0x00 Output      [0x00][winLen u8][windowId][paneLen u8][paneId][raw bytes]
//     0x01 WindowEvent [0x01][JSON {kind, windowId, name, active, width, height}]
//     0x02 Lifecycle   [0x02][JSON {kind, windowId, message}]
//   client -> server
//     0x10 Input       [0x10][winLen u8][windowId][raw bytes]

import { Endpoint } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice'
import type { TerminalEndpoint } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'

export type { TerminalEndpoint }

/** Carried as ?v=; the server rejects anything else before the upgrade. */
export const TERMINAL_WIRE_VERSION = '1'

/** The server's whole-frame cap on one client -> server message. */
export const MAX_INPUT_FRAME_BYTES = 4 << 10

const FRAME_OUTPUT = 0x00
const FRAME_WINDOW_EVENT = 0x01
const FRAME_LIFECYCLE = 0x02
const FRAME_INPUT = 0x10

export type WindowEventKind = 'added' | 'closed' | 'renamed' | 'active-changed' | 'resized'
export type LifecycleKind = 'attached' | 'paused' | 'resumed' | 'exited' | 'error'

/**
 * One tmux window. `width`/`height` are tmux's own size for it — whichever
 * attached client tmux's window-size option picked, which may be another
 * terminal entirely — and 0 when tmux has not reported one. Rendering at any
 * other size mangles the pane's cursor-addressed output.
 */
export interface WindowState {
  windowId: string
  name: string
  active: boolean
  width: number
  height: number
}

export type TerminalFrame =
  | { type: 'output'; windowId: string; paneId: string; data: Uint8Array }
  | { type: 'window'; kind: WindowEventKind; state: WindowState }
  | { type: 'lifecycle'; kind: LifecycleKind; windowId: string; message: string }

export interface TerminalClient {
  /** cols/rows are the opening size vote; 0x0 attaches without setting one. */
  attach(slug: string, cols: number, rows: number): Promise<{ windows: WindowState[] }>
  resize(slug: string, cols: number, rows: number): Promise<void>
  newWindow(slug: string): Promise<{ windowId: string }>
  closeWindow(slug: string, windowId: string): Promise<void>
  renameWindow(slug: string, windowId: string, name: string): Promise<void>
  selectWindow(slug: string, windowId: string): Promise<void>
  detach(slug: string): Promise<void>
  openStream(slug: string): WebSocket
}

const encoder = new TextEncoder()
const decoder = new TextDecoder()

/** Resolves the transport the webview was handed, or rejects with the reason. */
export async function getTerminalEndpoint(): Promise<TerminalEndpoint> {
  return await Endpoint()
}

export function createTerminalClient(endpoint: TerminalEndpoint): TerminalClient {
  async function post<T>(path: string, body: unknown): Promise<T | null> {
    const response = await fetch(endpoint.httpBaseURL + path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${endpoint.token}` },
      body: JSON.stringify(body),
    })
    if (!response.ok) throw new Error(await failureMessage(response))
    if (response.status === 204) return null
    return (await response.json()) as T
  }

  return {
    async attach(slug, cols, rows) {
      const body = await post<{ windows: Partial<WindowState>[] | null }>('/api/terminal/attach', { slug, cols, rows })
      return { windows: (body?.windows ?? []).map(toWindowState) }
    },
    async resize(slug, cols, rows) { await post('/api/terminal/resize', { slug, cols, rows }) },
    async newWindow(slug) {
      const body = await post<{ windowId: string }>('/api/terminal/windows/new', { slug })
      return { windowId: body?.windowId ?? '' }
    },
    async closeWindow(slug, windowId) { await post('/api/terminal/windows/close', { slug, windowId }) },
    async renameWindow(slug, windowId, name) { await post('/api/terminal/windows/rename', { slug, windowId, name }) },
    async selectWindow(slug, windowId) { await post('/api/terminal/windows/select', { slug, windowId }) },
    async detach(slug) { await post('/api/terminal/detach', { slug }) },
    openStream(slug) {
      const socket = new WebSocket(streamURL(endpoint, slug))
      socket.binaryType = 'arraybuffer'
      return socket
    },
  }
}

/**
 * Builds the data-plane URL. The bearer token cannot ride a header on a browser
 * handshake, so it goes in the query string the server also accepts.
 */
function streamURL(endpoint: TerminalEndpoint, slug: string): string {
  const url = new URL(endpoint.wsURL)
  url.searchParams.set('slug', slug)
  url.searchParams.set('token', endpoint.token)
  url.searchParams.set('v', TERMINAL_WIRE_VERSION)
  return url.toString()
}

/**
 * Splits keystrokes into input frames no larger than the server's cap. Bytes
 * are reassembled in order on the pane, so a split inside a multi-byte
 * sequence is harmless.
 */
export function encodeInputFrames(windowId: string, data: string | Uint8Array): Uint8Array[] {
  const id = encoder.encode(windowId)
  const body = typeof data === 'string' ? encoder.encode(data) : data
  const header = 2 + id.length
  const budget = MAX_INPUT_FRAME_BYTES - header
  if (budget <= 0) throw new Error('terminal window id is too long to frame')

  const frames: Uint8Array[] = []
  for (let offset = 0; offset < body.length; offset += budget) {
    const chunk = body.subarray(offset, offset + budget)
    const frame = new Uint8Array(header + chunk.length)
    frame[0] = FRAME_INPUT
    frame[1] = id.length
    frame.set(id, 2)
    frame.set(chunk, header)
    frames.push(frame)
  }
  return frames
}

/** Decodes one server frame, or null when it is malformed or of an unknown kind. */
export function decodeFrame(payload: ArrayBuffer | Uint8Array): TerminalFrame | null {
  const bytes = payload instanceof Uint8Array ? payload : new Uint8Array(payload)
  if (bytes.length === 0) return null

  switch (bytes[0]) {
    case FRAME_OUTPUT: {
      const window = readID(bytes, 1)
      if (!window) return null
      const pane = readID(bytes, window.next)
      if (!pane) return null
      return { type: 'output', windowId: window.id, paneId: pane.id, data: bytes.subarray(pane.next) }
    }
    case FRAME_WINDOW_EVENT: {
      const payload = readJSON<WindowState & { kind: WindowEventKind }>(bytes)
      if (!payload?.kind) return null
      return { type: 'window', kind: payload.kind, state: toWindowState(payload) }
    }
    case FRAME_LIFECYCLE: {
      const payload = readJSON<{ kind: LifecycleKind; windowId: string; message: string }>(bytes)
      if (!payload?.kind) return null
      return { type: 'lifecycle', kind: payload.kind, windowId: payload.windowId ?? '', message: payload.message ?? '' }
    }
    default:
      return null
  }
}

function toWindowState(window: Partial<WindowState>): WindowState {
  return {
    windowId: window.windowId ?? '',
    name: window.name ?? '',
    active: !!window.active,
    width: window.width ?? 0,
    height: window.height ?? 0,
  }
}

function readID(bytes: Uint8Array, offset: number): { id: string; next: number } | null {
  if (offset >= bytes.length) return null
  const size = bytes[offset]
  const start = offset + 1
  if (start + size > bytes.length) return null
  return { id: decoder.decode(bytes.subarray(start, start + size)), next: start + size }
}

function readJSON<T>(bytes: Uint8Array): T | null {
  try {
    return JSON.parse(decoder.decode(bytes.subarray(1))) as T
  } catch {
    return null
  }
}

async function failureMessage(response: Response): Promise<string> {
  try {
    const body = await response.json() as { message?: string }
    if (body?.message) return body.message
  } catch {
    // fall through to the status line
  }
  return `${response.status} ${response.statusText}`.trim()
}
