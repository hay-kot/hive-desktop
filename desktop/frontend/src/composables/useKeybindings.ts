import { computed, ref } from 'vue'
import {
  KeybindingSettings as GetKeybindingSettings,
  SetKeybindingSettings,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice'
import { commandCatalog } from '../keybindings/catalog'

// The frontend keybinding layer. Pure normalization (comboFromEvent /
// formatCombo) is separate from the effective keymap so both are unit-testable
// without mounting a component. Only *overrides* are persisted (localStorage
// key `hive.keybindings`, mirroring useTheme): an id absent from the store
// falls back to its catalog default, an id mapped to `[]` is explicitly
// unbound. Bindings target the stable command ids in keybindings/catalog.ts.

type Overrides = Record<string, string[]>

const KNOWN_IDS = new Set(commandCatalog.map((c) => c.id))
const DEFAULT_COMBOS: Record<string, string[]> = Object.fromEntries(
  commandCatalog.map((c) => [c.id, c.defaultCombos]),
)

const MODIFIER_ORDER = ['mod', 'ctrl', 'alt', 'shift'] as const

// event.key values that are modifiers or otherwise not real bindable keys.
const IGNORED_KEYS = new Set([
  'Shift', 'Control', 'Alt', 'Meta', 'CapsLock', 'NumLock', 'ScrollLock',
  'OS', 'AltGraph', 'Fn', 'FnLock', 'Hyper', 'Super', 'Symbol', 'SymbolLock',
  'Dead', 'Unidentified',
])

const KEY_SYMBOLS: Record<string, string> = {
  arrowdown: '↓', arrowup: '↑', arrowleft: '←', arrowright: '→',
  enter: '↵', space: 'Space', escape: 'Esc', backspace: '⌫',
  tab: 'Tab', delete: 'Del', plus: '+',
}

// ─── Pure helpers (exported for tests) ─────────────────────────────────────────

/** Meta/Ctrl → `mod`, Alt/Option → `alt`; anything else is not a modifier. */
function normalizeModifier(token: string): string | null {
  switch (token) {
    case 'mod': case 'meta': case 'cmd': case 'command': case 'ctrl': case 'control':
      return 'mod'
    case 'alt': case 'option': case 'opt':
      return 'alt'
    case 'shift':
      return 'shift'
    default:
      return null
  }
}

function normalizeKey(key: string): string {
  if (key === ' ' || key === 'Spacebar') return 'space'
  if (key === '+') return 'plus'
  return key.toLowerCase()
}

// Add an explicit `shift` token only when Shift didn't already change the
// produced character: single latin letters (`g` → `shift+g`) and named keys
// (`arrowdown` → `shift+arrowdown`). For symbols/digits the character itself
// already encodes Shift (Shift+/ yields `?`), so `shift` is not doubled on.
function shouldRecordShift(base: string): boolean {
  return /^[a-z]$/.test(base) || base.length > 1
}

/**
 * Canonicalize a combo string to its single spelling: modifiers collapsed
 * (Meta/Ctrl → `mod`), lowercased, ordered `mod, ctrl, alt, shift`, then the
 * base key. Returns '' for a combo with no base key.
 */
export function canonicalizeCombo(combo: string): string {
  const parts = combo.split('+').map((p) => p.trim().toLowerCase()).filter(Boolean)
  const mods = new Set<string>()
  let key = ''
  for (const part of parts) {
    const mod = normalizeModifier(part)
    if (mod) mods.add(mod)
    else key = normalizeKey(part)
  }
  if (!key) return ''
  const ordered = MODIFIER_ORDER.filter((m) => mods.has(m))
  return [...ordered, key].join('+')
}

/**
 * Canonical combo for a keydown, or null when the event is not a bindable
 * keystroke (lone modifier, IME composition, unidentified key).
 */
export function comboFromEvent(e: KeyboardEvent): string | null {
  if (e.isComposing || e.keyCode === 229) return null
  const rawKey = e.key
  if (!rawKey || IGNORED_KEYS.has(rawKey)) return null
  const base = normalizeKey(rawKey)
  if (!base) return null

  const mods = new Set<string>()
  if (e.metaKey || e.ctrlKey) mods.add('mod')
  if (e.altKey) mods.add('alt')
  if (e.shiftKey && shouldRecordShift(base)) mods.add('shift')

  const ordered = MODIFIER_ORDER.filter((m) => mods.has(m))
  return [...ordered, base].join('+')
}

function detectMac(): boolean {
  return typeof navigator !== 'undefined' && /Mac/i.test(navigator.userAgent)
}

function formatModifier(mod: string, isMac: boolean): string {
  switch (mod) {
    case 'mod': return isMac ? '⌘' : 'Ctrl'
    case 'alt': return isMac ? '⌥' : 'Alt'
    case 'shift': return isMac ? '⇧' : 'Shift'
    case 'ctrl': return 'Ctrl'
    default: return mod
  }
}

/** Human-readable label for a combo — `⌘K` on macOS, `Ctrl+K` elsewhere. */
export function formatCombo(combo: string, isMac: boolean = detectMac()): string {
  const canon = canonicalizeCombo(combo)
  if (!canon) return ''
  const parts = canon.split('+')
  const key = parts[parts.length - 1]
  const mods = parts.slice(0, -1).map((m) => formatModifier(m, isMac))
  const keyLabel = KEY_SYMBOLS[key] ?? (key.length === 1 ? key.toUpperCase() : capitalize(key))
  return isMac ? [...mods, keyLabel].join('') : [...mods, keyLabel].join('+')
}

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1)
}

// ─── Persisted overrides (module singleton) ────────────────────────────────────

function sanitizeOverrides(value: unknown): Overrides {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  const out: Overrides = {}
  for (const [id, combos] of Object.entries(value as Record<string, unknown>)) {
    if (!KNOWN_IDS.has(id) || !Array.isArray(combos)) continue
    const clean: string[] = []
    for (const combo of combos) {
      if (typeof combo !== 'string') continue
      const canon = canonicalizeCombo(combo)
      if (canon && !clean.includes(canon)) clean.push(canon)
    }
    out[id] = clean // may be [] to mean "explicitly unbound"
  }
  return out
}

// The durable record is settings.yaml's `keybindings` section, so shortcuts
// live alongside the rest of the user's config and can be managed from a
// dotfiles repo or handed to an agent (see the "Keyboard shortcuts" prompt in
// internal/app/prompts). Webview localStorage is not a candidate: it is
// partitioned per bundle id and per dev-server port and macOS may purge it
// outright, so a rebind could silently vanish.
//
// Only overrides are stored. An id absent here falls back to its catalog
// default; an id mapped to [] is explicitly unbound.
const overrides = ref<Overrides>({})

// Incremented by every mutation so an in-flight hydrate can tell its result is
// already stale and must not clobber a rebind the user just made (the same
// versioning idea as useTheme).
let overridesVersion = 0

// Saves are chained rather than fired in parallel so two quick rebinds cannot
// land out of order and leave settings.yaml disagreeing with the screen. The
// chain absorbs its own failures so it never settles rejected.
let persistChain: Promise<void> = Promise.resolve()

function persistOverrides(next: Overrides): void {
  persistChain = persistChain
    .then(async () => {
      await SetKeybindingSettings({ overrides: next })
    })
    .catch((error: unknown) => {
      // The rebind is already live in this session; losing the durable write is
      // degraded-but-working, not a reason to revert what the user sees.
      console.warn('Unable to persist keybindings to settings.yaml', error)
    })
}

/** Replaces the override map and writes it through to settings.yaml. */
function applyOverrides(next: Overrides): void {
  overridesVersion++
  overrides.value = next
  persistOverrides(next)
}

// Reads the durable overrides once at startup. A failure keeps the catalog
// defaults rather than blanking the keymap: an unavailable binding must not
// leave the app with no shortcuts.
async function hydrateFromSettings(): Promise<void> {
  const version = overridesVersion
  try {
    const settings = await GetKeybindingSettings()
    if (overridesVersion !== version) return
    overrides.value = sanitizeOverrides(settings.overrides)
  } catch (error) {
    console.warn('Unable to load keybindings from settings.yaml', error)
  }
}

/** Called once from main.ts, before the app can dispatch a shortcut. */
export function initializeKeybindings(): void {
  void hydrateFromSettings()
}

// True while the settings editor is capturing a keystroke; the global
// dispatcher checks this so a combo being recorded never also fires a command.
const recording = ref(false)

const effectiveBindings = computed<Record<string, string[]>>(() => {
  const result: Record<string, string[]> = {}
  for (const command of commandCatalog) {
    const override = overrides.value[command.id]
    result[command.id] = override !== undefined ? override : DEFAULT_COMBOS[command.id]
  }
  return result
})

// combo → command id. Catalog order makes resolution deterministic when two
// commands share a combo (the conflict is surfaced in the settings UI).
const reverseMap = computed<Map<string, string>>(() => {
  const map = new Map<string, string>()
  for (const command of commandCatalog) {
    for (const combo of effectiveBindings.value[command.id]) {
      if (!map.has(combo)) map.set(combo, command.id)
    }
  }
  return map
})

function combosFor(id: string): string[] {
  return effectiveBindings.value[id] ?? []
}

function resolve(combo: string): string | null {
  return reverseMap.value.get(combo) ?? null
}

function setCombos(id: string, combos: string[]): void {
  applyOverrides({ ...overrides.value, [id]: combos })
}

function addBinding(id: string, combo: string): void {
  const canon = canonicalizeCombo(combo)
  if (!canon || !KNOWN_IDS.has(id)) return
  const current = combosFor(id)
  if (current.includes(canon)) return
  setCombos(id, [...current, canon])
}

function removeBinding(id: string, combo: string): void {
  const canon = canonicalizeCombo(combo)
  setCombos(id, combosFor(id).filter((c) => c !== canon)) // [] = explicitly unbound
}

function resetToDefault(id: string): void {
  const next = { ...overrides.value }
  delete next[id]
  applyOverrides(next)
}

function clearAll(): void {
  applyOverrides({})
}

function isOverridden(id: string): boolean {
  return overrides.value[id] !== undefined
}

/** Command ids (other than excludeId) that also bind `combo`. */
function conflicts(combo: string, excludeId?: string): string[] {
  const canon = canonicalizeCombo(combo)
  if (!canon) return []
  const ids: string[] = []
  for (const command of commandCatalog) {
    if (command.id === excludeId) continue
    if (effectiveBindings.value[command.id].includes(canon)) ids.push(command.id)
  }
  return ids
}

export function useKeybindings() {
  return {
    /** Effective id → combos[] map (defaults ⊕ overrides). */
    bindings: effectiveBindings,
    recording,
    combosFor,
    resolve,
    addBinding,
    removeBinding,
    resetToDefault,
    clearAll,
    isOverridden,
    conflicts,
  }
}
