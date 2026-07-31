import { computed, ref } from 'vue'
import {
  KeybindingSettings as GetKeybindingSettings,
  SetKeybindingSettings,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice'
import { commands } from '../keybindings/catalog'

// The frontend keybinding layer. Pure normalization (comboFromEvent /
// formatCombo) is separate from the effective keymap so both are unit-testable
// without mounting a component. Only *overrides* are persisted (localStorage
// key `hive.keybindings`, mirroring useTheme): an id absent from the store
// falls back to its catalog default, an id mapped to `[]` is explicitly
// unbound. Bindings target the stable command ids in keybindings/catalog.ts.

type Overrides = Record<string, string[]>

const knownIDs = computed(() => new Set(commands.value.map((c) => c.id)))
const defaultCombos = computed<Record<string, string[]>>(
  () => Object.fromEntries(commands.value.map((c) => [c.id, c.defaultCombos])),
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

// The punctuation event.code names, mapped to the character the same physical
// key produces unmodified — the spelling the rest of this module uses.
const PUNCTUATION_CODES: Record<string, string> = {
  Backquote: '`', Minus: '-', Equal: '=', BracketLeft: '[', BracketRight: ']',
  Backslash: '\\', Semicolon: ';', Quote: "'", Comma: ',', Period: '.', Slash: '/',
}

const NAMED_CODES = /^(Arrow(Up|Down|Left|Right)|Enter|Escape|Tab|Backspace|Delete|Space|Home|End|PageUp|PageDown)$/

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

/** The base key an event.key names, or null when it is not a bindable key. */
function keyBase(e: KeyboardEvent): string | null {
  const rawKey = e.key
  if (!rawKey || IGNORED_KEYS.has(rawKey)) return null
  return normalizeKey(rawKey) || null
}

/**
 * The base key an event.code names, or null for a code this module has no
 * spelling for. Only used for alt combos — see comboFromEvent.
 */
function baseFromCode(code: string): string | null {
  if (/^Key[A-Z]$/.test(code)) return code.slice(3).toLowerCase()
  if (/^Digit[0-9]$/.test(code)) return code.slice(5)
  if (NAMED_CODES.test(code)) return code.toLowerCase()
  return PUNCTUATION_CODES[code] ?? null
}

/**
 * Canonical combo for a keydown, or null when the event is not a bindable
 * keystroke (lone modifier, IME composition, unidentified key).
 */
export function comboFromEvent(e: KeyboardEvent): string | null {
  if (e.isComposing || e.keyCode === 229) return null
  // An alt combo takes its base from the physical key, not the character. On
  // macOS Option composes: Option+G emits `©`, so reading event.key would make
  // the combo `alt+©` and no binding a user would think to write could match
  // it — and Option+E emits `Dead`, which keyBase drops outright. The cost is
  // that alt binds by position, so a non-QWERTY layout gets the key where `g`
  // sits on QWERTY; the alternative is alt bindings that cannot be spelled at
  // all. Every other modifier still reads the character, which is what makes
  // `?` (Shift+/) a combo rather than `shift+/`.
  //
  // A modifier held alone still falls through: its code (`AltLeft`) is not one
  // baseFromCode spells, so keyBase answers and IGNORED_KEYS rejects it.
  const base = (e.altKey && e.code ? baseFromCode(e.code) : null) ?? keyBase(e)
  if (!base) return null

  const mods = new Set<string>()
  if (e.metaKey || e.ctrlKey) mods.add('mod')
  if (e.altKey) mods.add('alt')
  if (e.shiftKey && shouldRecordShift(base)) mods.add('shift')

  const ordered = MODIFIER_ORDER.filter((m) => mods.has(m))
  return [...ordered, base].join('+')
}

/**
 * The combo to resolve when a terminal pane has focus, or null when the pane
 * should keep the keystroke.
 *
 * A pane owns every key it can use, so only modifiers a terminal never wants
 * qualify: Cmd on macOS, Ctrl+Shift elsewhere. comboFromEvent cannot make that
 * call on its own — it collapses Meta and Ctrl into one `mod` token, so `mod+k`
 * cannot tell ⌘K from Ctrl+K, and Ctrl+K is readline's kill-to-end-of-line.
 * Same split useTerminalWindows' isSearchCombo makes for ⌘F, for the same
 * reason.
 *
 * The Shift is dropped from the Ctrl form before resolving: on a platform
 * without Cmd, Ctrl+Shift is how a terminal emulator spells an app chord
 * (Ctrl+Shift+C is ⌘C), so it stands in for Cmd rather than being part of the
 * combo — which is what lets one configured `mod+k` match on both platforms.
 */
export function terminalEscapeCombo(e: KeyboardEvent): string | null {
  const escapes = e.ctrlKey ? e.shiftKey && !e.metaKey : e.metaKey && !e.altKey
  if (!escapes) return null
  const combo = comboFromEvent(e)
  if (!combo) return null
  if (!e.ctrlKey) return combo
  return canonicalizeCombo(combo.split('+').filter((token) => token !== 'shift').join('+'))
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

// An unknown id is kept, not dropped. Launcher commands come from actions.yml
// and are not known until the catalog has loaded, which is after this runs —
// dropping them here would erase a user's launcher bindings from settings.yaml
// on their next rebind. An id that stays unknown simply never resolves.
function sanitizeOverrides(value: unknown): Overrides {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  const out: Overrides = {}
  for (const [id, combos] of Object.entries(value as Record<string, unknown>)) {
    if (!Array.isArray(combos)) continue
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
  for (const command of commands.value) {
    const override = overrides.value[command.id]
    result[command.id] = override !== undefined ? override : defaultCombos.value[command.id]
  }
  return result
})

// combo → command id. Catalog order makes resolution deterministic when two
// commands share a combo (the conflict is surfaced in the settings UI).
const reverseMap = computed<Map<string, string>>(() => {
  const map = new Map<string, string>()
  for (const command of commands.value) {
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
  if (!canon || !knownIDs.value.has(id)) return
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
  for (const command of commands.value) {
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
