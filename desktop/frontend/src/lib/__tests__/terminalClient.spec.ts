import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  MAX_INPUT_FRAME_BYTES,
  createTerminalClient,
  decodeFrame,
  encodeInputFrames,
} from '../terminalClient'

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice', () => ({
  Endpoint: vi.fn(),
}))

const encoder = new TextEncoder()
const endpoint = {
  httpBaseURL: 'http://127.0.0.1:58006',
  wsURL: 'ws://127.0.0.1:58006/api/terminal/stream',
  token: 'tok-123',
}

// Mirrors encodeOutputFrame in internal/adapter/httpapi/terminal_stream.go.
function outputFrame(windowId: string, paneId: string, data: string): ArrayBuffer {
  const win = encoder.encode(windowId)
  const pane = encoder.encode(paneId)
  const body = encoder.encode(data)
  const bytes = new Uint8Array(3 + win.length + pane.length + body.length)
  bytes[0] = 0x00
  bytes[1] = win.length
  bytes.set(win, 2)
  bytes[2 + win.length] = pane.length
  bytes.set(pane, 3 + win.length)
  bytes.set(body, 3 + win.length + pane.length)
  return bytes.buffer
}

function jsonFrame(kind: number, payload: unknown): ArrayBuffer {
  const body = encoder.encode(JSON.stringify(payload))
  const bytes = new Uint8Array(1 + body.length)
  bytes[0] = kind
  bytes.set(body, 1)
  return bytes.buffer
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('terminal frame codec', () => {
  it('decodes an output frame into its ids and raw bytes', () => {
    const frame = decodeFrame(outputFrame('@12', '%34', 'hello'))
    expect(frame).toMatchObject({ type: 'output', windowId: '@12', paneId: '%34' })
    expect(new TextDecoder().decode((frame as { data: Uint8Array }).data)).toBe('hello')
  })

  it('decodes an output frame with an empty payload', () => {
    const frame = decodeFrame(outputFrame('@1', '%1', ''))
    expect((frame as { data: Uint8Array }).data).toHaveLength(0)
  })

  it('decodes window events with their stable string kinds', () => {
    expect(decodeFrame(jsonFrame(0x01, { kind: 'renamed', windowId: '@2', name: 'shell', active: false, width: 213, height: 55 })))
      .toEqual({ type: 'window', kind: 'renamed', state: { windowId: '@2', name: 'shell', active: false, width: 213, height: 55 } })
  })

  it('decodes a resize as tmux reporting the size the window must render at', () => {
    expect(decodeFrame(jsonFrame(0x01, { kind: 'resized', windowId: '@2', name: 'shell', active: true, width: 80, height: 24 })))
      .toEqual({ type: 'window', kind: 'resized', state: { windowId: '@2', name: 'shell', active: true, width: 80, height: 24 } })
  })

  it('reads an unreported size as 0 rather than inventing one', () => {
    expect(decodeFrame(jsonFrame(0x01, { kind: 'added', windowId: '@3' })))
      .toEqual({ type: 'window', kind: 'added', state: { windowId: '@3', name: '', active: false, width: 0, height: 0 } })
  })

  it('decodes lifecycle events', () => {
    expect(decodeFrame(jsonFrame(0x02, { kind: 'exited', windowId: '', message: 'overflow' })))
      .toEqual({ type: 'lifecycle', kind: 'exited', windowId: '', message: 'overflow' })
  })

  it('rejects empty, truncated and unknown frames', () => {
    expect(decodeFrame(new Uint8Array(0))).toBeNull()
    expect(decodeFrame(new Uint8Array([0x00, 0x04, 0x40]))).toBeNull() // declares 4 id bytes, carries 1
    expect(decodeFrame(new Uint8Array([0x7f, 0x01]))).toBeNull()
    expect(decodeFrame(new Uint8Array([0x01, 0x7b]))).toBeNull() // '{' is not valid JSON
  })

  it('encodes an input frame as [0x10][len][id][bytes]', () => {
    const [frame] = encodeInputFrames('@12', 'ab')
    expect(Array.from(frame)).toEqual([0x10, 3, 0x40, 0x31, 0x32, 0x61, 0x62])
  })

  it('emits no frame for empty input', () => {
    expect(encodeInputFrames('@1', '')).toEqual([])
  })

  it('chunks input past the 4 KiB frame cap without losing bytes', () => {
    const payload = 'a'.repeat(5000)
    const frames = encodeInputFrames('@12', payload)

    expect(frames.length).toBe(2)
    for (const frame of frames) {
      expect(frame.length).toBeLessThanOrEqual(MAX_INPUT_FRAME_BYTES)
      expect(frame[0]).toBe(0x10)
      expect(frame[1]).toBe(3)
    }
    const rejoined = frames.map((frame) => new TextDecoder().decode(frame.subarray(5))).join('')
    expect(rejoined).toBe(payload)
  })
})

describe('createTerminalClient', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn()
    globalThis.fetch = fetchMock as unknown as typeof fetch
  })

  it('attaches with the bearer token and returns the window set', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, {
      windows: [{ windowId: '@1', name: 'agent', active: true, width: 213, height: 55 }],
    }))

    const result = await createTerminalClient(endpoint).attach('hive-abc', 120, 40)

    // The cols/rows posted are a vote; the sizes that come back are tmux's.
    expect(result.windows).toEqual([{ windowId: '@1', name: 'agent', active: true, width: 213, height: 55 }])
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://127.0.0.1:58006/api/terminal/attach')
    expect(init.headers.Authorization).toBe('Bearer tok-123')
    expect(JSON.parse(init.body)).toEqual({ slug: 'hive-abc', cols: 120, rows: 40 })
  })

  it('lists a session’s windows without attaching', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, {
      windows: [{ windowId: '@2', name: 'shell', active: false, width: 120, height: 40 }],
    }))

    const result = await createTerminalClient(endpoint).listWindows('hive-abc')

    expect(result.windows).toEqual([{ windowId: '@2', name: 'shell', active: false, width: 120, height: 40 }])
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://127.0.0.1:58006/api/terminal/windows/list')
    expect(JSON.parse(init.body)).toEqual({ slug: 'hive-abc' })
  })

  it('treats a 204 as success for the void operations', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    const client = createTerminalClient(endpoint)

    await expect(client.resize('hive-abc', 100, 30)).resolves.toBeUndefined()
    await expect(client.selectWindow('hive-abc', '@2')).resolves.toBeUndefined()
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ slug: 'hive-abc', windowId: '@2' })
  })

  it('moves a window to a position and answers with the order tmux settled on', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, {
      windows: [
        { windowId: '@2', name: 'shell', active: false, width: 120, height: 40 },
        { windowId: '@1', name: 'agent', active: true, width: 120, height: 40 },
      ],
    }))

    const result = await createTerminalClient(endpoint).moveWindow('hive-abc', '@1', 1)

    // The reply is a window set, not an acknowledgement: tmux owns the order.
    expect(result.windows.map((window) => window.windowId)).toEqual(['@2', '@1'])
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://127.0.0.1:58006/api/terminal/windows/move')
    expect(JSON.parse(init.body)).toEqual({ slug: 'hive-abc', windowId: '@1', position: 1 })
  })

  it('starts a session and reports whether the call is what spawned it', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { started: true }))

    const result = await createTerminalClient(endpoint).start('hive-abc')

    expect(result).toEqual({ started: true })
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://127.0.0.1:58006/api/terminal/start')
    expect(JSON.parse(init.body)).toEqual({ slug: 'hive-abc' })
  })

  it('kills a session and reports whether there was one to kill', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { killed: false }))

    const result = await createTerminalClient(endpoint).kill('hive-abc')

    expect(result).toEqual({ killed: false })
    expect(fetchMock.mock.calls[0][0]).toBe('http://127.0.0.1:58006/api/terminal/kill')
  })

  it('surfaces the core error message and its kind from a failed control action', async () => {
    fetchMock.mockResolvedValue(jsonResponse(404, { kind: 'not_found', message: 'no terminal is attached for that slug' }))

    // The kind is what tells a session that is not running apart from tmux
    // being unusable; the view branches on it rather than on the message.
    await expect(createTerminalClient(endpoint).detach('gone'))
      .rejects.toMatchObject({ message: 'no terminal is attached for that slug', kind: 'not_found' })
  })

  it('opens the stream on the versioned URL with the token in the query', () => {
    const created: string[] = []
    class FakeSocket {
      binaryType = 'blob'
      constructor(url: string) { created.push(url) }
    }
    globalThis.WebSocket = FakeSocket as unknown as typeof WebSocket

    const socket = createTerminalClient(endpoint).openStream('hive-abc')

    expect(socket.binaryType).toBe('arraybuffer')
    expect(created[0]).toBe('ws://127.0.0.1:58006/api/terminal/stream?slug=hive-abc&token=tok-123&v=1')
  })
})
