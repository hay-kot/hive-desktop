import { effectScope, markRaw, nextTick, ref, watch, type Ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import { CanvasAddon } from '@xterm/addon-canvas'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon, type ISearchOptions } from '@xterm/addon-search'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal, type IDisposable, type ILinkHandler, type ITerminalAddon } from '@xterm/xterm'
// Rides the async terminal chunk on purpose: ~10MB of glyphs nobody pays for
// until they open Terminal mode.
import {
  decodeFrame,
  encodeInputFrames,
  TerminalRequestError,
  type TerminalClient,
  type WindowEventKind,
  type WindowState,
} from '../lib/terminalClient'
import { loadTerminalFaces, TERMINAL_FONT_STACK, resetTerminalFacesForTests } from '../lib/terminalFaces'
import { searchHighlightColors, xtermTheme } from '../lib/terminalTheme'
import { useTerminalFont } from './useTerminalFont'
import { useTheme } from './useTheme'

/**
 * 'ended' is terminal: the control client is gone and the only way forward is
 * reconnect(), which re-attaches from scratch.
 */
export type TerminalStatus = 'connecting' | 'live' | 'ended'

/**
 * Why the session ended, so the UI can say which of the signals fired.
 * 'not-started' is the one that is not a failure: tmux is running no session
 * under this slug yet, and starting it is an action the view offers.
 */
export type TerminalEndReason = 'not-started' | 'attach-failed' | 'exited' | 'error' | 'disconnected'

export interface TerminalSize {
  cols: number
  rows: number
}

/**
 * tmux answered a size vote with a different grid, so this pane is rendering a
 * window some other client's size decided. Not an error — the rule needs
 * naming, or it reads as a rendering bug.
 */
export interface TerminalSizeConstraint {
  voted: TerminalSize
  granted: TerminalSize
}

export interface TerminalWindowTab {
  // Unique per Terminal instance, not per tmux window: reconnect rebuilds the
  // terminals under the same window ids, and a v-for keyed on the window id
  // would reuse the host element and never open the new Terminal on it.
  uid: number
  windowId: string
  name: string
  active: boolean
  // The viewport sits above the live tail, so new output lands below the fold.
  scrolledUp: boolean
  term: Terminal
  fit: FitAddon
}

/**
 * The find bar, which searches one window at a time: the active tab's buffer,
 * scrollback included. `matches` is -1 when there are more than the addon will
 * highlight, and `index` is the 1-based position of the current match, 0 for
 * none.
 */
export interface TerminalSearch {
  open: boolean
  query: string
  matches: number
  index: number
}

export interface UseTerminalWindows {
  tabs: Ref<TerminalWindowTab[]>
  activeWindowId: Ref<string>
  status: Ref<TerminalStatus>
  // True once a terminal has processed output. 'live' is not enough to swap a
  // held pane onto this session: the socket opens before the first-paint
  // capture lands, and swapping then shows a blank grid for a frame.
  painted: Ref<boolean>
  endReason: Ref<TerminalEndReason | null>
  error: Ref<string | null>
  actionError: Ref<string | null>
  sizeConstraint: Ref<TerminalSizeConstraint | null>
  dismissSizeConstraint: () => void
  search: Ref<TerminalSearch>
  openSearch: () => void
  closeSearch: () => void
  setSearchQuery: (query: string) => void
  findNext: () => void
  findPrevious: () => void
  start: () => Promise<void>
  reconnect: () => Promise<void>
  select: (windowId: string) => Promise<void>
  newWindow: () => Promise<void>
  closeWindow: (windowId: string) => Promise<void>
  rename: (windowId: string, name: string) => Promise<void>
  attachTab: (windowId: string, host: HTMLElement) => void
  disposeTab: (windowId: string) => void
  focusActive: () => void
  scrollToBottom: () => void
  dispose: () => void
}

// The grid a window renders at while tmux has reported none of its own, and
// only then. Every path that learns tmux's size overrides it.
const DEFAULT_SIZE: TerminalSize = { cols: 80, rows: 24 }
const RESIZE_DEBOUNCE_MS = 80
// How long tmux's answer to a vote is waited for before the difference is
// reported as a constraint. A vote is answered by a window event, which arrives
// well inside this; an unanswered vote means something else decided the size.
const CONSTRAINT_SETTLE_MS = 750

// A link has to leave the webview: it hosts one document for the app's whole
// lifetime, and xterm's own default for an OSC 8 hyperlink — confirm() then
// window.open() — is answered by neither, so a click on one does nothing at
// all. WebLinksAddon covers the bare URLs xterm does not linkify on its own.
function openLink(uri: string): void {
  void Browser.OpenURL(uri).catch(() => {})
}

const linkHandler: ILinkHandler = { activate: (_event, uri) => openLink(uri) }

interface TabRuntime {
  host?: HTMLElement
  observer?: ResizeObserver
  finder: SearchAddon
  disposers: IDisposable[]
  // An atlas renderer is live on this terminal. False after a context loss the
  // canvas claim did not survive, which is what makes the next activation
  // retry instead of leaving the pane on the DOM renderer. ADR 0045.
  rendered?: boolean
}

// How far off the live tail the viewport has to be before the way back is
// offered. One wheel notch is about three rows, so a nudge — or the row of
// drift a trackpad leaves behind — does not flash a pill at anyone; a scroll
// meant as a scroll does.
const TAIL_SLACK_ROWS = 5

// The pane box belongs to the app window, not to a session, so one remembered
// vote serves every session — including one being attached for the first time.
// It is keyed by font size because the cell metrics, and so the vote, change
// with the preset.
const VOTE_KEY = 'hive.terminal.vote'
// tmux's own bound on a client dimension. A stored value outside it would fail
// the attach, and the session would land on the error overlay instead.
const MAX_DIMENSION = 1000

let nextTabUID = 1

function rememberedVote(fontPx: number): TerminalSize | null {
  try {
    const stored = JSON.parse(localStorage.getItem(VOTE_KEY) ?? 'null') as
      { cols?: number; rows?: number; fontPx?: number } | null
    if (!stored || stored.fontPx !== fontPx) return null
    if (!validDimension(stored.cols) || !validDimension(stored.rows)) return null
    return { cols: stored.cols, rows: stored.rows }
  } catch {
    // unparseable or unreadable storage is simply no memory
    return null
  }
}

function rememberVote(size: TerminalSize, fontPx: number): void {
  try {
    localStorage.setItem(VOTE_KEY, JSON.stringify({ ...size, fontPx }))
  } catch {
    // storage denied; the vote is re-measured next attach either way
  }
}

function validDimension(value: number | undefined): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 && value <= MAX_DIMENSION
}

export function useTerminalWindows(slug: string, client: TerminalClient): UseTerminalWindows {
  const tabs = ref<TerminalWindowTab[]>([]) as Ref<TerminalWindowTab[]>
  const activeWindowId = ref('')
  const status = ref<TerminalStatus>('connecting')
  const painted = ref(false)
  const endReason = ref<TerminalEndReason | null>(null)
  const error = ref<string | null>(null)
  const actionError = ref<string | null>(null)
  const sizeConstraint = ref<TerminalSizeConstraint | null>(null)
  const search = ref<TerminalSearch>({ open: false, query: '', matches: 0, index: 0 })

  const runtime = new Map<string, TabRuntime>()
  const scope = effectScope(true)
  let socket: WebSocket | null = null
  let disposed = false

  const { px: fontSizePx } = useTerminalFont()

  // The last size this client voted for: a request, never the size anything
  // renders at. It opens at the last measured vote, and null — nothing measured
  // and nothing remembered — is a real state rather than a placeholder, because
  // tmux obeys the attach vote and would resize the session to it.
  let vote: TerminalSize | null = rememberedVote(fontSizePx.value)
  let resizeTimer: ReturnType<typeof setTimeout> | undefined
  let constraintTimer: ReturnType<typeof setTimeout> | undefined
  let constraintDismissed = false
  // A window created from the toolbar is only knowable by id once tmux
  // announces it, so the intent to focus it is parked until then.
  let pendingActivate = ''

  scope.run(() => {
    const { theme } = useTheme()
    watch(theme, () => {
      const palette = xtermTheme()
      for (const tab of tabs.value) tab.term.options.theme = palette
      // A decoration keeps the colour it was drawn with, so live highlights
      // would stay in the old theme until the next keystroke.
      if (search.value.open) runSearch('incremental')
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
      fontFamily: TERMINAL_FONT_STACK,
      fontSize: fontSizePx.value,
      linkHandler,
      scrollback: 5000,
      theme: xtermTheme(),
      // registerDecoration is still proposed API, and every find highlights
      // through it — without this the first findNext throws and search is dead.
      allowProposedApi: true,
    }))
    const fit = markRaw(new FitAddon())
    term.loadAddon(fit)
    term.loadAddon(markRaw(new WebLinksAddon((_event, uri) => openLink(uri))))
    const finder = markRaw(new SearchAddon())
    term.loadAddon(finder)
    // Before any output reaches it: xterm re-wraps its buffer on resize, so a
    // grid sized after the first paint mangles the snapshot it just drew.
    term.resize(state.width || unreportedSize().cols, state.height || unreportedSize().rows)
    term.attachCustomKeyEventHandler((event: KeyboardEvent) => {
      if (event.type !== 'keydown' || !isSearchCombo(event)) return true
      openSearch()
      return false
    })
    runtime.set(state.windowId, {
      finder,
      disposers: [
        finder,
        term.onData((data: string) => sendInput(state.windowId, data)),
        // onScroll covers what output does to the buffer — the auto-pin to the
        // tail, and a trim moving it — but *not* the user scrolling: xterm's
        // viewport syncs the buffer from its own DOM scroll handler and
        // suppresses the event to avoid feeding itself. attachTab listens to
        // that DOM scroll for the other half.
        term.onScroll(() => refreshScrolledUp(state.windowId)),
        // Entering the alternate screen has no scrollback and fires no scroll
        // event on the way in.
        term.buffer.onBufferChange(() => refreshScrolledUp(state.windowId)),
        // Fires as output lands too, not just on a new query: a match count is
        // only true of the buffer it was counted in.
        finder.onDidChangeResults(({ resultIndex, resultCount }) => {
          if (activeWindowId.value !== state.windowId) return
          search.value = { ...search.value, matches: resultCount, index: resultIndex + 1 }
        }),
      ],
    })
    return { uid: nextTabUID++, windowId: state.windowId, name: state.name, active: state.active, scrolledUp: false, term, fit }
  }

  // Reads through findTab so the reactive proxy is mutated, not the raw object
  // createTab returned — a raw write would leave the pill stale.
  function refreshScrolledUp(windowId: string): void {
    const tab = findTab(windowId)
    if (!tab) return
    const buffer = tab.term.buffer.active
    tab.scrolledUp = buffer.baseY - buffer.viewportY > TAIL_SLACK_ROWS
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
    if (activeWindowId.value !== windowId) clearHighlights()
    activeWindowId.value = windowId
    for (const tab of tabs.value) tab.active = tab.windowId === windowId
    showRenderer(windowId)
    // A search belongs to the buffer it ran against, so switching tabs re-runs
    // it rather than carrying the old window's hit count onto the new one.
    if (search.value.open) runSearch('incremental')
  }

  // A GL context is claimed when a window is first shown, not when its pane
  // mounts. Mounting covers every window of every pooled session, which spends
  // a context per background tab and pushes WebKit past its per-page limit on
  // each attach — and the pane it then kills is somebody else's. ADR 0045.
  function showRenderer(windowId: string): void {
    const state = runtime.get(windowId)
    const tab = findTab(windowId)
    // No host yet means the pane has not mounted; attachTab claims it there.
    if (!state?.host || !tab || state.rendered) return
    loadRenderer(state, tab.term)
  }

  // ─── Find ────────────────────────────────────────────────────────────────
  // One bar over one window: the active tab's buffer, scrollback included.

  function openSearch(): void {
    search.value = { ...search.value, open: true }
    runSearch('incremental')
  }

  function closeSearch(): void {
    clearHighlights()
    search.value = { open: false, query: '', matches: 0, index: 0 }
    focusActive()
  }

  function setSearchQuery(query: string): void {
    search.value = { ...search.value, query }
    runSearch('incremental')
  }

  function findNext(): void { runSearch('next') }
  function findPrevious(): void { runSearch('previous') }

  // 'incremental' keeps the viewport on the match it is already showing while
  // the query is still being typed; the other two are the user stepping.
  function runSearch(mode: 'incremental' | 'next' | 'previous'): void {
    const finder = runtime.get(activeWindowId.value)?.finder
    if (!finder) return
    if (!search.value.query) {
      finder.clearDecorations()
      search.value = { ...search.value, matches: 0, index: 0 }
      return
    }
    const highlight = searchHighlightColors()
    const options: ISearchOptions = {
      incremental: mode === 'incremental',
      decorations: {
        matchBackground: highlight.match,
        matchOverviewRuler: highlight.match,
        activeMatchBackground: highlight.active,
        activeMatchColorOverviewRuler: highlight.active,
      },
    }
    if (mode === 'previous') finder.findPrevious(search.value.query, options)
    else finder.findNext(search.value.query, options)
  }

  function clearHighlights(): void {
    for (const state of runtime.values()) state.finder.clearDecorations()
  }

  function attachTab(windowId: string, host: HTMLElement): void {
    const tab = findTab(windowId)
    const state = runtime.get(windowId)
    if (!tab || !state || state.host) return
    state.host = host
    tab.term.open(host)
    const observer = new ResizeObserver(() => scheduleVote())
    observer.observe(host)
    state.observer = observer
    watchViewportScroll(state, windowId, host)
    if (tab.windowId === activeWindowId.value) {
      // After open(), never before: an unopened Terminal defers addon
      // activation to its own open(), which would throw a missing-context
      // error out of there rather than out of the load, past the fallback.
      showRenderer(windowId)
      tab.term.focus()
    }
    scheduleVote()
  }

  // The only signal that the user scrolled. xterm's own onScroll is suppressed
  // on this path — the viewport reads its DOM scrollTop, syncs the buffer, and
  // swallows the event so it cannot feed itself — so a wheel, a trackpad or a
  // dragged scrollbar moves the viewport off the tail silently, and nothing
  // would ever offer the way back. The element only exists after open(), and
  // xterm's own listener is registered inside it, so ours runs second and reads
  // a buffer already synced.
  function watchViewportScroll(state: TabRuntime, windowId: string, host: HTMLElement): void {
    const viewport = host.querySelector('.xterm-viewport')
    if (!viewport) return
    const onScroll = (): void => refreshScrolledUp(windowId)
    viewport.addEventListener('scroll', onScroll, { passive: true })
    state.disposers.push({ dispose: () => viewport.removeEventListener('scroll', onScroll) })
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
    const host = tab ? runtime.get(tab.windowId)?.host : undefined
    if (!tab || !host) return
    // A pane inside a display:none subtree — every pooled session behind the
    // shown one — has no rendered box, but proposeDimensions still produces a
    // tiny "valid" grid there: getComputedStyle answers the specified '100%',
    // which FitAddon parses as 100px. Voting it would squeeze every window of
    // the session to ~8×4 and force the TUI inside to reflow, then reflow
    // back on reveal — the repaint the pool exists to avoid.
    if (!host.clientWidth || !host.clientHeight) return
    const proposed = tab.fit.proposeDimensions()
    if (!proposed?.cols || !proposed.rows) return
    scheduleConstraintCheck()
    if (vote && proposed.cols === vote.cols && proposed.rows === vote.rows) return
    vote = { cols: proposed.cols, rows: proposed.rows }
    rememberVote(vote, fontSizePx.value)
    void client.resize(slug, vote.cols, vote.rows).catch((e: unknown) => {
      actionError.value = message(e, 'Could not resize the terminal.')
    })
  }

  // The grid a tab opens at while tmux has reported no size for its window — a
  // window created from the toolbar lands at the pane's size rather than
  // snapping to it when the reconcile behind it arrives.
  function unreportedSize(): TerminalSize {
    return vote ?? DEFAULT_SIZE
  }

  function scheduleConstraintCheck(): void {
    clearTimeout(constraintTimer)
    constraintTimer = setTimeout(checkSizeConstraint, CONSTRAINT_SETTLE_MS)
  }

  // A vote tmux did not grant means another client decided this window's size.
  // The pane then renders a grid that does not match its box, which reads as a
  // rendering bug unless the rule is named.
  function checkSizeConstraint(): void {
    const tab = findTab(activeWindowId.value)
    if (!tab || !vote || constraintDismissed) {
      sizeConstraint.value = null
      return
    }
    const granted = { cols: tab.term.cols, rows: tab.term.rows }
    sizeConstraint.value = granted.cols === vote.cols && granted.rows === vote.rows
      ? null
      : { voted: { ...vote }, granted }
  }

  // Dismissal lasts as long as this attach: the constraint is a property of the
  // other client, so re-raising it on the next resize would nag about something
  // already read and understood.
  function dismissSizeConstraint(): void {
    constraintDismissed = true
    clearTimeout(constraintTimer)
    sizeConstraint.value = null
  }

  function handleFrame(payload: unknown): void {
    if (!(payload instanceof ArrayBuffer) && !(payload instanceof Uint8Array)) return
    const frame = decodeFrame(payload)
    if (!frame) return

    switch (frame.type) {
      case 'output': {
        const tab = findTab(frame.windowId)
        if (!tab) break
        // The callback fires once xterm has processed the chunk, which is the
        // earliest moment this attach has a screen worth revealing.
        if (painted.value) tab.term.write(frame.data)
        else tab.term.write(frame.data, () => { painted.value = true })
        break
      }
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
      // The kind reports "this window's active flag or pane changed", not
      // "this window is now the session's", and a reconcile emits one for the
      // window that just *lost* the flag as well — in tmux index order, so
      // selecting a lower-indexed window lands the deactivated one last.
      // Following that would put the selection back where it came from.
      case 'active-changed':
        if (state.active && findTab(windowId)) setActive(windowId)
        break
      case 'resized':
        break
    }
    const tab = findTab(windowId)
    if (tab) applySize(tab, state.width, state.height)
    scheduleConstraintCheck()
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
      // 0x0 sets no client size at all: tmux ignores a control client until it
      // sets one, so the session keeps the size its other clients gave it.
      const { windows } = await client.attach(slug, vote?.cols ?? 0, vote?.rows ?? 0)
      if (disposed) return
      tabs.value = windows.map(createTab)
      setActive(windows.find((window) => window.active)?.windowId ?? windows[0]?.windowId ?? '')
      openSocket()
    } catch (e) {
      // The core classifies "tmux is running no such session" rather than
      // letting a dead control stream's message stand in for it, so this is a
      // kind check, never a message match.
      if (e instanceof TerminalRequestError && e.kind === 'not_found') {
        end('not-started', e.message)
        return
      }
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
    if (!findTab(windowId)) return
    // Reselecting the active window is still an intent to type into it: the
    // click just moved DOM focus onto the tab, so hand it back to the pane.
    if (activeWindowId.value === windowId) {
      focusActive()
      return
    }
    setActive(windowId)
    await nextTick()
    findTab(windowId)?.term.focus()
    scheduleVote()
    await control(() => client.selectWindow(slug, windowId), 'Could not select that window.')
  }

  function focusActive(): void {
    findTab(activeWindowId.value)?.term.focus()
  }

  function scrollToBottom(): void {
    const tab = findTab(activeWindowId.value)
    if (!tab) return
    tab.term.scrollToBottom()
    tab.term.focus()
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
    clearTimeout(constraintTimer)
    closeSocket()
    disposeTabs()
    scope.stop()
    // Intentional teardown releases the control client; only an unexpected drop
    // leaves tmux attached (ADR 0036 / decision D5).
    void client.detach(slug).catch(() => {})
  }

  return {
    tabs, activeWindowId, status, painted, endReason, error, actionError, sizeConstraint, dismissSizeConstraint,
    search, openSearch, closeSearch, setSearchQuery, findNext, findPrevious,
    start, reconnect, select, newWindow, closeWindow, rename, attachTab, disposeTab, focusActive, scrollToBottom, dispose,
  }
}

// Cmd+F on macOS, Ctrl+Shift+F everywhere else — the convention every terminal
// emulator settled on, and for the reason they settled on it: a bare Ctrl+F is
// readline's forward-char and belongs to the pane, not to us.
function isSearchCombo(event: KeyboardEvent): boolean {
  if (event.key !== 'f' && event.key !== 'F') return false
  if (event.ctrlKey) return event.shiftKey && !event.metaKey
  return event.metaKey && !event.altKey
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
    state.rendered = claimCanvas(state, term)
    return
  }
  state.rendered = true
  // Fires only when the browser did not restore the context on its own. The
  // addon puts the DOM renderer back as it goes, so claim the canvas instead.
  webgl.onContextLoss(() => {
    webgl.dispose()
    state.rendered = claimCanvas(state, term)
  })
}

// The canvas claim is the whole of what stands between a lost context and the
// renderer #131 is about, so a failed one is recorded rather than swallowed:
// showRenderer tries again the next time the pane is shown.
function claimCanvas(state: TabRuntime, term: Terminal): boolean {
  return loadRendererAddon(state, term, () => new CanvasAddon()) !== undefined
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

// Re-exported so the pane's own tests keep reaching it through the composable
// they exercise.
export { resetTerminalFacesForTests }
