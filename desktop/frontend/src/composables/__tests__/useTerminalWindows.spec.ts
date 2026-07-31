import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ISearchOptions } from '@xterm/addon-search'
import { resetTerminalFacesForTests, useTerminalWindows } from '../useTerminalWindows'
import {
  defaultTerminalFontWeight,
  defaultTerminalFontWeightBold,
  setTerminalFontFamily,
  setTerminalFontSize,
  setTerminalFontWeight,
  terminalFontSizePx,
} from '../useTerminalFont'
import { TERMINAL_FONT, terminalFontStack } from '../../lib/terminalFaces'
import { TerminalRequestError, type TerminalClient } from '../../lib/terminalClient'

const xterm = vi.hoisted(() => {
  class FakeTerminal {
    cols = 80
    rows = 24
    options: Record<string, unknown> = {}
    write = vi.fn()
    focus = vi.fn()
    open = vi.fn()
    loadAddon = vi.fn((addon: { activate?: (term: FakeTerminal) => void }) => addon.activate?.(this))
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
    private keyHandler?: (event: KeyboardEvent) => boolean

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

    attachCustomKeyEventHandler(handler: (event: KeyboardEvent) => boolean) {
      this.keyHandler = handler
    }

    press(event: Partial<KeyboardEvent>): boolean {
      return this.keyHandler?.({ type: 'keydown', ...event } as KeyboardEvent) ?? true
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

  class FakeWebLinksAddon {
    static instances: FakeWebLinksAddon[] = []
    dispose = vi.fn()
    activate = vi.fn()

    constructor(readonly handler: (event: MouseEvent, uri: string) => void) {
      FakeWebLinksAddon.instances.push(this)
    }
  }

  // The real addon reports hit counts through onDidChangeResults, so the fake
  // has to be driven the same way: a find only produces a count if it fires.
  // It also reproduces the one way a find fails outright — highlighting goes
  // through Terminal.registerDecoration, which is proposed API and throws on a
  // terminal that did not opt in, so a search that highlights is dead without
  // it and no assertion about the results would ever be reached.
  class FakeSearchAddon {
    static instances: FakeSearchAddon[] = []
    static results: { resultIndex: number; resultCount: number } = { resultIndex: 0, resultCount: 1 }
    dispose = vi.fn()
    activate = vi.fn((term: FakeTerminal) => { this.proposedApi = term.options.allowProposedApi === true })
    clearDecorations = vi.fn()
    findNext = vi.fn((term: string, options?: ISearchOptions) => this.record('next', term, options))
    findPrevious = vi.fn((term: string, options?: ISearchOptions) => this.record('previous', term, options))
    calls: { mode: string; term: string; options?: ISearchOptions }[] = []
    private proposedApi = false
    private resultHandlers: ((results: { resultIndex: number; resultCount: number }) => void)[] = []

    constructor() { FakeSearchAddon.instances.push(this) }

    onDidChangeResults(handler: (results: { resultIndex: number; resultCount: number }) => void) {
      this.resultHandlers.push(handler)
      return { dispose: () => {} }
    }

    private record(mode: string, term: string, options?: ISearchOptions): boolean {
      if (options?.decorations && !this.proposedApi) {
        throw new Error('You must set the allowProposedApi option to true to use proposed API')
      }
      this.calls.push({ mode, term, options })
      for (const handler of this.resultHandlers) handler(FakeSearchAddon.results)
      return true
    }
  }

  return { FakeTerminal, FakeFitAddon, FakeWebglAddon, FakeCanvasAddon, FakeSearchAddon, FakeWebLinksAddon }
})

const wails = vi.hoisted(() => ({ OpenURL: vi.fn(() => Promise.resolve()) }))

vi.mock('@xterm/xterm', () => ({ Terminal: xterm.FakeTerminal }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: xterm.FakeFitAddon }))
vi.mock('@xterm/addon-search', () => ({ SearchAddon: xterm.FakeSearchAddon }))
vi.mock('@xterm/addon-webgl', () => ({ WebglAddon: xterm.FakeWebglAddon }))
vi.mock('@xterm/addon-canvas', () => ({ CanvasAddon: xterm.FakeCanvasAddon }))
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: xterm.FakeWebLinksAddon }))
vi.mock('@wailsio/runtime', () => ({ Browser: { OpenURL: wails.OpenURL } }))

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
    start: vi.fn().mockResolvedValue({ started: true }),
    kill: vi.fn().mockResolvedValue({ killed: true }),
    listWindows: vi.fn().mockResolvedValue({ windows: [] }),
    resize: vi.fn().mockResolvedValue(undefined),
    newWindow: vi.fn().mockResolvedValue({ windowId: '@3' }),
    closeWindow: vi.fn().mockResolvedValue(undefined),
    renameWindow: vi.fn().mockResolvedValue(undefined),
    moveWindow: vi.fn().mockResolvedValue({ windows: [] }),
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
// term.open() builds .xterm-viewport inside the host and the composable hangs
// its scroll listener off it. The fake Terminal opens nothing, so the host has
// to stand in for what xterm would have put there.
function paneHost(): HTMLElement {
  const host = document.createElement('div')
  const viewport = document.createElement('div')
  viewport.className = 'xterm-viewport'
  host.appendChild(viewport)
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
    resetTerminalFacesForTests()
    xterm.FakeTerminal.instances = []
    xterm.FakeFitAddon.instances = []
    xterm.FakeWebglAddon.instances = []
    xterm.FakeCanvasAddon.instances = []
    xterm.FakeSearchAddon.instances = []
    xterm.FakeSearchAddon.results = { resultIndex: 0, resultCount: 1 }
    xterm.FakeWebglAddon.unavailable = false
    xterm.FakeCanvasAddon.unavailable = false
    xterm.FakeWebLinksAddon.instances = []
    wails.OpenURL.mockClear()
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

  // A reconcile reports the deactivated window too, in tmux index order — so
  // selecting a lower-indexed window ends on the one that just lost the flag.
  // Following it would hand the selection straight back.
  it('ignores an active-changed for a window tmux says is not active', async () => {
    const { session, socket } = await attached()

    socket.onmessage?.({ data: windowFrame('active-changed', '@2', { name: 'shell', active: true }) })
    expect(session.activeWindowId.value).toBe('@2')

    socket.onmessage?.({ data: windowFrame('active-changed', '@1', { name: 'agent', active: true }) })
    socket.onmessage?.({ data: windowFrame('active-changed', '@2', { name: 'shell', active: false }) })

    expect(session.activeWindowId.value).toBe('@1')
    expect(session.tabs.value.map((tab) => tab.active)).toEqual([true, false])
  })

  // The webview answers neither half of xterm's default OSC 8 activation, so
  // every link kind is routed out to the OS browser instead.
  it('opens both hyperlink kinds through the system browser', async () => {
    const { session } = await attached()

    const handler = session.tabs.value[0].term.options.linkHandler as { activate: (event: MouseEvent, uri: string) => void }
    handler.activate(new MouseEvent('click'), 'https://example.test/osc8')
    expect(wails.OpenURL).toHaveBeenCalledWith('https://example.test/osc8')

    xterm.FakeWebLinksAddon.instances[0].handler(new MouseEvent('click'), 'https://example.test/bare')
    expect(wails.OpenURL).toHaveBeenCalledWith('https://example.test/bare')
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

  // Every pooled session mounts a pane per window, so claiming a context at
  // mount spends one per background tab and walks the page past WebKit's
  // limit — where the context it costs is some other pane's. ADR 0045.
  it('claims a renderer for the window on screen, and for the rest on activation', async () => {
    const { session } = await attached()

    session.attachTab('@1', paneHost())
    session.attachTab('@2', paneHost())

    expect(xterm.FakeWebglAddon.instances).toHaveLength(1)

    await session.select('@2')

    expect(xterm.FakeWebglAddon.instances).toHaveLength(2)
  })

  // The canvas claim is all that stands between a lost context and the DOM
  // renderer, so a failed one must not strand the pane there for good.
  it('retries the renderer on the next activation when the canvas claim failed', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { session } = await attached()
    session.attachTab('@1', paneHost())
    session.attachTab('@2', paneHost())

    xterm.FakeCanvasAddon.unavailable = true
    xterm.FakeWebglAddon.instances[0].loseContext()
    expect(xterm.FakeCanvasAddon.instances).toHaveLength(0)

    xterm.FakeCanvasAddon.unavailable = false
    await session.select('@2')
    await session.select('@1')

    expect(xterm.FakeWebglAddon.instances).toHaveLength(3)
    warn.mockRestore()
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
  // Both weights and the italic of each: xterm measures its cell once on open
  // and the atlas caches whatever was resident, so a face arriving later stays
  // wrong until the atlas is cleared. ADR 0038.
  it('preloads every face a pane can draw with before it attaches', async () => {
    const { client } = await attached()

    const stack = terminalFontStack('')
    const px = terminalFontSizePx.medium
    expect(loadedFaces).toEqual([
      `${defaultTerminalFontWeight} ${px}px ${stack}`,
      `italic ${defaultTerminalFontWeight} ${px}px ${stack}`,
      `${defaultTerminalFontWeightBold} ${px}px ${stack}`,
      `italic ${defaultTerminalFontWeightBold} ${px}px ${stack}`,
    ])
    const load = (document.fonts.load as ReturnType<typeof vi.fn>)
    expect(load.mock.invocationCallOrder.at(-1))
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

  // #181: normal cells were locked to whatever the renderer drew, with no way
  // to ask for a lighter face.
  it('opens every terminal at the configured font weights', async () => {
    await attached()

    for (const term of xterm.FakeTerminal.instances) {
      expect(term.options.fontWeight).toBe(defaultTerminalFontWeight)
      expect(term.options.fontWeightBold).toBe(defaultTerminalFontWeightBold)
    }
  })

  // Weight moves the advance width the same way size does, so the grid has to
  // be re-voted for — and the face has to be resident before xterm re-measures
  // against it, or it measures the outgoing one.
  it('applies a font weight to every open terminal and re-votes', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()
    session.attachTab('@1', paneHost())
    await vi.advanceTimersByTimeAsync(100)
    client.resize.mockClear()
    loadedFaces.length = 0
    xterm.FakeFitAddon.instances[0].proposed = { cols: 100, rows: 30 }

    setTerminalFontWeight(700)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(100)

    for (const term of xterm.FakeTerminal.instances) {
      expect(term.options.fontWeight).toBe(700)
    }
    expect(loadedFaces).toContain(`700 ${terminalFontSizePx.medium}px ${terminalFontStack('')}`)
    expect(client.resize).toHaveBeenCalledWith('hive-abc', 100, 30)

    setTerminalFontWeight(defaultTerminalFontWeight)
    await flushPromises()
  })

  // A chosen family leads the stack and the bundled face backs it, so a font
  // the OS reports but the webview cannot resolve degrades to the shipped one
  // rather than to whatever `monospace` happens to be.
  it('applies a font family to every open terminal, keeping the bundled face as fallback', async () => {
    vi.useFakeTimers()
    const client = fakeClient()
    const session = open(client)
    await session.start()
    await flushPromises()
    session.attachTab('@1', paneHost())
    await vi.advanceTimersByTimeAsync(100)

    setTerminalFontFamily('Menlo')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(100)

    for (const term of xterm.FakeTerminal.instances) {
      expect(term.options.fontFamily).toBe(terminalFontStack('Menlo'))
      expect(term.options.fontFamily).toContain(TERMINAL_FONT)
    }

    setTerminalFontFamily(TERMINAL_FONT)
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

  it('separates a session that is not running from an attach that failed', async () => {
    const client = fakeClient()
    client.attach.mockRejectedValue(new TerminalRequestError('session "hive-abc" is not running', 'not_found'))
    const session = open(client)

    await session.start()

    // Not a fault: the view turns this into the panel offering to start it.
    expect(session.endReason.value).toBe('not-started')
    expect(session.error.value).toBe('session "hive-abc" is not running')
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

  // A wheel notch is about three rows. Offering the way back the moment the
  // viewport moves at all would put a pill on screen for a nudge, and take it
  // away again before it finished appearing.
  it('waits for a scroll worth calling a scroll before offering the way back', async () => {
    const { session } = await attached()
    const term = xterm.FakeTerminal.instances[0]

    term.scrollTo(96, 100)
    expect(session.tabs.value[0].scrolledUp).toBe(false)

    term.scrollTo(94, 100)
    expect(session.tabs.value[0].scrolledUp).toBe(true)
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

  describe('reordering windows', () => {
    // tmux reports the whole order back, and it is the one that lands: this
    // client's guess is only what fills the gap while the round trip runs.
    it('reorders on the drop and settles on the order tmux answers with', async () => {
      const { client, session } = await attached()
      let resolve: (value: { windows: { windowId: string }[] }) => void = () => {}
      client.moveWindow.mockReturnValue(new Promise((r) => { resolve = r }))

      const moving = session.moveWindow('@1', 1)
      expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@2', '@1'])
      expect(client.moveWindow).toHaveBeenCalledWith('hive-abc', '@1', 1)

      resolve({ windows: [{ windowId: '@1' }, { windowId: '@2' }] })
      await moving

      expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@1', '@2'])
      expect(session.actionError.value).toBeNull()
    })

    it('keeps the terminals and the selection across a move', async () => {
      const { client, session } = await attached()
      client.moveWindow.mockResolvedValue({ windows: [{ windowId: '@2' }, { windowId: '@1' }] })
      const terminals = session.tabs.value.map((tab) => tab.term)

      await session.moveWindow('@1', 1)

      expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@2', '@1'])
      expect(session.tabs.value.map((tab) => tab.term)).toEqual([terminals[1], terminals[0]])
      expect(session.activeWindowId.value).toBe('@1')
      expect(terminals[0].dispose).not.toHaveBeenCalled()
    })

    it('puts the strip back and names the failure when tmux refuses', async () => {
      const { client, session } = await attached()
      client.moveWindow.mockRejectedValue(new Error('tmux refused the move'))

      await session.moveWindow('@1', 1)

      expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@1', '@2'])
      expect(session.actionError.value).toBe('tmux refused the move')
      expect(session.status.value).toBe('live')
    })

    // A window opened while the move was in flight is tmux's news, not part of
    // what the refusal was about.
    it('rolls back the order without dropping a window that arrived meanwhile', async () => {
      const { client, session, socket } = await attached()
      let reject: (reason: Error) => void = () => {}
      client.moveWindow.mockReturnValue(new Promise((_, r) => { reject = r }))

      const moving = session.moveWindow('@1', 1)
      socket.onmessage?.({ data: windowFrame('added', '@3', { name: 'logs' }) })
      reject(new Error('tmux refused the move'))
      await moving

      expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@1', '@2', '@3'])
    })

    it('never asks tmux for a move that changes nothing', async () => {
      const { client, session } = await attached()

      await session.moveWindow('@1', 0)
      await session.moveWindow('@1', 5)
      await session.moveWindow('@404', 1)

      expect(client.moveWindow).not.toHaveBeenCalled()
      expect(session.tabs.value.map((tab) => tab.windowId)).toEqual(['@1', '@2'])
    })
  })

  it('reports a failed control action without ending the session', async () => {
    const { client, session } = await attached()
    client.newWindow.mockRejectedValue(new Error('tmux refused'))

    await session.newWindow()

    expect(session.actionError.value).toBe('tmux refused')
    expect(session.status.value).toBe('live')
  })

  // xterm suppresses onScroll for a wheel, a trackpad and a dragged scrollbar:
  // its viewport syncs the buffer from the DOM scroll and swallows the event.
  // Without the DOM listener nothing notices the viewport leaving the tail on an
  // idle session, and the way back is never offered.
  it('offers the way back to the tail when the user scrolls, which xterm does not announce', async () => {
    const { session } = await attached()
    const host = paneHost()
    session.attachTab('@1', host)
    const term = xterm.FakeTerminal.instances[0]
    const viewport = host.querySelector('.xterm-viewport')!

    term.buffer.active.viewportY = 40
    term.buffer.active.baseY = 120
    viewport.dispatchEvent(new Event('scroll'))
    expect(session.tabs.value[0].scrolledUp).toBe(true)

    term.buffer.active.viewportY = 120
    viewport.dispatchEvent(new Event('scroll'))
    expect(session.tabs.value[0].scrolledUp).toBe(false)
  })

  it('searches the active window and reports the hit it is on', async () => {
    const { session } = await attached()
    xterm.FakeSearchAddon.results = { resultIndex: 2, resultCount: 9 }

    session.openSearch()
    session.setSearchQuery('panic')

    expect(session.search.value.open).toBe(true)
    // resultIndex is 0-based; the bar counts from 1.
    expect(session.search.value).toMatchObject({ query: 'panic', matches: 9, index: 3 })
    const finder = xterm.FakeSearchAddon.instances[0]
    expect(finder.calls.at(-1)).toMatchObject({ mode: 'next', term: 'panic' })

    session.findPrevious()
    expect(finder.calls.at(-1)).toMatchObject({ mode: 'previous', term: 'panic' })
  })

  // A hit count is only true of the buffer it was counted in, so carrying one
  // across tabs would put a number on the new window that never matched it.
  it('re-runs the search against the window it switches to', async () => {
    const { session } = await attached()
    session.openSearch()
    session.setSearchQuery('panic')

    await session.select('@2')

    const [first, second] = xterm.FakeSearchAddon.instances
    expect(first.clearDecorations).toHaveBeenCalled()
    expect(second.calls.at(-1)).toMatchObject({ term: 'panic' })
  })

  it('opens the find bar from the pane and keeps the combo off the wire', async () => {
    const { session, socket } = await attached()
    const term = xterm.FakeTerminal.instances[0]

    expect(term.press({ key: 'f', ctrlKey: true })).toBe(true)
    expect(session.search.value.open).toBe(false)

    expect(term.press({ key: 'f', metaKey: true })).toBe(false)
    expect(session.search.value.open).toBe(true)
    expect(socket.sent).toHaveLength(0)
  })

  it('drops the query and the highlights when the bar closes', async () => {
    const { session } = await attached()
    session.openSearch()
    session.setSearchQuery('panic')

    session.closeSearch()

    expect(session.search.value).toEqual({ open: false, query: '', matches: 0, index: 0 })
    expect(xterm.FakeSearchAddon.instances[0].clearDecorations).toHaveBeenCalled()
    expect(xterm.FakeTerminal.instances[0].focus).toHaveBeenCalled()
  })
})
