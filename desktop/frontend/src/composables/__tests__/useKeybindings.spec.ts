import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

// The durable overrides live in settings.yaml, read and written through
// SettingsService. The effective keymap is a module singleton hydrated from
// that read, so each test seeds the stubbed service, resets modules, and
// imports fresh.
const settings = vi.hoisted(() => ({ get: vi.fn(), set: vi.fn() }))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice', () => ({
  KeybindingSettings: settings.get,
  SetKeybindingSettings: settings.set,
}))

/** Seeds what settings.yaml holds before useKeybindings hydrates. */
function seedStoredOverrides(overrides: unknown): void {
  settings.get.mockResolvedValue({ overrides })
}

/** The override map most recently written through to settings.yaml. */
function lastPersisted(): Record<string, string[]> | undefined {
  const calls = settings.set.mock.calls
  return calls.length ? (calls[calls.length - 1]![0] as { overrides: Record<string, string[]> }).overrides : undefined
}

beforeEach(() => {
  localStorage.clear()
  settings.get.mockReset()
  settings.set.mockReset()
  settings.get.mockResolvedValue({ overrides: {} })
  settings.set.mockResolvedValue(undefined)
  vi.resetModules()
})

function ev(init: Partial<KeyboardEvent>): KeyboardEvent {
  return {
    isComposing: false,
    keyCode: 0,
    key: '',
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...init,
  } as KeyboardEvent
}

describe('comboFromEvent', () => {
  it('collapses Meta and Ctrl to a single mod token', async () => {
    const { comboFromEvent } = await import('../useKeybindings')
    expect(comboFromEvent(ev({ key: 'k', metaKey: true }))).toBe('mod+k')
    expect(comboFromEvent(ev({ key: 'k', ctrlKey: true }))).toBe('mod+k')
  })

  it('lowercases letters and records Shift as an explicit token', async () => {
    const { comboFromEvent } = await import('../useKeybindings')
    expect(comboFromEvent(ev({ key: 'G', shiftKey: true }))).toBe('shift+g')
    expect(comboFromEvent(ev({ key: 'j' }))).toBe('j')
  })

  it('does not double-count Shift for symbols whose character already changed', async () => {
    const { comboFromEvent } = await import('../useKeybindings')
    // Shift+/ produces '?'; the character encodes the shift already.
    expect(comboFromEvent(ev({ key: '?', shiftKey: true }))).toBe('?')
  })

  it('names arrow, space, and enter keys', async () => {
    const { comboFromEvent } = await import('../useKeybindings')
    expect(comboFromEvent(ev({ key: 'ArrowDown' }))).toBe('arrowdown')
    expect(comboFromEvent(ev({ key: 'ArrowUp' }))).toBe('arrowup')
    expect(comboFromEvent(ev({ key: ' ' }))).toBe('space')
    expect(comboFromEvent(ev({ key: 'Enter' }))).toBe('enter')
  })

  it('orders modifiers canonically regardless of which are held', async () => {
    const { comboFromEvent } = await import('../useKeybindings')
    expect(comboFromEvent(ev({ key: 'K', metaKey: true, altKey: true, shiftKey: true }))).toBe('mod+alt+shift+k')
  })

  it('returns null for lone modifiers, IME composition, and unidentified keys', async () => {
    const { comboFromEvent } = await import('../useKeybindings')
    expect(comboFromEvent(ev({ key: 'Shift', shiftKey: true }))).toBeNull()
    expect(comboFromEvent(ev({ key: 'Meta', metaKey: true }))).toBeNull()
    expect(comboFromEvent(ev({ key: 'a', isComposing: true }))).toBeNull()
    expect(comboFromEvent(ev({ key: 'a', keyCode: 229 }))).toBeNull()
    expect(comboFromEvent(ev({ key: 'Unidentified' }))).toBeNull()
  })
})

describe('formatCombo', () => {
  it('renders mac symbols and non-mac words', async () => {
    const { formatCombo } = await import('../useKeybindings')
    expect(formatCombo('mod+k', true)).toBe('⌘K')
    expect(formatCombo('mod+k', false)).toBe('Ctrl+K')
    expect(formatCombo('shift+g', true)).toBe('⇧G')
    expect(formatCombo('shift+g', false)).toBe('Shift+G')
    expect(formatCombo('mod+shift+k', true)).toBe('⌘⇧K')
    expect(formatCombo('mod+shift+k', false)).toBe('Ctrl+Shift+K')
  })

  it('renders named keys with symbols and bare letters uppercased', async () => {
    const { formatCombo } = await import('../useKeybindings')
    expect(formatCombo('arrowdown', true)).toBe('↓')
    expect(formatCombo('j', true)).toBe('J')
    expect(formatCombo('enter', false)).toBe('↵')
  })
})

describe('canonicalizeCombo', () => {
  it('collapses and orders modifiers into the single spelling', async () => {
    const { canonicalizeCombo } = await import('../useKeybindings')
    expect(canonicalizeCombo('k+mod')).toBe('mod+k')
    expect(canonicalizeCombo('ctrl+k')).toBe('mod+k')
    expect(canonicalizeCombo('Meta+Shift+K')).toBe('mod+shift+k')
    expect(canonicalizeCombo('shift')).toBe('')
  })
})

describe('effective keymap', () => {
  it('resolves default combos to their command ids', async () => {
    const { useKeybindings } = await import('../useKeybindings')
    const kb = useKeybindings()
    expect(kb.bindings.value['feed.next']).toEqual(['j', 'arrowdown'])
    expect(kb.resolve('j')).toBe('feed.next')
    expect(kb.resolve('arrowdown')).toBe('feed.next')
    expect(kb.resolve('mod+k')).toBe('palette.toggle')
    expect(kb.resolve('nope')).toBeNull()
  })

  it('adds an override that shadows the default and persists it', async () => {
    const { useKeybindings } = await import('../useKeybindings')
    const kb = useKeybindings()
    kb.addBinding('feed.next', 'g')

    expect(kb.bindings.value['feed.next']).toEqual(['j', 'arrowdown', 'g'])
    expect(kb.resolve('g')).toBe('feed.next')
    expect(kb.isOverridden('feed.next')).toBe(true)
    await nextTick()
    expect(lastPersisted()).toMatchObject({ 'feed.next': ['j', 'arrowdown', 'g'] })
  })

  it('removes a binding, leaving an empty array as an explicit unbind', async () => {
    const { useKeybindings } = await import('../useKeybindings')
    const kb = useKeybindings()
    kb.removeBinding('feed.next', 'j')
    expect(kb.bindings.value['feed.next']).toEqual(['arrowdown'])
    expect(kb.resolve('j')).toBeNull()

    kb.removeBinding('feed.next', 'arrowdown')
    expect(kb.bindings.value['feed.next']).toEqual([])
    expect(kb.isOverridden('feed.next')).toBe(true)
  })

  it('restores catalog defaults on reset', async () => {
    const { useKeybindings } = await import('../useKeybindings')
    const kb = useKeybindings()
    kb.removeBinding('feed.next', 'j')
    kb.resetToDefault('feed.next')
    expect(kb.bindings.value['feed.next']).toEqual(['j', 'arrowdown'])
    expect(kb.isOverridden('feed.next')).toBe(false)
    expect(kb.resolve('j')).toBe('feed.next')
  })

  it('reports conflicts, honoring the excluded id', async () => {
    const { useKeybindings } = await import('../useKeybindings')
    const kb = useKeybindings()
    kb.addBinding('feed.refresh', 'j')
    expect(kb.conflicts('j').sort()).toEqual(['feed.next', 'feed.refresh'])
    expect(kb.conflicts('j', 'feed.refresh')).toEqual(['feed.next'])
  })
})

describe('launcher commands', () => {
  // A launcher is a terminal-popup action, so it joins the catalog only once
  // actions.yml has been read — which is after settings.yaml has.
  it('binds a launcher registered after the overrides were read', async () => {
    seedStoredOverrides({ 'launcher.lazygit': ['alt+g'] })
    const { useKeybindings, initializeKeybindings } = await import('../useKeybindings')
    const { setLauncherCommands } = await import('../../keybindings/catalog')
    initializeKeybindings()
    await vi.waitFor(() => expect(settings.get).toHaveBeenCalled())

    const kb = useKeybindings()
    expect(kb.resolve('alt+g')).toBeNull()

    setLauncherCommands([
      { id: 'launcher.lazygit', title: 'lazygit', group: 'Launchers', defaultCombos: [], context: 'global' },
    ])
    expect(kb.resolve('alt+g')).toBe('launcher.lazygit')
  })

  // Persisting drops whatever the override map does not hold, so a launcher
  // binding erased at load would be erased from settings.yaml by the next
  // unrelated rebind.
  it('keeps a launcher override through a rebind made before its catalog loaded', async () => {
    seedStoredOverrides({ 'launcher.lazygit': ['alt+g'] })
    const { useKeybindings, initializeKeybindings } = await import('../useKeybindings')
    initializeKeybindings()
    await vi.waitFor(() => expect(settings.get).toHaveBeenCalled())

    useKeybindings().addBinding('feed.next', 'g')
    await vi.waitFor(() => expect(lastPersisted()).toEqual({
      'launcher.lazygit': ['alt+g'],
      'feed.next': ['j', 'arrowdown', 'g'],
    }))
  })

  // Catalog order is what resolves a shared combo, and launchers come last, so
  // a config file cannot take a built-in shortcut away from the app.
  it('does not let a launcher shadow a built-in combo', async () => {
    const { useKeybindings } = await import('../useKeybindings')
    const { setLauncherCommands } = await import('../../keybindings/catalog')
    setLauncherCommands([
      { id: 'launcher.thief', title: 'Thief', group: 'Launchers', defaultCombos: ['mod+k'], context: 'global' },
    ])
    expect(useKeybindings().resolve('mod+k')).toBe('palette.toggle')
  })
})

describe('override storage sanitization', () => {
  // settings.yaml is hand-editable, so anything can arrive here.
  it('drops non-arrays and non-string combos on load, and never binds an unknown id', async () => {
    seedStoredOverrides({
      'feed.next': ['g'],
      'unknown.id': ['x'],
      'feed.prev': 'not-an-array',
      'feed.refresh': [123, 'r', 'r'],
    })
    const { useKeybindings, initializeKeybindings } = await import('../useKeybindings')
    initializeKeybindings()
    await vi.waitFor(() => expect(useKeybindings().bindings.value['feed.next']).toEqual(['g']))

    const kb = useKeybindings()
    expect(kb.bindings.value['feed.refresh']).toEqual(['r']) // 123 dropped, dupe collapsed
    expect(kb.bindings.value['feed.prev']).toEqual(['k', 'arrowup']) // invalid → default
    // Kept in the override map — it may be a launcher whose catalog has not
    // arrived — but it resolves to nothing until something claims the id.
    expect(kb.resolve('x')).toBeNull()
  })

  it('falls back to defaults for a malformed or non-object section', async () => {
    seedStoredOverrides('not an object')
    const { useKeybindings, initializeKeybindings } = await import('../useKeybindings')
    initializeKeybindings()
    await nextTick()
    expect(useKeybindings().bindings.value['feed.next']).toEqual(['j', 'arrowdown'])
  })

  // An unreadable settings.yaml must leave the app usable rather than
  // shortcut-less.
  it('keeps catalog defaults when the settings read fails', async () => {
    settings.get.mockRejectedValue(new Error('unavailable'))
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { useKeybindings, initializeKeybindings } = await import('../useKeybindings')
    initializeKeybindings()
    await vi.waitFor(() => expect(warn).toHaveBeenCalled())
    expect(useKeybindings().bindings.value['feed.next']).toEqual(['j', 'arrowdown'])
    warn.mockRestore()
  })

  // Hydration happens after mount; a rebind made while that read is in flight
  // is newer than its result and must survive.
  it('does not let a late hydrate clobber a rebind made while it was in flight', async () => {
    let resolveRead: (value: { overrides: Record<string, string[]> }) => void = () => {}
    settings.get.mockReturnValue(new Promise((resolve) => { resolveRead = resolve }))

    const { useKeybindings, initializeKeybindings } = await import('../useKeybindings')
    initializeKeybindings()

    const kb = useKeybindings()
    kb.addBinding('feed.next', 'g')
    resolveRead({ overrides: { 'feed.next': ['z'] } })
    await nextTick()

    expect(kb.bindings.value['feed.next']).toEqual(['j', 'arrowdown', 'g'])
  })

  it('clears the section entirely on reset-all', async () => {
    const { useKeybindings } = await import('../useKeybindings')
    const kb = useKeybindings()
    kb.addBinding('feed.next', 'g')
    kb.clearAll()

    expect(kb.bindings.value['feed.next']).toEqual(['j', 'arrowdown'])
    // Writes are chained so two quick edits cannot land out of order, so the
    // clear lands a tick behind the add.
    await vi.waitFor(() => expect(lastPersisted()).toEqual({}))
  })
})
