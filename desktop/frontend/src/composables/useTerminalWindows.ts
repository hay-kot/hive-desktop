import { effectScope, markRaw, nextTick, ref, watch, type Ref } from 'vue'
import { CanvasAddon } from '@xterm/addon-canvas'
import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal, type IDisposable, type ITerminalAddon } from '@xterm/xterm'
// Rides the async terminal chunk on purpose: ~10MB of glyphs nobody pays for
// until they open Terminal mode.
import '../assets/fonts/jetbrains-mono-nerd.css'
import {
  decodeFrame,
  encodeInputFrames,
  type TerminalClient,
  type WindowEventKind,
  type WindowState,
} from '../lib/terminalClient'
import { xtermTheme } from '../lib/terminalTheme'
import { useTerminalFont } from './useTerminalFont'
import { useTheme } from './useTheme'

/**
 * 'ended' is terminal: the control client is gone and the only way forward is
 * reconnect(), which re-attaches from scratch.
 */
export type TerminalStatus = 'connecting' | 'live' | 'ended'

/** Why the session ended, so the UI can say which of the two signals fired. */
export type TerminalEndReason = 'attach-failed' | 'exited' | 'error' | 'disconnected'

export interface TerminalWindowTab {
  // Unique per Terminal instance, not per tmux window: reconnect rebuilds the
  // terminals under the same window ids, and a v-for keyed on the window id
  // would reuse the host element and never open the new Terminal on it.
  uid: number
  windowId: string
  name: string
  active: boolean
  term: Terminal
  fit: FitAddon
}

export interface UseTerminalWindows {
  tabs: Ref<TerminalWindowTab[]>
  activeWindowId: Ref<string>
  status: Ref<TerminalStatus>
  endReason: Ref<TerminalEndReason | null>
  error: Ref<string | null>
  actionError: Ref<string | null>
  start: () => Promise<void>
  reconnect: () => Promise<void>
  select: (windowId: string) => Promise<void>
  newWindow: () => Promise<void>
  closeWindow: (windowId: string) => Promise<void>
  rename: (windowId: string, name: string) => Promise<void>
  attachTab: (windowId: string, host: HTMLElement) => void
  disposeTab: (windowId: string) => void
  dispose: () => void
}

// The size a window renders at until tmux reports its own. Every path that
// learns tmux's size overrides it.
const DEFAULT_SIZE = { cols: 80, rows: 24 }
const RESIZE_DEBOUNCE_MS = 80

// The face xterm measures its cell from. The rest of the stack only covers the
// window between a Terminal opening and this one resolving.
const TERMINAL_FONT = "'JetBrainsMono Nerd Font'"

interface TabRuntime {
  host?: HTMLElement
  observer?: ResizeObserver
  disposers: IDisposable[]
}

let nextTabUID = 1

export function useTerminalWindows(slug: string, client: TerminalClient): UseTerminalWindows {
  const tabs = ref<TerminalWindowTab[]>([]) as Ref<TerminalWindowTab[]>
  const activeWindowId = ref('')
  const status = ref<TerminalStatus>('connecting')
  const endReason = ref<TerminalEndReason | null>(null)
  const error = ref<string | null>(null)
  const actionError = ref<string | null>(null)

  const runtime = new Map<string, TabRuntime>()
  const scope = effectScope(true)
  let socket: WebSocket | null = null
  let disposed = false
  // The last size this client voted for. tmux sizes a window to the *smallest*
  // attached client, so this is a request, never the size anything renders at.
  let vote = { ...DEFAULT_SIZE }
  let resizeTimer: ReturnType<typeof setTimeout> | undefined
  // A window created from the toolbar is only knowable by id once tmux
  // announces it, so the intent to focus it is parked until then.
  let pendingActivate = ''

  const { px: fontSizePx } = useTerminalFont()

  scope.run(() => {
    const { theme } = useTheme()
    watch(theme, () => {
      const palette = xtermTheme()
      for (const tab of tabs.value) tab.term.options.theme = palette
    })
    // New cell metrics change how many cells fit the same box, so the vote
    // must re-run; the grid itself stays at tmux's size until tmux answers.
    watch(fontSizePx, (px) => {
      for (const tab of tabs.value) tab.term.options.fontSize = px
      scheduleVote()
    })
  })

  function findTab(windowId: string): TerminalWindowTab | undefined {
    return tabs.value.find((tab) => tab.windowId === windowId)
  }

  function createTab(state: WindowState): TerminalWindowTab {
    // lineHeight and letterSpacing are unset on purpose: every renderer
    // quantises both to whole device pixels, so neither can move the cell onto
    // a cleaner boundary, and a lineHeight above 1 pads the glyph away from the
    // cell edge box drawing has to meet. ADR 0038.
    const term = markRaw(new Terminal({
      fontFamily: `${TERMINAL_FONT}, 'IBM Plex Mono', ui-monospace, monospace`,
      fontSize: fontSizePx.value,
      scrollback: 5000,
      theme: xtermTheme(),
    }))
    const fit = markRaw(new FitAddon())
    term.loadAddon(fit)
    // Before any output reaches it: xterm re-wraps its buffer on resize, so a
    // grid sized after the first paint mangles the snapshot it just drew.
    term.resize(state.width || DEFAULT_SIZE.cols, state.height || DEFAULT_SIZE.rows)
    runtime.set(state.windowId, {
      disposers: [term.onData((data: string) => sendInput(state.windowId, data))],
    })
    return { uid: nextTabUID++, windowId: state.windowId, name: state.name, active: state.active, term, fit }
  }

  // applySize holds a terminal to tmux's size for its window. A 0 means tmux has
  // not reported one — a %window-add placeholder, say — and the reconcile behind
  // it carries the real size a moment later.
  function applySize(tab: TerminalWindowTab, width: number, height: number): void {
    if (!width || !height) return
    if (tab.term.cols === width && tab.term.rows === height) return
    tab.term.resize(width, height)
  }

  function sendInput(windowId: string, data: string): void {
    if (!socket || socket.readyState !== WebSocket.OPEN) return
    for (const frame of encodeInputFrames(windowId, data)) socket.send(frame)
  }

  function setActive(windowId: string): void {
    activeWindowId.value = windowId
    for (const tab of tabs.value) tab.active = tab.windowId === windowId
  }

  function attachTab(windowId: string, host: HTMLElement): void {
    const tab = findTab(windowId)
    const state = runtime.get(windowId)
    if (!tab || !state || state.host) return
    state.host = host
    tab.term.open(host)
    // After open(), never before: an unopened Terminal defers addon activation
    // to its own open(), which would throw a missing-context error out of there
    // rather than out of the load, past the fallback below.
    loadRenderer(state, tab.term)
    const observer = new ResizeObserver(() => scheduleVote())
    observer.observe(host)
    state.observer = observer
    if (tab.windowId === activeWindowId.value) tab.term.focus()
    scheduleVote()
  }

  function scheduleVote(): void {
    clearTimeout(resizeTimer)
    resizeTimer = setTimeout(voteSize, RESIZE_DEBOUNCE_MS)
  }

  // The measurement is a vote, not a resize: it says how big a grid this pane
  // could show, and tmux answers with the size it actually gave the window
  // (%layout-change -> a window event). proposeDimensions rather than fit()
  // because fit() would resize the Terminal itself, which is tmux's call.
  function voteSize(): void {
    const tab = findTab(activeWindowId.value)
    if (!tab || !runtime.get(tab.windowId)?.host) return
    const proposed = tab.fit.proposeDimensions()
    if (!proposed?.cols || !proposed.rows) return
    if (proposed.cols === vote.cols && proposed.rows === vote.rows) return
    vote = { cols: proposed.cols, rows: proposed.rows }
    void client.resize(slug, vote.cols, vote.rows).catch((e: unknown) => {
      actionError.value = message(e, 'Could not resize the terminal.')
    })
  }

  function handleFrame(payload: unknown): void {
    if (!(payload instanceof ArrayBuffer) && !(payload instanceof Uint8Array)) return
    const frame = decodeFrame(payload)
    if (!frame) return

    switch (frame.type) {
      case 'output':
        findTab(frame.windowId)?.term.write(frame.data)
        break
      case 'window':
        applyWindowEvent(frame.kind, frame.state)
        break
      case 'lifecycle':
        if (frame.kind === 'exited') end('exited', frame.message || 'The tmux session ended.')
        else if (frame.kind === 'error') end('error', frame.message || 'The terminal client failed.')
        break
    }
  }

  // Every window event carries the whole window, so the size is taken from all
  // of them rather than from 'resized' alone — a reconcile reports one change
  // per window and its kind may be any of these.
  function applyWindowEvent(kind: WindowEventKind, state: WindowState): void {
    const { windowId } = state
    switch (kind) {
      case 'added': {
        if (findTab(windowId)) return
        tabs.value = [...tabs.value, createTab(state)]
        if (state.active || tabs.value.length === 1 || pendingActivate === windowId) {
          pendingActivate = ''
          setActive(windowId)
          void nextTick(() => scheduleVote())
        }
        return
      }
      case 'closed':
        disposeTab(windowId)
        return
      case 'renamed': {
        const tab = findTab(windowId)
        if (tab) tab.name = state.name
        break
      }
      case 'active-changed':
        if (findTab(windowId)) setActive(windowId)
        break
      case 'resized':
        break
    }
    const tab = findTab(windowId)
    if (tab) applySize(tab, state.width, state.height)
  }

  function openSocket(): void {
    socket = client.openStream(slug)
    socket.binaryType = 'arraybuffer'
    socket.onopen = () => { if (!disposed) status.value = 'live' }
    socket.onmessage = (event: MessageEvent) => handleFrame(event.data)
    socket.onclose = () => dropped()
    socket.onerror = () => dropped()
  }

  // A transport drop is a distinct signal from a lifecycle end: tmux may still
  // be attached, but this webview has lost the stream either way.
  function dropped(): void {
    if (disposed || status.value === 'ended') return
    end('disconnected', 'The terminal stream disconnected.')
  }

  function end(reason: TerminalEndReason, detail: string): void {
    if (disposed) return
    status.value = 'ended'
    endReason.value = reason
    error.value = detail
    closeSocket()
  }

  function closeSocket(): void {
    if (!socket) return
    socket.onopen = null
    socket.onmessage = null
    socket.onclose = null
    socket.onerror = null
    try {
      socket.close()
    } catch {
      // a socket that never opened throws on close; nothing left to clean up
    }
    socket = null
  }

  function disposeTab(windowId: string): void {
    const state = runtime.get(windowId)
    if (state) {
      state.observer?.disconnect()
      for (const disposer of state.disposers) disposer.dispose()
      runtime.delete(windowId)
    }
    const tab = findTab(windowId)
    if (tab) {
      tab.fit.dispose()
      tab.term.dispose()
      tabs.value = tabs.value.filter((other) => other.windowId !== windowId)
    }
    if (activeWindowId.value === windowId) setActive(tabs.value[0]?.windowId ?? '')
  }

  function disposeTabs(): void {
    for (const tab of [...tabs.value]) disposeTab(tab.windowId)
  }

  async function start(): Promise<void> {
    status.value = 'connecting'
    endReason.value = null
    error.value = null
    actionError.value = null
    try {
      await loadTerminalFaces(fontSizePx.value)
      const { windows } = await client.attach(slug, vote.cols, vote.rows)
      if (disposed) return
      tabs.value = windows.map(createTab)
      setActive(windows.find((window) => window.active)?.windowId ?? windows[0]?.windowId ?? '')
      openSocket()
    } catch (e) {
      end('attach-failed', message(e, 'Could not attach to this session.'))
    }
  }

  // Reconnect always builds new terminals. After an overflow the backend tears
  // its client down and re-attaches with a fresh capture, so a reused terminal
  // would paint that capture over a stale screen.
  async function reconnect(): Promise<void> {
    closeSocket()
    disposeTabs()
    await start()
  }

  async function select(windowId: string): Promise<void> {
    if (!findTab(windowId) || activeWindowId.value === windowId) return
    setActive(windowId)
    await nextTick()
    findTab(windowId)?.term.focus()
    scheduleVote()
    await control(() => client.selectWindow(slug, windowId), 'Could not select that window.')
  }

  async function newWindow(): Promise<void> {
    const created = await control(() => client.newWindow(slug), 'Could not create a window.')
    if (created?.windowId) {
      if (findTab(created.windowId)) setActive(created.windowId)
      else pendingActivate = created.windowId
    }
  }

  async function closeWindow(windowId: string): Promise<void> {
    await control(() => client.closeWindow(slug, windowId), 'Could not close that window.')
  }

  async function rename(windowId: string, name: string): Promise<void> {
    await control(() => client.renameWindow(slug, windowId, name), 'Could not rename that window.')
  }

  async function control<T>(run: () => Promise<T>, fallback: string): Promise<T | undefined> {
    actionError.value = null
    try {
      return await run()
    } catch (e) {
      actionError.value = message(e, fallback)
      return undefined
    }
  }

  function dispose(): void {
    if (disposed) return
    disposed = true
    clearTimeout(resizeTimer)
    closeSocket()
    disposeTabs()
    scope.stop()
    // Intentional teardown releases the control client; only an unexpected drop
    // leaves tmux attached (ADR 0036 / decision D5).
    void client.detach(slug).catch(() => {})
  }

  return {
    tabs, activeWindowId, status, endReason, error, actionError,
    start, reconnect, select, newWindow, closeWindow, rename, attachTab, disposeTab, dispose,
  }
}

function message(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

// xterm's DOM renderer paints box drawing from the font's own glyphs and
// underlines as text-decoration on per-cell inline-block spans, so neither can
// join across cells at any font size or device pixel ratio. An atlas renderer
// strokes both to the cell's own device-pixel bounds, so one is loaded wherever
// a context for it exists, and the DOM renderer is only ever the last resort.
// ADR 0038.
function loadRenderer(state: TabRuntime, term: Terminal): void {
  const webgl = loadRendererAddon(state, term, () => new WebglAddon())
  if (!webgl) {
    loadRendererAddon(state, term, () => new CanvasAddon())
    return
  }
  // Fires only when the browser did not restore the context on its own. The
  // addon puts the DOM renderer back as it goes, so claim the canvas instead.
  webgl.onContextLoss(() => {
    webgl.dispose()
    loadRendererAddon(state, term, () => new CanvasAddon())
  })
}

function loadRendererAddon<T extends ITerminalAddon>(
  state: TabRuntime,
  term: Terminal,
  create: () => T,
): T | undefined {
  try {
    const addon = create()
    term.loadAddon(addon)
    // Disposed with the tab and ahead of the Terminal: xterm disposes its core
    // before its addons, and this one restores a renderer on the way out.
    state.disposers.push(addon)
    return addon
  } catch (error) {
    console.warn('Terminal renderer unavailable, falling back', error)
    return undefined
  }
}

// xterm measures its cell when a Terminal opens and never re-measures when a
// face arrives later, and an atlas renderer caches the glyphs it rasterised
// from whatever was resident — so bold has to be here too, not just regular.
async function loadTerminalFaces(px: number): Promise<void> {
  await Promise.all([`${px}px`, `bold ${px}px`].map(
    (font) => document.fonts?.load(`${font} ${TERMINAL_FONT}`).catch(() => {}),
  ))
}
