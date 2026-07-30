import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useTerminalWindows } from '../useTerminalWindows'
import { setTerminalFontSize, terminalFontSizePx } from '../useTerminalFont'
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
    buffer = {
      active: { viewportY: 0, baseY: 0 },
      onBufferChange: (handler: () => void) => {
        this.bufferHandlers.push(handler)
        return { dispose: () => {} }
      },
    }
    scrollToBottom = vi.fn(() => {
      this.buffer.active.viewportY = this.buffer.active.baseY
      for (const handler of this.scrollHandlers) handler()
    })
    private handlers: ((data: string) => void)[] = []
    private scrollHandlers: (() => void)[] = []
    private bufferHandlers: (() => void)[] = []

    constructor(options: Record<string, unknown> = {}) {
      this.options = { ...options }
      FakeTerminal.instances.push(this)
    }

    onData(handler: (data: string) => void) {
      this.handlers.push(handler)
      return { dispose: () => { this.onDataDisposed = true } }
    }

    onScroll(handler: () => void) {
      this.scrollHandlers.push(handler)
      return { dispose: () => {} }
    }

    type(data: string): void {
      for (const handler of this.handlers) handler(data)
    }

    scrollTo(viewportY: number, baseY: number): void {
      this.buffer.active.viewportY = viewportY
      this.buffer.active.baseY = baseY
      for (const handler of this.scrollHandlers) handler()
    }

    switchBuffer(): void {
      for (const handler of this.bufferHandlers) handler()
    }

    static instances: FakeTerminal[] = []
  }

  class FakeFitAddon {
    proposed: { cols: number; rows: number } | undefined = { cols: 80, rows: 24 }
    proposeDimensions = vi.fn(() => this.proposed)
    dispose = vi.fn()
    constructor() { FakeFitAddon.instances.push(this) }
    static instances: FakeFitAddon[] = []
  }

  // Both renderer addons throw out of their constructor when the context they
  // need is missing, which is the fallback trigger the composable catches.
  class FakeWebglAddon {
    static instances: FakeWebglAddon[] = []
    static unavailable = false
    dispose = vi.fn()
    activate = vi.fn()
    private lossHandlers: (() => void)[] = []

    constructor() {
      if (FakeWebglAddon.unavailable) throw new Error('WebGL2 not supported')
      FakeWebglAddon.instances.push(this)
    }

    onContextLoss(handler: () => void) {
      this.lossHandlers.push(handler)
      return { dispose: () => {} }
    }

    loseContext(): void {
      for (const handler of this.lossHandlers) handler()
    }
  }

  class FakeCanvasAddon {
    static instances: FakeCanvasAddon[] = []
    static unavailable = false
    dispose = vi.fn()
    activate = vi.fn()

    constructor() {
      if (FakeCanvasAddon.unavailable) throw new Error('no 2d context')
      FakeCanvasAddon.instances.push(this)
    }
  }

  return { FakeTerminal, FakeFitAddon, FakeWebglAddon, FakeCanvasAddon }
})

vi.mock('@xterm/xterm', () => ({ Terminal: xterm.FakeTerminal }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: xterm.FakeFitAddon }))
vi.mock('@xterm/addon-webgl', () => ({ WebglAddon: xterm.FakeWebglAddon }))
vi.mock('@xterm/addon-canvas', () => ({ CanvasAddon: xterm.FakeCanvasAddon }))

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
let loadedFaces: string[] = []

type MockedClient = { [K in keyof TerminalClient]: ReturnType<typeof vi.fn> }

function fakeClient(): MockedClient {
  return {
    attach: vi.fn().mockResolvedValue({
      windows: [
        { windowId: '@1', name: 'agent', active: true, width: 213, height: 55 },
        { windowId: '@2', name: 'shell', active: false, width: 213, height: 55 },
      ],
    }),
    listWindows: vi.fn().mockResolvedValue({ windows: [] }),
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

// happy-dom lays nothing out, so a plain div measures 0×0 — which the
// composable rightly refuses to vote from. A pane meant to be visible
// stubs its box.
function paneHost(): HTMLElement {
  const host = document.createElement('div')
  Object.defineProperties(host, {
    clientWidth: { value: 800 },
    clientHeight: { value: 600 },
  })
  return host
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

// Window events carry the whole window; the attach fixture's windows are 213x55,
// so that is what an event leaves a size at unless it says otherwise.
function windowFrame(
  kind: string,
  windowId: string,
  window: { name?: string; active?: boolean; width?: number; height?: number } = {},
): ArrayBuffer {
  return jsonFrame(0x01, {
    kind,
    windowId,
    name: window.name ?? '',
    active: window.active ?? false,
    width: window.width ?? 213,
    height: window.height ?? 55,
  })
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
    loadedFaces = []
    xterm.FakeTerminal.instances = []
    xterm.FakeFitAddon.instances = []
    xterm.FakeWebglAddon.instances = []
    xterm.FakeCanvasAddon.instances = []
    xterm.FakeWebglAddon.unavailable = false
    xterm.FakeCanvasAddon.unavailable = false
    FakeResizeObserver.instances = []
    // The remembered vote outlives a composable on purpose, so each test states
    // its own starting memory rather than inheriting the last one's.
    localStorage.clear()
    globalThis.WebSocket = FakeSocket as unknown as typeof WebSocket
    globalThis.ResizeObserver = FakeResizeObserver as unknown as typeof ResizeObserver
    Object.defineProperty(document, 'fonts', {
      configurable: true,
      value: {
        load: vi.fn((font: string) => {
          loadedFaces.push(font)
          return Promise.resolve([])
        }),
      },
    })
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

  // 'live' only says the socket opened; the first-paint capture is still in
  // flight then, and the session switcher must not reveal a blank grid.
  it('flags the first paint once a terminal has processed output', async () => {
    const { session, socket } = await attached()
    expect(session.painted.value).toBe(false)

    socket.onmessage?.({ data: outputFrame('@1', '%1', 'hello') })
    const write = session.tabs.value[0].term.write as ReturnType<typeof vi.fn>
    expect(session.painted.value).toBe(false)
    write.mock.calls[0][1]() // xterm reports the chunk processed
    expect(session.painted.value).toBe(true)

    // Later output does not pay for the callback.
    socket.onmessage?.({ data: outputFrame('@1', '%1', 'more') })
    expect(write.mock.calls[1][1]).toBeUndefined()
  })

  it('maps window events onto the tab list', async () => {
    const { session, socket } = await attached()

    socket.onmessage?.({ data: windowFrame('added', '@3', { name: 'logs' }) })
    expect(session.tabs.value.map((tab) => tab.name)).toEqual(['agent', 'shell', 'logs'])

    socket.onmessage?.({ data: windowFrame('renamed', '@3', { name: 'tail' }) })
    expect(session.tabs.value[2].name).toBe('tail')

    socket.onmessage?.({ data: windowFrame('active-changed', '@2', { name: 'shell', active: true }) })
    expect(session.activeWindowId.value).toBe('@2')
    expect(session.tabs.value.map((tab) => tab.active)).toEqual([false, true, false])

    socket.onmessage?.({ data: windowFrame('closed', '@3', { name: 'tail' }) })
    expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@1', '@2'])
  })

  // Every client attached to a window renders the same grid, so the size a tab
  // renders at is whatever tmux reports — never what this pane measured.
  it('opens every terminal at the size tmux reported, before any output lands', async () => {
    const { session, socket } = await attached()

    for (const tab of session.tabs.value) {
      expect(tab.term.resize).toHaveBeenCalledWith(213, 55)
      expect(tab.term.cols).toBe(213)
      expect(tab.term.rows).toBe(55)
    }

    // A resize re-wraps the buffer, so one after the first paint would mangle
    // the snapshot tmux just drew.
    const term = xterm.FakeTerminal.instances[1]
    socket.onmessage?.({ data: outputFrame('@2', '%9', 'from the shell') })
    expect(term.resize.mock.invocationCallOrder[0]).toBeLessThan(term.write.mock.invocationCallOrder[0])
  })

  it('follows tmux on a resized event', async () => {
    const { session, socket } = await attached()

    socket.onmessage?.({ data: windowFrame('resized', '@2', { name: 'shell', width: 80, height: 24 }) })

    expect(session.tabs.value[1].term.resize).toHaveBeenLastCalledWith(80, 24)
    expect(session.tabs.value[0].term.resize).toHaveBeenLastCalledWith(213, 55)
  })

  it('takes the size off any window event, not just the resized one', async () => {
    const { session, socket } = await attached()

    socket.onmessage?.({ data: windowFrame('renamed', '@1', { name: 'agent', active: true, width: 100, height: 30 }) })
    expect(session.tabs.value[0].term.resize).toHaveBeenLastCalledWith(100, 30)

    // A %window-add placeholder has no size yet; the reconcile behind it does.
    socket.onmessage?.({ data: windowFrame('added', '@3', { name: '', width: 0, height: 0 }) })
    expect(xterm.FakeTerminal.instances[2].resize).toHaveBeenLastCalledWith(80, 24)
    socket.onmessage?.({ data: windowFrame('renamed', '@3', { name: 'logs', width: 213, height: 55 }) })
    expect(xterm.FakeTerminal.instances[2].resize).toHaveBeenLastCalledWith(213, 55)
  })

  it('disposes exactly the closed tab: its terminal, addon and resize observer', async () => {
    const { session, socket } = await attached()
    session.attachTab('@1', paneHost())
    session.attachTab('@2', paneHost())

    socket.onmessage?.({ data: windowFrame('closed', '@2', { name: 'shell' }) })

    expect(xterm.FakeTerminal.instances[1].dispose).toHaveBeenCalledTimes(1)
    expect(xterm.FakeFitAddon.instances[1].dispose).toHaveBeenCalledTimes(1)
    expect(FakeResizeObserver.instances[1].disconnected).toBe(true)
    expect(xterm.FakeTerminal.instances[1].onDataDisposed).toBe(true)

    expect(xterm.FakeTerminal.instances[0].dispose).not.toHaveBeenCalled()
    expect(xterm.FakeFitAddon.instances[0].dispose).not.toHaveBeenCalled()
    expect(FakeResizeObserver.instances[0].disconnected).toBe(false)
  })

  // xterm's DOM renderer cannot join box drawing or underlines across cells, so
  // an atlas renderer is the fix for #131 rather than any cell-metric tuning.
  it('loads the WebGL renderer, and only once the pane is open', async () => {
    const { session } = await attached()

    session.attachTab('@1', paneHost())

    const term = xterm.FakeTerminal.instances[0]
    expect(xterm.FakeWebglAddon.instances).toHaveLength(1)
    expect(xterm.FakeCanvasAddon.instances).toHaveLength(0)
    const loaded = term.loadAddon.mock.calls.findIndex((call) => call[0] instanceof xterm.FakeWebglAddon)
    expect(term.open.mock.invocationCallOrder[0])
      .toBeLessThan(term.loadAddon.mock.invocationCallOrder[loaded])
  })

  it('falls back to the canvas renderer where WebGL2 is missing', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    xterm.FakeWebglAddon.unavailable = true
    const { session } = await attached()

    session.attachTab('@1', paneHost())

    expect(xterm.FakeCanvasAddon.instances).toHaveLength(1)
    warn.mockRestore()
  })

  // Both atlas renderers gone leaves the DOM renderer, which draws with the
  // #131 seams — degraded, but never a pane that failed to open.
  it('still opens the pane when no renderer context exists at all', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    xterm.FakeWebglAddon.unavailable = true
    xterm.FakeCanvasAddon.unavailable = true
    const { session } = await attached()

    session.attachTab('@1', paneHost())

    expect(xterm.FakeTerminal.instances[0].open).toHaveBeenCalledTimes(1)
    expect(session.status.value).toBe('live')
    warn.mockRestore()
  })

  it('claims the canvas renderer when a WebGL context is lost for good', async () => {
    const { session } = await attached()
    session.attachTab('@1', paneHost())

    xterm.FakeWebglAddon.instances[0].loseContext()

    expect(xterm.FakeWebglAddon.instances[0].dispose).toHaveBeenCalledTimes(1)
    expect(xterm.FakeCanvasAddon.instances).toHaveLength(1)
  })

  // xterm disposes its core before its addons, and a renderer addon restores a
  // renderer as it goes, so it has to be disposed while the core is still up.
  it('disposes the renderer addon ahead of the terminal it renders', async () => {
    const { session } = await attached()
    session.attachTab('@1', paneHost())

    session.dispose()

    const addon = xterm.FakeWebglAddon.instances[0]
    const term = xterm.FakeTerminal.instances[0]
    expect(addon.dispose).toHaveBeenCalledTimes(1)
    expect(addon.dispose.mock.invocationCallOrder[0])
      .toBeLessThan(term.dispose.mock.invocationCallOrder[0])
  })

  // An atlas renderer caches the glyphs it rasterised, so a face that arrives
  // after the first paint stays wrong; xterm never re-measures on a font load.
  it('preloads the regular and bold faces before it attaches', async () => {
    const { client } = await attached()

    expect(loadedFaces).toEqual([
      `${terminalFontSizePx.medium}px 'JetBrainsMono Nerd Font'`,
      `bold ${terminalFontSizePx.medium}px 'JetBrainsMono Nerd Font'`,
    ])
    const load = (document.fonts.load as ReturnType<typeof vi.fn>)
    expect(load.mock.invocationCallOrder[1])
      .toBeLessThan(client.attach.mock.invocationCallOrder[0])
  })

  // Pinning either is the obvious-looking fix for #131 and is the wrong one:
  // both quantise to whole device pixels, and a lineHeight above 1 pads the
  // glyph away from the cell edge box drawing has to reach. ADR 0038.
  it('sets no lineHeight and no letterSpacing', async () => {
    await attached()

    for (const term of xterm.FakeTerminal.instances) {
      expect(term.options).not.toHaveProperty('lineHeight')
      expect(term.options).not.toHaveProperty('letterSpacing')
    }
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

  // The measurement is a vote sent to tmux, which may or may not honour it: it
  // must not touch the grid any terminal renders at.
  it('votes the measured size on a resize observation without resizing anything', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()

    session.attachTab('@1', paneHost())
    xterm.FakeFitAddon.instances[0].proposed = { cols: 120, rows: 40 }
    FakeResizeObserver.instances[0].trigger()
    await vi.advanceTimersByTimeAsync(100)

    expect(xterm.FakeFitAddon.instances[0].proposeDimensions).toHaveBeenCalled()
    expect(client.resize).toHaveBeenCalledWith('hive-abc', 120, 40)
    for (const term of xterm.FakeTerminal.instances) {
      expect(term.resize).toHaveBeenLastCalledWith(213, 55)
    }
  })

  // A size preset changes cell metrics, not the host box, so the observer never
  // fires: the composable itself must re-vote. The grid still belongs to tmux.
  it('applies a font size preset to every open terminal and re-votes', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()
    session.attachTab('@1', paneHost())
    await vi.advanceTimersByTimeAsync(100)
    client.resize.mockClear()
    xterm.FakeFitAddon.instances[0].proposed = { cols: 100, rows: 30 }

    setTerminalFontSize('xl')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(100)

    for (const term of xterm.FakeTerminal.instances) {
      expect(term.options.fontSize).toBe(terminalFontSizePx.xl)
    }
    expect(client.resize).toHaveBeenCalledWith('hive-abc', 100, 30)
    for (const term of xterm.FakeTerminal.instances) {
      expect(term.resize).toHaveBeenLastCalledWith(213, 55)
    }

    // currentSize is a module singleton; put the default back for later tests.
    setTerminalFontSize('medium')
    await flushPromises()
  })

  // The attach size is a vote tmux obeys, so attaching with a placeholder would
  // resize the session — and every other client attached to it — to a size
  // nothing had measured. Setting no size leaves it alone.
  it('attaches without a size when nothing has been measured', async () => {
    const client = fakeClient()
    await open(client).start()

    expect(client.attach).toHaveBeenCalledWith('hive-abc', 0, 0)
  })

  it('attaches with the size this app window last voted, and does not re-vote it', async () => {
    vi.useFakeTimers()
    localStorage.setItem('hive.terminal.vote', JSON.stringify({ cols: 120, rows: 40, fontPx: terminalFontSizePx.medium }))
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()

    expect(client.attach).toHaveBeenCalledWith('hive-abc', 120, 40)

    session.attachTab('@1', paneHost())
    xterm.FakeFitAddon.instances[0].proposed = { cols: 120, rows: 40 }
    await vi.advanceTimersByTimeAsync(100)

    expect(client.resize).not.toHaveBeenCalled()
  })

  it('remembers a measured vote for the next session it attaches', async () => {
    vi.useFakeTimers()
    const first = fakeClient()
    const session = open(first)
    await session.start()
    await flushPromises()
    session.attachTab('@1', paneHost())
    xterm.FakeFitAddon.instances[0].proposed = { cols: 213, rows: 55 }
    FakeResizeObserver.instances[0].trigger()
    await vi.advanceTimersByTimeAsync(100)

    const second = fakeClient()
    await open(second).start()

    expect(second.attach).toHaveBeenCalledWith('hive-abc', 213, 55)
  })

  // A vote tmux never granted means another attached client decided the size.
  // Nothing announces that, so the difference is the only signal there is.
  it('reports the size tmux granted when it does not match the vote', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()
    session.attachTab('@1', paneHost())

    xterm.FakeFitAddon.instances[0].proposed = { cols: 300, rows: 80 }
    FakeResizeObserver.instances[0].trigger()
    await vi.advanceTimersByTimeAsync(100)
    expect(session.sizeConstraint.value).toBeNull()

    // tmux answers a vote it honours with a window event; silence past the
    // settle window is what says some other client won.
    await vi.advanceTimersByTimeAsync(1000)
    expect(session.sizeConstraint.value).toEqual({ voted: { cols: 300, rows: 80 }, granted: { cols: 213, rows: 55 } })

    session.dismissSizeConstraint()
    expect(session.sizeConstraint.value).toBeNull()
    FakeResizeObserver.instances[0].trigger()
    await vi.advanceTimersByTimeAsync(1000)
    expect(session.sizeConstraint.value).toBeNull()
  })

  it('clears the constraint once tmux grants the voted size', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()
    sockets[0].onopen?.()
    session.attachTab('@1', paneHost())
    xterm.FakeFitAddon.instances[0].proposed = { cols: 300, rows: 80 }
    FakeResizeObserver.instances[0].trigger()
    await vi.advanceTimersByTimeAsync(1100)
    expect(session.sizeConstraint.value).not.toBeNull()

    sockets[0].onmessage?.({ data: windowFrame('resized', '@1', { name: 'agent', active: true, width: 300, height: 80 }) })
    await vi.advanceTimersByTimeAsync(1000)

    expect(session.sizeConstraint.value).toBeNull()
  })

  it('votes nothing when the pane cannot be measured', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()

    session.attachTab('@1', paneHost())
    xterm.FakeFitAddon.instances[0].proposed = undefined
    FakeResizeObserver.instances[0].trigger()
    await vi.advanceTimersByTimeAsync(100)

    expect(client.resize).not.toHaveBeenCalled()
  })

  // A pooled session's panes sit behind display:none while another session is
  // shown, and a hidden box still yields a small "valid" proposal — WebKit
  // answers the specified '100%' for it, which FitAddon parses as 100px. A
  // vote from there squeezes the session's windows to ~8×4 for every client.
  it('never votes from a pane with no rendered box', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()

    session.attachTab('@1', document.createElement('div'))
    xterm.FakeFitAddon.instances[0].proposed = { cols: 8, rows: 4 }
    FakeResizeObserver.instances[0].trigger()
    await vi.advanceTimersByTimeAsync(100)

    expect(client.resize).not.toHaveBeenCalled()
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
    session.attachTab('@1', paneHost())

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
    socket.onmessage?.({ data: windowFrame('added', '@3', { name: 'logs' }) })

    expect(session.activeWindowId.value).toBe('@3')
  })

  it('refocuses the pane when the active window is reselected', async () => {
    const { client, session } = await attached()
    const term = xterm.FakeTerminal.instances[0]
    term.focus.mockClear()

    await session.select('@1')

    expect(term.focus).toHaveBeenCalled()
    expect(client.selectWindow).not.toHaveBeenCalled()
  })

  it('tracks whether the active window is scrolled above the live tail', async () => {
    const { session } = await attached()
    const term = xterm.FakeTerminal.instances[0]

    expect(session.tabs.value[0].scrolledUp).toBe(false)
    term.scrollTo(5, 12)
    expect(session.tabs.value[0].scrolledUp).toBe(true)

    // Entering the alternate screen (a full-screen TUI) has no scrollback and
    // fires no scroll event, so the buffer switch is what clears the flag.
    term.buffer.active = { viewportY: 0, baseY: 0 }
    term.switchBuffer()
    expect(session.tabs.value[0].scrolledUp).toBe(false)
  })

  it('scrolls the active window back to the tail and refocuses it', async () => {
    const { session } = await attached()
    const term = xterm.FakeTerminal.instances[0]
    term.scrollTo(5, 12)
    term.focus.mockClear()

    session.scrollToBottom()

    expect(term.scrollToBottom).toHaveBeenCalled()
    expect(term.focus).toHaveBeenCalled()
    expect(session.tabs.value[0].scrolledUp).toBe(false)
  })

  it('reports a failed control action without ending the session', async () => {
    const { client, session } = await attached()
    client.newWindow.mockRejectedValue(new Error('tmux refused'))

    await session.newWindow()

    expect(session.actionError.value).toBe('tmux refused')
    expect(session.status.value).toBe('live')
  })
})
