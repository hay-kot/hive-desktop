import { computed, onScopeDispose, ref, toValue, watch } from 'vue'
import type { Component, ComputedRef, MaybeRefOrGetter, Ref } from 'vue'
import { scopeForSigil, type CommandScope, type PaletteScopeId, type PaletteScopeSpec, paletteScopes } from '../palette/scopes'
import { usePaletteRecents } from './usePaletteRecents'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface Command {
  id: string
  /** Palette row text, e.g. "Switch to profile: Desktop" */
  title: string
  /** Section header, e.g. "Profiles", "Feeds", "Window" */
  group?: string
  /**
   * Group placement: lower sorts earlier, default 0, ties broken by group
   * name. Every row in a group must carry the same value, or the group splits.
   */
  order?: number
  /** Extra match terms */
  keywords?: string[]
  /** Icon shown in the row's leading chip; palette falls back to a default */
  icon?: Component
  /**
   * An AppIcon registry name, for rows whose glyph is named rather than
   * imported — a configured action carries its type's icon as a string, the
   * same one its menu entry draws. Ignored when `icon` is set.
   */
  iconName?: string
  /** Tints the named icon, as the action menus tint theirs. */
  iconColor?: string
  /** Right-aligned mono hint, e.g. a shortcut or context label */
  hint?: string
  /** Palette scope: 'goto' rows browse, 'actions' rows act. Default 'actions'. */
  scope?: CommandScope
  /** Phase 2: running the row keeps the palette open (sigil-legend rows). */
  keepOpen?: boolean
  run: () => void | Promise<void>
}

interface Registration {
  key: symbol
  source: MaybeRefOrGetter<Command[]>
}

// ─── Module-scoped registry ───────────────────────────────────────────────────

const registrations = ref<Registration[]>([])

// ─── Pure scoring / sorting (exported for unit tests) ─────────────────────────

/**
 * Score a command against a search query.
 *
 * Returns:
 *   3  — prefix match on title
 *   2  — substring match on title
 *   1  — match in keywords or group
 *  -1  — no match (should be filtered out)
 *   0  — empty query (matches everything, no preference)
 */
export function scoreCommand(query: string, cmd: Command): number {
  if (!query) return 0
  const q = query.toLowerCase()
  const t = cmd.title.toLowerCase()
  if (t.startsWith(q)) return 3
  if (t.includes(q)) return 2
  if ((cmd.keywords ?? []).some((k) => k.toLowerCase().includes(q))) return 1
  if (cmd.group?.toLowerCase().includes(q)) return 1
  return -1
}

function compareGroupPlacement(a: Command, b: Command): number {
  const delta = (a.order ?? 0) - (b.order ?? 0)
  if (delta) return delta
  const ga = a.group ?? ''
  const gb = b.group ?? ''
  return ga < gb ? -1 : ga > gb ? 1 : 0
}

/**
 * Sort commands by group placement (order asc, then group name asc). The sort
 * is stable, so rows keep their registration order within a group — which is
 * how positional lists (a session's windows, the sidebar's feeds) stay in
 * their own order rather than alphabetical.
 */
export function sortCommands(cmds: Command[]): Command[] {
  return [...cmds].sort(compareGroupPlacement)
}

/**
 * Filter and rank commands by query.
 *
 * - Empty query: all commands in group placement order.
 * - Non-empty: filtered (score ≥ 0), best score first; group placement only
 *   breaks ties, so a strong title match beats an early group's keyword hit.
 */
export function filterAndScore(query: string, cmds: Command[]): Command[] {
  if (!query) return sortCommands(cmds)

  const scored = cmds
    .map((cmd) => ({ cmd, score: scoreCommand(query, cmd) }))
    .filter(({ score }) => score >= 0)

  scored.sort((a, b) => b.score - a.score || compareGroupPlacement(a.cmd, b.cmd))

  return scored.map((s) => s.cmd)
}

// ─── Registration ─────────────────────────────────────────────────────────────

/**
 * Registers commands for the lifetime of the calling effect scope.
 * Auto-unregisters via onScopeDispose.
 *
 * Accepts a MaybeRefOrGetter so reactive sources (profiles, feeds) stay live:
 *   useCommands(computed(() => profiles.value.map(...)))
 */
export function useCommands(commands: MaybeRefOrGetter<Command[]>): void {
  const key = Symbol()
  registrations.value = [...registrations.value, { key, source: commands }]

  onScopeDispose(() => {
    registrations.value = registrations.value.filter((r) => r.key !== key)
  })
}

// ─── Shell escape (`!` queries) ───────────────────────────────────────────────

interface ShellEscapeRegistration {
  key: symbol
  source: (line: string) => Command[]
  available: () => boolean
}

const shellEscapes = ref<ShellEscapeRegistration[]>([])

/**
 * Claims the Shell scope for the lifetime of the calling effect scope.
 * `available` drives the Shell tab's visibility (default: always shown) so it
 * can hide outside the view it applies to; `source` still returns [] where a
 * given line cannot run (e.g. no attached session) — an empty line is never
 * offered either.
 */
export function useShellEscape(
  source: (line: string) => Command[],
  available: () => boolean = () => true,
): void {
  const key = Symbol()
  shellEscapes.value = [...shellEscapes.value, { key, source, available }]

  onScopeDispose(() => {
    shellEscapes.value = shellEscapes.value.filter((r) => r.key !== key)
  })
}

// ─── Keys scope (`?` queries) ───────────────────────────────────────────────

interface KeysScopeRegistration {
  key: symbol
  source: () => Command[]
}

const keysScopes = ref<KeysScopeRegistration[]>([])

/**
 * Claims the Keys scope for the calling effect scope. Kept catalog-agnostic
 * (like useShellEscape) so this module never imports keybindings/catalog;
 * useAppPaletteRows supplies `source` over useKeymapRows().
 */
export function useKeysScope(source: () => Command[]): void {
  const key = Symbol()
  keysScopes.value = [...keysScopes.value, { key, source }]

  onScopeDispose(() => {
    keysScopes.value = keysScopes.value.filter((r) => r.key !== key)
  })
}

// ─── Palette state (module singleton) ─────────────────────────────────────────

const _open = ref(false)
const _query = ref('')
const _scope = ref<PaletteScopeId>('all')

/** Registry entries whose tab is currently shown (Shell/Keys only where available). */
const visibleScopes = computed<PaletteScopeSpec[]>(() =>
  paletteScopes.filter((s) => {
    if (s.id === 'shell') return shellEscapes.value.some((r) => r.available())
    if (s.id === 'keys') return keysScopes.value.length > 0
    return true
  }),
)

// A tab that goes away (e.g. Shell on a view switch) must not strand the
// palette on a scope with no way back to it via the tab strip.
watch(
  visibleScopes,
  (visible) => {
    if (_open.value && !visible.some((s) => s.id === _scope.value)) _scope.value = 'all'
  },
  { flush: 'sync' },
)

/**
 * Returns palette state shared across all callers.
 * CommandPalette.vue consumes this directly (exempt from props-in/events-out
 * for the palette itself).
 */
export function useCommandPalette(): {
  open: Ref<boolean>
  query: Ref<string>
  scope: Ref<PaletteScopeId>
  visibleScopes: ComputedRef<PaletteScopeSpec[]>
  results: ComputedRef<Command[]>
  toggle(): void
  openWithScope(scope: PaletteScopeId): void
  setQuery(next: string): void
  setScope(scope: PaletteScopeId): void
  cycleScope(delta: 1 | -1): void
  popScope(): boolean
  run(cmd: Command): void | Promise<void>
} {
  const { recordRun } = usePaletteRecents()

  const results = computed<Command[]>(() => {
    const query = _query.value
    const allCommands = () => registrations.value.flatMap((r) => toValue(r.source))

    switch (_scope.value) {
      case 'shell': {
        const line = query.trim()
        if (!line) return []
        return shellEscapes.value.flatMap((r) => r.source(line))
      }
      case 'keys':
        return filterAndScore(query, keysScopes.value.flatMap((r) => r.source()))
      case 'goto':
      case 'actions': {
        const wanted = _scope.value
        return filterAndScore(query, allCommands().filter((cmd) => (cmd.scope ?? 'actions') === wanted))
      }
      default:
        return filterAndScore(query, allCommands())
    }
  })

  function toggle(): void {
    _open.value = !_open.value
    if (!_open.value) {
      _query.value = ''
      _scope.value = 'all'
    }
  }

  function openWithScope(scope: PaletteScopeId): void {
    _open.value = true
    _scope.value = scope
    _query.value = ''
  }

  /**
   * The input's write path. Performs sigil interception: an empty query
   * followed by a visible scope's sigil enters that scope and absorbs the
   * sigil, leaving the rest of `next` as the query — so a paste of "@foo"
   * lands in the goto scope with query "foo". A non-empty prior query means
   * the sigil is mid-edit, not an entry point, so it is left as literal text.
   */
  function setQuery(next: string): void {
    if (!_query.value && next) {
      const entered = scopeForSigil(next[0])
      if (entered && visibleScopes.value.some((s) => s.id === entered)) {
        _scope.value = entered
        _query.value = next.slice(1)
        return
      }
    }
    _query.value = next
  }

  function setScope(scope: PaletteScopeId): void {
    _scope.value = scope
  }

  function cycleScope(delta: 1 | -1): void {
    const visible = visibleScopes.value
    if (visible.length === 0) return
    const at = visible.findIndex((s) => s.id === _scope.value)
    const from = at >= 0 ? at : 0
    _scope.value = visible[(from + delta + visible.length) % visible.length].id
  }

  function popScope(): boolean {
    if (_query.value || _scope.value === 'all') return false
    _scope.value = 'all'
    return true
  }

  function run(cmd: Command): void | Promise<void> {
    // A sigil-legend row switches scope itself; closing around that would undo
    // the very navigation the row exists to offer. It is chrome, not a
    // repeatable entry, so it is never recorded either.
    if (cmd.keepOpen) return cmd.run()
    // Shell lines aren't repeatable entries and Keys rows are reference, so
    // only All/Go to/Actions rows earn a place in Recent.
    if (_scope.value === 'all' || _scope.value === 'goto' || _scope.value === 'actions') recordRun(cmd.id)
    _open.value = false
    _query.value = ''
    _scope.value = 'all'
    return cmd.run()
  }

  return {
    open: _open,
    query: _query,
    scope: _scope,
    visibleScopes,
    results,
    toggle,
    openWithScope,
    setQuery,
    setScope,
    cycleScope,
    popScope,
    run,
  }
}
