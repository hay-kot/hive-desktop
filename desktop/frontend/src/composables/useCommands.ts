import { computed, onScopeDispose, ref, toValue } from 'vue'
import type { Component, ComputedRef, MaybeRefOrGetter, Ref } from 'vue'

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
}

const shellEscapes = ref<ShellEscapeRegistration[]>([])

/**
 * Claims `!`-prefixed queries for the lifetime of the calling effect scope.
 * While the query starts with `!` the palette stops fuzzy-matching and shows
 * only what the handlers return for the rest of the line — the line is a
 * command to run, not a search, so fuzzy rows would be coincidental and Enter
 * must never land on one. Return [] where the escape cannot run (wrong view);
 * an empty line is never offered.
 */
export function useShellEscape(source: (line: string) => Command[]): void {
  const key = Symbol()
  shellEscapes.value = [...shellEscapes.value, { key, source }]

  onScopeDispose(() => {
    shellEscapes.value = shellEscapes.value.filter((r) => r.key !== key)
  })
}

// ─── Palette state (module singleton) ─────────────────────────────────────────

const _open = ref(false)
const _query = ref('')

/**
 * Returns palette state shared across all callers.
 * CommandPalette.vue consumes this directly (exempt from props-in/events-out
 * for the palette itself).
 */
export function useCommandPalette(): {
  open: Ref<boolean>
  query: Ref<string>
  results: ComputedRef<Command[]>
  toggle(): void
  run(cmd: Command): void | Promise<void>
} {
  const results = computed<Command[]>(() => {
    const query = _query.value
    if (query.startsWith('!')) {
      const line = query.slice(1).trim()
      if (!line) return []
      return shellEscapes.value.flatMap((r) => r.source(line))
    }
    const all = registrations.value.flatMap((r) => toValue(r.source))
    return filterAndScore(query, all)
  })

  function toggle(): void {
    _open.value = !_open.value
    if (!_open.value) _query.value = ''
  }

  function run(cmd: Command): void | Promise<void> {
    _open.value = false
    _query.value = ''
    return cmd.run()
  }

  return { open: _open, query: _query, results, toggle, run }
}
