import { computed, watch, type Ref } from 'vue'
import type { Router } from 'vue-router'
import IconCode from '~icons/lucide/code'
import IconGauge from '~icons/lucide/gauge'
import IconInbox from '~icons/lucide/inbox'
import IconLayoutGrid from '~icons/lucide/layout-grid'
import IconList from '~icons/lucide/list'
import IconMessagesSquare from '~icons/lucide/messages-square'
import IconPalette from '~icons/lucide/palette'
import IconRss from '~icons/lucide/rss'
import IconSearch from '~icons/lucide/search'
import IconShare2 from '~icons/lucide/share-2'
import IconTerminal from '~icons/lucide/terminal'
import IconWorkflow from '~icons/lucide/workflow'
import { commands as bindableCommands, terminalWindowCommandID, type CommandContext } from '../keybindings/catalog'
import { formatCombo, useKeybindings } from './useKeybindings'
import { useCommands, useCommandPalette, type Command } from './useCommands'
import { setTheme, themeLabels, themes } from './useTheme'
import { terminalSessionGroups, useTerminalSessions } from './useTerminalSessions'
import { useTerminalPinnedChats } from './useTerminalPinnedChats'
import { useAgentSessionsAll } from './useAgentSessionsAll'
import { useAttachedTerminalWindows } from './useAttachedTerminalWindows'
import { applicationSettingsSections } from '../router'
import { applicationSettingsSectionMeta } from '../components/settings/sectionMeta'
import { actionTypeMeta } from '../lib/actionPresentation'
import { containerLine } from '../lib/itemPresentation'
import type { ActionView } from '../types/action'
import type { InboxItem, Profile, SidebarSelection } from '../types/feed'

export interface AppPaletteDeps {
  /** Resolves a catalog command id to its implementation (App.vue's runMap). */
  runCommand: (id: string) => void
  /** Whether a catalog command's context fires from where the user is standing. */
  contextActive: (context: CommandContext) => boolean
  mode: Ref<'hub' | 'terminal' | 'agents'>
  setMode: (next: 'hub' | 'terminal' | 'agents') => void
  shellLoaded: Ref<boolean>
  onboardingActive: Ref<boolean>
  hubActive: Ref<boolean>
  feedNavActive: Ref<boolean>
  devToolsEnabled: Ref<boolean>
  router: Router
  profiles: Ref<Profile[]>
  activeProfile: Ref<Profile | null>
  requestSelectProfile: (id: string) => Promise<void>
  navigateSidebar: (selection: SidebarSelection) => void
  selectedItem: Ref<InboxItem | null>
  actions: Ref<ActionView[]>
  invokeAction: (id: string) => Promise<void>
  flowsActive: Ref<boolean>
  openFlows: (focusNodeId?: string) => void
  requestExitFlows: () => void
  openNewProfile: () => void
  /** Gates the "Filter sessions" named row, same as terminal.* commands. */
  terminalActive: Ref<boolean>
  /** The active flow's nodes, for the "jump to node" rows. */
  activeFlowNodes: Ref<{ id: string; name?: string; type: string }[]>
  /** The slug attached on screen, so its own attach row does not offer itself. */
  onScreenSessionSlug: Ref<string>
}

/**
 * Registers every App-level palette row source for the app's lifetime:
 * catalog commands, mode switches, and the hub's own objects (profiles, feeds,
 * flow nodes, themes), plus the Go-to rows for sessions, windows, settings
 * sections, and chats that are global rather than tied to a lazily mounted
 * mode.
 */
export function useAppPaletteRows(deps: AppPaletteDeps): void {
  const {
    runCommand, contextActive, mode, setMode, shellLoaded, onboardingActive, hubActive, feedNavActive,
    devToolsEnabled, router, profiles, activeProfile, requestSelectProfile, navigateSidebar, selectedItem,
    actions, invokeAction, flowsActive, openFlows, requestExitFlows, openNewProfile, activeFlowNodes,
    onScreenSessionSlug, terminalActive,
  } = deps

  const { combosFor } = useKeybindings()

  // The app is usable — past onboarding, the shell resolved — regardless of
  // which mode is on screen. This is the gate the #306 fix widens the hub's
  // own Go-to rows to, in place of the narrower "on the hub" gate they used to
  // carry: their run()s already land in the hub from anywhere.
  const appReady = computed(() => shellLoaded.value && !onboardingActive.value)

  // Session and window rows read the same module singletons the sidebar tree
  // itself reads, so they are global rather than tied to TerminalMode ever
  // having mounted.
  const { sessions: terminalSessionRows, scratch: terminalScratchRow, reload: reloadTerminalSessions } = useTerminalSessions()
  const { rows: pinnedChatRows } = useTerminalPinnedChats()
  const { attached: attachedTerminal } = useAttachedTerminalWindows()
  const activeTerminalSessions = computed(() => terminalSessionRows.value.filter((row) => row.state === 'active'))
  const terminalGroups = computed(() => terminalSessionGroups(activeTerminalSessions.value, terminalScratchRow.value, pinnedChatRows.value))

  // Chat rows read the same recents listing the Code view's pinned-chats
  // section and the Agents area's own sidebar do.
  const { recents: chatRecents, reloadRecents } = useAgentSessionsAll()

  useCommands(computed(() => {
    const cmds: Command[] = []

    for (const command of bindableCommands.value) {
      if (command.paletteHidden || !contextActive(command.context)) continue
      cmds.push({
        id: command.id,
        title: command.title,
        group: command.group,
        keywords: command.keywords,
        icon: command.icon,
        scope: command.scope,
        hint: formatCombo(combosFor(command.id)[0] ?? ''),
        run: () => runCommand(command.id),
      })
    }

    // view.focus-search is palette-hidden because one command answers `/` in
    // two unrelated surfaces, and a single row for it would no-op wherever the
    // other surface is on screen. These named rows stand in per surface,
    // gated the same way the surface's own commands are, sharing its hint.
    const focusSearchHint = formatCombo(combosFor('view.focus-search')[0] ?? '')
    if (feedNavActive.value) {
      cmds.push({
        id: 'view.focus-search:feed',
        title: 'Search items…',
        group: 'Feeds',
        scope: 'actions',
        keywords: ['find', 'filter', 'search'],
        icon: IconSearch,
        hint: focusSearchHint,
        run: () => runCommand('view.focus-search'),
      })
    }
    if (terminalActive.value) {
      cmds.push({
        id: 'view.focus-search:terminal',
        title: 'Filter sessions',
        group: 'Terminal',
        scope: 'actions',
        keywords: ['terminal', 'filter', 'search', 'find', 'session'],
        icon: IconSearch,
        hint: focusSearchHint,
        run: () => runCommand('view.focus-search'),
      })
    }

    // A row per mode this one is not: with the other modes' objects hidden,
    // these keep a mode change reachable without the title bar.
    if (appReady.value) {
      if (mode.value !== 'hub') {
        cmds.push({
          id: 'mode:hub',
          title: 'Go to Inbox',
          group: 'View',
          scope: 'goto',
          keywords: ['inbox', 'hub', 'feed', 'mode'],
          icon: IconInbox,
          hint: formatCombo(combosFor('view.go-inbox')[0] ?? ''),
          run: () => setMode('hub'),
        })
      }
      if (mode.value !== 'terminal') {
        cmds.push({
          id: 'mode:terminal',
          title: 'Go to Code',
          group: 'View',
          scope: 'goto',
          keywords: ['code', 'terminal', 'sessions', 'mode'],
          icon: IconCode,
          hint: formatCombo(combosFor('view.go-code')[0] ?? ''),
          run: () => setMode('terminal'),
        })
      }
      if (mode.value !== 'agents') {
        cmds.push({
          id: 'mode:agents',
          title: 'Go to Chats',
          group: 'View',
          scope: 'goto',
          keywords: ['chats', 'agents', 'chat', 'mode'],
          icon: IconMessagesSquare,
          hint: formatCombo(combosFor('view.go-chats')[0] ?? ''),
          run: () => setMode('agents'),
        })
      }

      // Profiles, feeds and Trash, and themes reach across every mode (#306):
      // requestSelectProfile, navigateSidebar and setTheme all push a route
      // that lands in the hub, so there is nothing hub-specific left in them.
      for (const p of profiles.value) {
        cmds.push({
          id: `profile:${p.id}`,
          title: `Switch to profile: ${p.name}`,
          group: 'Profiles',
          scope: 'goto',
          icon: IconLayoutGrid,
          run: () => requestSelectProfile(p.id),
        })
      }

      const profileName = activeProfile.value?.name

      cmds.push({
        id: 'view:trash',
        title: 'Open Trash',
        group: 'Feeds',
        scope: 'goto',
        icon: IconList,
        hint: profileName,
        run: () => navigateSidebar({ type: 'trash' }),
      })

      for (const f of activeProfile.value?.feeds ?? []) {
        cmds.push({
          id: `feed:${f.id}`,
          title: `Select feed: ${f.name}`,
          group: 'Feeds',
          scope: 'goto',
          icon: IconRss,
          hint: profileName,
          run: () => navigateSidebar({ type: 'feed', feedId: f.id }),
        })
      }

      for (const t of themes) {
        cmds.push({
          id: `theme:${t}`,
          title: `Theme: ${themeLabels[t]}`,
          group: 'Theme',
          scope: 'actions',
          keywords: ['theme', 'appearance', t],
          icon: IconPalette,
          run: () => setTheme(t),
        })
      }

      // One row per application settings section — titled the way the nav
      // panel itself labels them, so the palette and Settings agree.
      for (const section of applicationSettingsSections) {
        const meta = applicationSettingsSectionMeta[section]
        cmds.push({
          id: `settings:${section}`,
          title: `Settings › ${meta.label}`,
          group: 'Settings',
          scope: 'goto',
          icon: meta.icon,
          run: () => void router.push({ name: 'application-settings', params: { section } }),
        })
      }
    }

    if (hubActive.value) {
      // The selected item's configured actions, under its own reference — the
      // same set the detail pane draws as cards. Running one from here goes
      // through invokeAction, so an action that declares inputs opens its form
      // and an interactive launch-session opens the session dialog, exactly as
      // a card click does.
      if (feedNavActive.value && selectedItem.value) {
        const itemGroup = containerLine(selectedItem.value) || 'Item'
        for (const action of actions.value) {
          const meta = actionTypeMeta(action.type)
          cmds.push({
            id: `item:action:${action.id}`,
            title: action.label,
            group: itemGroup,
            order: -3,
            scope: 'actions',
            keywords: ['action', 'item', action.type],
            iconName: meta.icon,
            iconColor: meta.color,
            run: () => void invokeAction(action.id),
          })
        }
      }

      cmds.push({
        id: 'profile:new',
        title: 'New profile…',
        group: 'Profiles',
        scope: 'actions',
        keywords: ['workspace', 'create'],
        run: openNewProfile,
      })

      // View — enter/exit the flows canvas for the active profile. The canvas
      // is profile-bound, so this and the node rows below stay hub-gated.
      cmds.push({
        id: 'flow:edit',
        title: flowsActive.value ? 'Back to feed' : 'Edit flow…',
        group: 'View',
        scope: 'actions',
        keywords: ['flows', 'pipeline', 'nodes', 'canvas', 'editor'],
        icon: IconWorkflow,
        run: () => { flowsActive.value ? requestExitFlows() : openFlows() },
      })

      // Jump to any node in the active flow by name (8d) — opens the canvas
      // focused/centered on that node, same as "Reveal in flow" from the
      // sidebar.
      for (const node of activeFlowNodes.value) {
        cmds.push({
          id: `flow:node:${node.id}`,
          title: `Jump to node: ${node.name || node.type}`,
          group: 'Flow',
          scope: 'goto',
          keywords: ['flows', 'node', 'canvas', 'reveal'],
          icon: IconShare2,
          run: () => openFlows(node.id),
        })
      }
    }

    // The palette is the only way in outside a Vite build, where the dev strip
    // carries the link.
    if (devToolsEnabled.value) {
      cmds.push({
        id: 'dev:open',
        title: 'Open developer tools',
        group: 'View',
        scope: 'goto',
        keywords: ['runtime', 'performance', 'memory', 'cpu', 'diagnostics'],
        icon: IconGauge,
        run: () => { void router.push({ name: 'dev' }) },
      })
    }

    // Session attach rows, grouped the way the sidebar tree groups them
    // (pinned chats, scratch, then one per repo). The route is the attach
    // state in and out of Code alike, so the row on screen already skips
    // itself.
    for (const group of terminalGroups.value) {
      for (const row of group.sessions) {
        if (row.slug === onScreenSessionSlug.value) continue
        cmds.push({
          id: `terminal:attach:${row.slug}`,
          title: `Attach session: ${row.name}`,
          group: group.name,
          scope: 'goto',
          keywords: [row.slug, group.name, 'session', 'attach', 'switch', 'open'],
          icon: IconTerminal,
          hint: group.name,
          run: () => void router.push({ name: 'terminal', params: { slug: row.slug } }),
        })
      }
    }

    // Window rows for whichever session is attached (the module projection
    // TerminalMode writes, since the live tab list is otherwise
    // component-local). Hints carry the numbered jump chords the keymap
    // already binds to each position in the strip.
    if (attachedTerminal.value) {
      const { slug, name, windows } = attachedTerminal.value
      windows.forEach((win, index) => {
        cmds.push({
          id: `terminal:window:${win.windowId}`,
          title: `Go to window: ${win.name}`,
          group: name,
          scope: 'goto',
          keywords: ['window', 'tab', 'jump', 'switch'],
          icon: IconTerminal,
          hint: formatCombo(combosFor(terminalWindowCommandID(index + 1))[0] ?? ''),
          run: () => void router.push({ name: 'terminal', params: { slug }, query: { window: win.windowId } }),
        })
      })
    }

    // Chat rows — every recent session across every workspace; empty and
    // absent wherever the Agents area itself is unavailable.
    for (const session of chatRecents.value) {
      cmds.push({
        id: `chat:${session.id}`,
        title: `Chat: ${session.name}`,
        group: 'Chats',
        scope: 'goto',
        icon: IconMessagesSquare,
        run: () => void router.push({ name: 'agents', params: { workspace: session.workspace }, query: { chat: String(session.id) } }),
      })
    }

    return cmds
  }))

  // Session and chat rows are read from module singletons the sidebar trees
  // keep warm elsewhere; a palette open is the moment they are about to be
  // shown, so that is when staleness is worth paying to fix. Both reloads keep
  // last-good rows on failure.
  const { open: paletteOpen } = useCommandPalette()
  watch(paletteOpen, (open) => {
    if (!open) return
    void reloadTerminalSessions()
    void reloadRecents()
  })
}
