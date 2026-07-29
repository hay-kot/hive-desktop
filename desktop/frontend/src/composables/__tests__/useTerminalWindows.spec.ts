import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useTerminalWindows } from '../useTerminalWindows'
import type { TerminalClient } from '../../lib/terminalClient'

const xterm = vi.hoisted(() => {
  class FakeTerminal {
    cols = 80
    rows = 24
    options: Record<string, unknown> = {}
    write = vi.fn()
    focus = vi.fn()
    open = vi.fn()
    loadAddon = vi.fn()
    dispose = vi.fn()
    resize = vi.fn((cols: number, rows: number) => { this.cols = cols; this.rows = rows })
    onDataDisposed = false
    private handlers: ((data: string) => void)[] = []

    constructor() { FakeTerminal.instances.push(this) }

    onData(handler: (data: string) => void) {
      this.handlers.push(handler)
      return { dispose: () => { this.onDataDisposed = true } }
    }

    type(data: string): void {
      for (const handler of this.handlers) handler(data)
    }

    static instances: FakeTerminal[] = []
  }

  class FakeFitAddon {
    fit = vi.fn()
    dispose = vi.fn()
    constructor() { FakeFitAddon.instances.push(this) }
    static instances: FakeFitAddon[] = []
  }

  return { FakeTerminal, FakeFitAddon }
})

vi.mock('@xterm/xterm', () => ({ Terminal: xterm.FakeTerminal }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: xterm.FakeFitAddon }))

const encoder = new TextEncoder()

class FakeSocket {
  static OPEN = 1
  readyState = 1
  binaryType = 'blob'
  sent: Uint8Array[] = []
  closed = false
  onopen: (() => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null

  send(frame: Uint8Array): void { this.sent.push(frame) }
  close(): void { this.closed = true }
}

class FakeResizeObserver {
  static instances: FakeResizeObserver[] = []
  observed: unknown[] = []
  disconnected = false

  constructor(private readonly callback: () => void) { FakeResizeObserver.instances.push(this) }

  observe(target: unknown): void { this.observed.push(target) }
  disconnect(): void { this.disconnected = true }
  trigger(): void { this.callback() }
}

let sockets: FakeSocket[] = []

type MockedClient = { [K in keyof TerminalClient]: ReturnType<typeof vi.fn> }

function fakeClient(): MockedClient {
  return {
    attach: vi.fn().mockResolvedValue({
      windows: [
        { windowId: '@1', name: 'agent', active: true },
        { windowId: '@2', name: 'shell', active: false },
      ],
      streamPath: '/api/terminal/stream',
    }),
    resize: vi.fn().mockResolvedValue(undefined),
    newWindow: vi.fn().mockResolvedValue({ windowId: '@3' }),
    closeWindow: vi.fn().mockResolvedValue(undefined),
    renameWindow: vi.fn().mockResolvedValue(undefined),
    selectWindow: vi.fn().mockResolvedValue(undefined),
    detach: vi.fn().mockResolvedValue(undefined),
    openStream: vi.fn(() => {
      const socket = new FakeSocket()
      sockets.push(socket)
      return socket as unknown as WebSocket
    }),
  }
}

function open(client: MockedClient) {
  return useTerminalWindows('hive-abc', client as unknown as TerminalClient)
}

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

async function attached() {
  const client = fakeClient()
  const session = open(client)
  await session.start()
  await flushPromises()
  sockets[0].onopen?.()
  return { client, session, socket: sockets[0] }
}

describe('useTerminalWindows', () => {
  beforeEach(() => {
    sockets = []
    xterm.FakeTerminal.instances = []
    xterm.FakeFitAddon.instances = []
    FakeResizeObserver.instances = []
    globalThis.WebSocket = FakeSocket as unknown as typeof WebSocket
    globalThis.ResizeObserver = FakeResizeObserver as unknown as typeof ResizeObserver
  })

  afterEach(() => vi.useRealTimers())

  it('attaches, builds one terminal per window and goes live on the stream', async () => {
    const { session, socket } = await attached()

    expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@1', '@2'])
    expect(session.activeWindowId.value).toBe('@1')
    expect(xterm.FakeTerminal.instances).toHaveLength(2)
    expect(socket.binaryType).toBe('arraybuffer')
    expect(session.status.value).toBe('live')
  })

  it('writes an output frame to the terminal of its window', async () => {
    const { session, socket } = await attached()

    socket.onmessage?.({ data: outputFrame('@2', '%9', 'from the shell') })

    expect(session.tabs.value[0].term.write).not.toHaveBeenCalled()
    const written = (session.tabs.value[1].term.write as ReturnType<typeof vi.fn>).mock.calls[0][0]
    expect(new TextDecoder().decode(written)).toBe('from the shell')
  })

  it('maps window events onto the tab list', async () => {
    const { session, socket } = await attached()

    socket.onmessage?.({ data: jsonFrame(0x01, { kind: 'added', windowId: '@3', name: 'logs', active: false }) })
    expect(session.tabs.value.map((tab) => tab.name)).toEqual(['agent', 'shell', 'logs'])

    socket.onmessage?.({ data: jsonFrame(0x01, { kind: 'renamed', windowId: '@3', name: 'tail', active: false }) })
    expect(session.tabs.value[2].name).toBe('tail')

    socket.onmessage?.({ data: jsonFrame(0x01, { kind: 'active-changed', windowId: '@2', name: 'shell', active: true }) })
    expect(session.activeWindowId.value).toBe('@2')
    expect(session.tabs.value.map((tab) => tab.active)).toEqual([false, true, false])

    socket.onmessage?.({ data: jsonFrame(0x01, { kind: 'closed', windowId: '@3', name: 'tail', active: false }) })
    expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@1', '@2'])
  })

  it('disposes exactly the closed tab: its terminal, addon and resize observer', async () => {
    const { session, socket } = await attached()
    session.attachTab('@1', document.createElement('div'))
    session.attachTab('@2', document.createElement('div'))

    socket.onmessage?.({ data: jsonFrame(0x01, { kind: 'closed', windowId: '@2', name: 'shell', active: false }) })

    expect(xterm.FakeTerminal.instances[1].dispose).toHaveBeenCalledTimes(1)
    expect(xterm.FakeFitAddon.instances[1].dispose).toHaveBeenCalledTimes(1)
    expect(FakeResizeObserver.instances[1].disconnected).toBe(true)
    expect(xterm.FakeTerminal.instances[1].onDataDisposed).toBe(true)

    expect(xterm.FakeTerminal.instances[0].dispose).not.toHaveBeenCalled()
    expect(xterm.FakeFitAddon.instances[0].dispose).not.toHaveBeenCalled()
    expect(FakeResizeObserver.instances[0].disconnected).toBe(false)
  })

  it('sends typed input as an input frame for the typing window', async () => {
    const { session, socket } = await attached()

    xterm.FakeTerminal.instances[1].type('ls\r')

    expect(socket.sent).toHaveLength(1)
    expect(Array.from(socket.sent[0])).toEqual([0x10, 2, 0x40, 0x32, 0x6c, 0x73, 0x0d])
    expect(session.status.value).toBe('live')
  })

  it('splits input larger than the frame cap across frames', async () => {
    const { socket } = await attached()

    xterm.FakeTerminal.instances[0].type('x'.repeat(5000))

    expect(socket.sent).toHaveLength(2)
    for (const frame of socket.sent) expect(frame.length).toBeLessThanOrEqual(4096)
    const total = socket.sent.reduce((sum, frame) => sum + frame.length - 4, 0)
    expect(total).toBe(5000)
  })

  it('fits on a resize observation and pushes the new size to the control plane', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()

    session.attachTab('@1', document.createElement('div'))
    xterm.FakeTerminal.instances[0].cols = 120
    xterm.FakeTerminal.instances[0].rows = 40
    FakeResizeObserver.instances[0].trigger()
    await vi.advanceTimersByTimeAsync(100)

    expect(xterm.FakeFitAddon.instances[0].fit).toHaveBeenCalled()
    expect(client.resize).toHaveBeenCalledWith('hive-abc', 120, 40)
    // tmux sizes the client, not the window, so the hidden tab follows along.
    expect(xterm.FakeTerminal.instances[1].resize).toHaveBeenLastCalledWith(120, 40)
  })

  it('ends on a lifecycle exit with the reason tmux gave', async () => {
    const { session, socket } = await attached()

    socket.onmessage?.({ data: jsonFrame(0x02, { kind: 'exited', windowId: '', message: 'overflow' }) })

    expect(session.status.value).toBe('ended')
    expect(session.endReason.value).toBe('exited')
    expect(session.error.value).toBe('overflow')
    expect(socket.closed).toBe(true)
  })

  it('ends on a transport drop as a distinct signal', async () => {
    const { session, socket } = await attached()

    socket.onclose?.()

    expect(session.status.value).toBe('ended')
    expect(session.endReason.value).toBe('disconnected')
    expect(session.error.value).toContain('disconnected')
  })

  it('ends when the attach itself fails', async () => {
    const client = fakeClient()
    client.attach.mockRejectedValue(new Error('no terminal is attached for that slug'))
    const session = open(client)

    await session.start()

    expect(session.status.value).toBe('ended')
    expect(session.endReason.value).toBe('attach-failed')
    expect(session.error.value).toBe('no terminal is attached for that slug')
  })

  it('reconnect re-attaches with fresh terminals', async () => {
    const { client, session, socket } = await attached()
    socket.onerror?.()
    const before = session.tabs.value.map((tab) => tab.term)

    await session.reconnect()
    await flushPromises()
    sockets[1].onopen?.()

    expect(client.attach).toHaveBeenCalledTimes(2)
    expect(session.status.value).toBe('live')
    expect(xterm.FakeTerminal.instances).toHaveLength(4)
    for (const term of before) expect(term.dispose).toHaveBeenCalled()
    expect(session.tabs.value.every((tab) => !before.includes(tab.term))).toBe(true)
  })

  it('detaches and tears everything down on dispose', async () => {
    const { client, session, socket } = await attached()
    session.attachTab('@1', document.createElement('div'))

    session.dispose()

    expect(client.detach).toHaveBeenCalledWith('hive-abc')
    expect(socket.closed).toBe(true)
    expect(session.tabs.value).toHaveLength(0)
    expect(FakeResizeObserver.instances[0].disconnected).toBe(true)
    for (const term of xterm.FakeTerminal.instances) expect(term.dispose).toHaveBeenCalled()
  })

  it('drives window operations through the control plane', async () => {
    const { client, session } = await attached()

    await session.select('@2')
    await session.newWindow()
    await session.rename('@2', 'build')
    await session.closeWindow('@2')

    expect(client.selectWindow).toHaveBeenCalledWith('hive-abc', '@2')
    expect(client.newWindow).toHaveBeenCalledWith('hive-abc')
    expect(client.renameWindow).toHaveBeenCalledWith('hive-abc', '@2', 'build')
    expect(client.closeWindow).toHaveBeenCalledWith('hive-abc', '@2')
  })

  it('activates a window created from the toolbar once tmux announces it', async () => {
    const { session, socket } = await attached()

    await session.newWindow()
    socket.onmessage?.({ data: jsonFrame(0x01, { kind: 'added', windowId: '@3', name: 'logs', active: false }) })

    expect(session.activeWindowId.value).toBe('@3')
  })

  it('reports a failed control action without ending the session', async () => {
    const { client, session } = await attached()
    client.newWindow.mockRejectedValue(new Error('tmux refused'))

    await session.newWindow()

    expect(session.actionError.value).toBe('tmux refused')
    expect(session.status.value).toBe('live')
  })
})
