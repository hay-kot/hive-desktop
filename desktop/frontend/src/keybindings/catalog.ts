import { computed, ref, type Component, type ComputedRef } from 'vue'
import IconArrowDown from '~icons/lucide/arrow-down'
import IconArrowUp from '~icons/lucide/arrow-up'
import IconBot from '~icons/lucide/bot'
import IconBug from '~icons/lucide/bug'
import IconChevronLeft from '~icons/lucide/chevron-left'
import IconChevronRight from '~icons/lucide/chevron-right'
import IconCommand from '~icons/lucide/command'
import IconExternalLink from '~icons/lucide/external-link'
import IconEye from '~icons/lucide/eye'
import IconMailCheck from '~icons/lucide/mail-check'
import IconMinus from '~icons/lucide/minus'
import IconPanelLeft from '~icons/lucide/panel-left'
import IconPanelRight from '~icons/lucide/panel-right'
import IconPlus from '~icons/lucide/plus'
import IconRefreshCw from '~icons/lucide/refresh-cw'
import IconSearch from '~icons/lucide/search'
import IconSquarePlus from '~icons/lucide/square-plus'
import IconTerminal from '~icons/lucide/terminal'
import IconX from '~icons/lucide/x'

// The single declarative source of truth for *bindable* commands — the stable
// app actions a user can rebind from Settings ▸ Keybindings and that also seed
// the command palette. Dynamic palette entries (switch profile, select feed,
// jump to node) are data, not commands, and are NOT bindable, so they live in
// App.vue's palette registration rather than here.
//
// Launchers are the one exception, and they are commands rather than data: a
// `terminal-popup` action exists to be reached by a chord of its own, so each
// one contributes a `launcher.<action-id>` command through setLauncherCommands
// below. Read `commands` — not `commandCatalog` — anywhere the answer has to
// include them.
//
// `context` gates where a bare (modifier-less) binding fires: `feed` commands
// only run when the feed is actually on screen, `terminal` commands only inside
// terminal mode, `terminal-session` commands only while a session is attached
// there, `agents` commands only inside the Agents area; `global` commands run
// anywhere.
// `defaultCombos` are canonical combo strings (see useKeybindings.comboFromEvent)
// — an empty array means "bindable, but unbound by default".
//
// A combo resolves to exactly one command — the first in this list that claims
// it — so `context` narrows *when* a command fires, it does not let two
// commands share a chord. Widget-local navigation (the palette's own ↑↓, the
// session tree's) is therefore not modelled here: it would have to fight the
// feed's `j`/`k` for the same combo. Those keys stay handlers on the widget
// that owns focus.
// `terminal-session` is the only context nothing in the static catalog below
// claims: it belongs to a launcher that opens in a session's checkout, which is
// a command whose whole meaning is the session it runs in (ADR quick-terminal-launchers-are-session-scoped).
export type CommandContext = 'global' | 'feed' | 'terminal' | 'terminal-session' | 'agents'

export interface BindableCommand {
  id: string
  /** Human label shown in the palette row and the keybindings editor. */
  title: string
  /** Section header, shared with the palette grouping. */
  group: string
  /** Extra palette match terms. */
  keywords?: string[]
  /** Leading icon for the palette row / settings row. */
  icon?: Component
  /** Canonical default combos; `[]` = bindable but unbound. */
  defaultCombos: string[]
  context: CommandContext
  /** Omit from the command palette (still bindable + listed in settings). */
  paletteHidden?: boolean
  /**
   * Fires over a focused terminal pane, and on the terminal escape chord rather
   * than on the binding alone — Command on macOS, Ctrl+Shift where there is no
   * Command (useKeybindings.terminalEscapeCombo). Without it the pane keeps the
   * key, which is what leaves Ctrl+T as readline's transpose on a platform
   * where `mod` is Ctrl.
   */
  escapesPane?: boolean
}

// How far the digit row reaches. A session with more windows than this is
// walked from the tree; there is no chord for the tenth.
const DIRECT_WINDOW_COMMANDS = 9
const WINDOW_PREFIX = 'terminal.select-window-'

function terminalWindowCommandID(position: number): string {
  return WINDOW_PREFIX + position
}

/** The 1-based window a jump command names, or null for any other command. */
export function terminalWindowPosition(commandID: string): number | null {
  if (!commandID.startsWith(WINDOW_PREFIX)) return null
  const position = Number(commandID.slice(WINDOW_PREFIX.length))
  return Number.isInteger(position) && position >= 1 && position <= DIRECT_WINDOW_COMMANDS ? position : null
}

// Generated rather than written out: nine entries that differ in a digit, and
// one implementation behind them. They are palette-hidden because the palette
// does not filter by context — nine rows naming windows that do not exist would
// otherwise sit in it on the feed.
const windowJumpCommands: BindableCommand[] = Array.from({ length: DIRECT_WINDOW_COMMANDS }, (_, index) => {
  const position = index + 1
  return {
    id: terminalWindowCommandID(position),
    title: `Go to window ${position}`,
    group: 'Terminal',
    keywords: ['terminal', 'window', 'tab', 'switch', String(position)],
    icon: IconTerminal,
    defaultCombos: [`mod+${position}`],
    context: 'terminal',
    paletteHidden: true,
  }
})

export const commandCatalog: BindableCommand[] = [
  {
    id: 'feed.next',
    title: 'Next item',
    group: 'Feeds',
    keywords: ['down', 'next', 'navigate'],
    icon: IconArrowDown,
    defaultCombos: ['j', 'arrowdown'],
    context: 'feed',
  },
  {
    id: 'feed.prev',
    title: 'Previous item',
    group: 'Feeds',
    keywords: ['up', 'previous', 'navigate'],
    icon: IconArrowUp,
    defaultCombos: ['k', 'arrowup'],
    context: 'feed',
  },
  {
    id: 'feed.open-in-browser',
    title: 'Open item in browser',
    group: 'Feeds',
    keywords: ['open', 'browser', 'github', 'link'],
    icon: IconExternalLink,
    defaultCombos: ['o', 'enter'],
    context: 'feed',
  },
  {
    id: 'feed.toggle-unread',
    title: 'Toggle unread filter',
    group: 'Feeds',
    keywords: ['unread', 'filter'],
    icon: IconEye,
    defaultCombos: ['u'],
    context: 'feed',
  },
  {
    id: 'feed.toggle-preview',
    title: 'Toggle preview pane',
    group: 'Feeds',
    keywords: ['preview', 'detail', 'pane', 'panel', 'reading', 'close'],
    icon: IconPanelRight,
    defaultCombos: ['p'],
    context: 'feed',
  },
  { id: 'feed.toggle-archive', title: 'Archive / unarchive item', group: 'Feeds', defaultCombos: ['e'], context: 'feed' },
  { id: 'feed.mark-unread', title: 'Mark unread', group: 'Feeds', defaultCombos: ['shift+u'], context: 'feed' },
  // Scoped to the selected feed; a no-op in Trash, which carries no unread
  // semantics. The workspace variant stays unbound by default: it clears every
  // feed at once and there is no undo, so it should be asked for by name.
  {
    id: 'feed.mark-all-read',
    title: 'Mark all as read',
    group: 'Feeds',
    keywords: ['read', 'unread', 'clear', 'catch up', 'bulk'],
    icon: IconMailCheck,
    defaultCombos: ['shift+a'],
    context: 'feed',
  },
  {
    id: 'feed.mark-workspace-read',
    title: 'Mark all feeds as read',
    group: 'Feeds',
    keywords: ['read', 'unread', 'clear', 'catch up', 'bulk', 'workspace', 'everything'],
    icon: IconMailCheck,
    defaultCombos: [],
    context: 'feed',
  },
  {
    id: 'feed.refresh',
    title: 'Refresh feeds',
    group: 'Feeds',
    keywords: ['reload', 'sync'],
    icon: IconRefreshCw,
    defaultCombos: ['r'],
    context: 'feed',
  },
  {
    id: 'palette.toggle',
    title: 'Command palette',
    group: 'General',
    keywords: ['search', 'commands', 'palette'],
    icon: IconCommand,
    defaultCombos: ['mod+k'],
    context: 'global',
    paletteHidden: true,
    escapesPane: true,
  },
  {
    id: 'session.new',
    title: 'New session…',
    group: 'General',
    keywords: ['session', 'create', 'hive', 'agent', 'launch', 'new'],
    icon: IconSquarePlus,
    defaultCombos: ['mod+shift+n'],
    context: 'global',
  },
  {
    id: 'terminal.popup.toggle',
    title: 'Terminal pop-up',
    group: 'General',
    keywords: ['terminal', 'shell', 'popup', 'console', 'run', 'command', 'lazygit'],
    icon: IconTerminal,
    defaultCombos: ['mod+`'],
    context: 'global',
  },
  // Directional rather than one toggle: which pane you land on should be
  // readable off the chord, not off where focus happened to be.
  //
  // Only the sidebar half has to escape a focused pane, so it is the one chord
  // terminal mode takes away from tmux (App.vue's dispatcher, and xterm's own
  // handler in useTerminalWindows). `mod` is Cmd on macOS and Ctrl elsewhere,
  // where Ctrl+← is readline's backward-word — rebind it there if the pane
  // needs it back.
  {
    id: 'terminal.focus-sidebar',
    title: 'Focus session tree',
    group: 'Terminal',
    keywords: ['terminal', 'sidebar', 'sessions', 'tree', 'focus', 'left'],
    icon: IconPanelLeft,
    defaultCombos: ['mod+arrowleft'],
    context: 'terminal',
  },
  {
    id: 'terminal.focus-pane',
    title: 'Focus terminal',
    group: 'Terminal',
    keywords: ['terminal', 'pane', 'focus', 'right'],
    icon: IconPanelRight,
    defaultCombos: ['mod+arrowright'],
    context: 'terminal',
  },
  // Bare `/`, the way every list this is modelled on spells it. A focused pane
  // keeps the key — it is a character — so this fires from the tree, which is
  // where a search for a session starts anyway.
  {
    id: 'terminal.focus-filter',
    title: 'Filter sessions',
    group: 'Terminal',
    keywords: ['terminal', 'filter', 'search', 'find', 'session'],
    icon: IconSearch,
    defaultCombos: ['/'],
    context: 'terminal',
  },
  // The window lifecycle, on the chords a terminal emulator already spells them
  // with: ⌘T, ⌘W, and ⌘⇧] / ⌘⇧[ to walk the list. They escape a focused pane
  // rather than piercing it, so where `mod` is Ctrl the pane keeps Ctrl+T for
  // readline and the app answers Ctrl+Shift+T instead.
  //
  // `mod+}` is not a slip for `mod+shift+]`: Shift already changed the
  // character, so the combo names `}` (see useKeybindings.shouldRecordShift),
  // and that one spelling is what ⌘⇧] and Ctrl+Shift+] both produce.
  {
    id: 'terminal.new-window',
    title: 'New window',
    group: 'Terminal',
    keywords: ['terminal', 'window', 'tab', 'new', 'create', 'open'],
    icon: IconPlus,
    defaultCombos: ['mod+t'],
    context: 'terminal',
    escapesPane: true,
  },
  {
    id: 'terminal.close-window',
    title: 'Close window',
    group: 'Terminal',
    keywords: ['terminal', 'window', 'tab', 'close', 'kill'],
    icon: IconX,
    defaultCombos: ['mod+w'],
    context: 'terminal',
    escapesPane: true,
  },
  {
    id: 'terminal.next-window',
    title: 'Next window',
    group: 'Terminal',
    keywords: ['terminal', 'window', 'tab', 'next', 'cycle', 'switch'],
    icon: IconChevronRight,
    defaultCombos: ['mod+}'],
    context: 'terminal',
    escapesPane: true,
  },
  {
    id: 'terminal.prev-window',
    title: 'Previous window',
    group: 'Terminal',
    keywords: ['terminal', 'window', 'tab', 'previous', 'cycle', 'switch'],
    icon: IconChevronLeft,
    defaultCombos: ['mod+{'],
    context: 'terminal',
    escapesPane: true,
  },
  // A position in the window strip, not a tmux window index: the strip is what
  // is on screen, and tmux's indices have gaps as soon as a window is closed.
  ...windowJumpCommands,
  // The Agents area is a plain two-level list beside a pane, not a tree, so it
  // needs only the pair terminal mode's focus chords have — no filter, no
  // window jumps. Combos are new ones, not terminal.*'s: a combo resolves to
  // exactly one command, so reusing mod+arrowleft/-right here would shadow
  // whichever command claims it first rather than binding both.
  {
    id: 'agents.focus-sidebar',
    title: 'Focus workspace list',
    group: 'Agents',
    keywords: ['agents', 'workspace', 'sidebar', 'list', 'focus', 'left'],
    icon: IconBot,
    defaultCombos: ['mod+shift+arrowleft'],
    context: 'agents',
  },
  {
    id: 'agents.focus-pane',
    title: 'Focus session',
    group: 'Agents',
    keywords: ['agents', 'session', 'pane', 'focus', 'right'],
    icon: IconBot,
    defaultCombos: ['mod+shift+arrowright'],
    context: 'agents',
  },
  {
    id: 'report.open',
    title: 'Report a problem',
    group: 'General',
    keywords: ['bug', 'issue', 'feedback', 'diagnostics', 'crash', 'report'],
    icon: IconBug,
    defaultCombos: ['mod+shift+b'],
    context: 'global',
  },
  {
    id: 'window.hide',
    title: 'Hide window',
    group: 'Window',
    keywords: ['minimize', 'close'],
    icon: IconMinus,
    defaultCombos: [],
    context: 'global',
  },
]

// Read off the static catalog rather than off `commands`: a launcher pierces a
// focused pane through its own path, and nothing loaded from actions.yml gets
// to claim the escape chord.
const paneEscapes = new Set(commandCatalog.filter((command) => command.escapesPane).map((command) => command.id))

/** Whether the command is one of those that fire over a focused terminal pane. */
export function commandEscapesPane(commandID: string): boolean {
  return paneEscapes.has(commandID)
}

// The namespace a launcher's bindable command id lives in — `launcher.lazygit`
// for the action `lazygit`. It is what a user writes in settings.yaml, so it is
// as much a part of the config contract as the action id itself.
const LAUNCHER_PREFIX = 'launcher.'

export function launcherCommandID(actionID: string): string {
  return LAUNCHER_PREFIX + actionID
}

/** The action id behind a launcher command, or null for any other command. */
export function launcherActionID(commandID: string): string | null {
  return commandID.startsWith(LAUNCHER_PREFIX) ? commandID.slice(LAUNCHER_PREFIX.length) : null
}

const launcherCommands = ref<BindableCommand[]>([])

/**
 * Replaces the launcher commands, which change whenever actions.yml does.
 * Called by useLaunchers; everything else reads `commands`.
 */
export function setLauncherCommands(next: BindableCommand[]): void {
  launcherCommands.value = next
}

/**
 * Every bindable command: the static catalog above followed by the configured
 * launchers. Launchers come last so a combo they share with a built-in resolves
 * to the built-in — a config file must not be able to take `mod+k` away from
 * the palette.
 */
export const commands: ComputedRef<BindableCommand[]> = computed(
  () => [...commandCatalog, ...launcherCommands.value],
)
