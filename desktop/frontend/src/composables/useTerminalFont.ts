import { computed, ref, type ComputedRef, type Ref } from 'vue'
import {
  AppearanceSettings as GetAppearanceSettings,
  SetTerminalFontSize as PersistTerminalFontSize,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice'

export const terminalFontSizes = ['small', 'medium', 'large', 'xl', 'xxl'] as const
export type TerminalFontSize = (typeof terminalFontSizes)[number]

export const terminalFontSizeLabels: Record<TerminalFontSize, string> = {
  small: 'Small',
  medium: 'Medium',
  large: 'Large',
  xl: 'XL',
  xxl: 'XXL',
}

// Presets rather than a free number so every size has known-good cell metrics.
// Medium is the default — one notch above the 12px the terminal shipped with.
export const terminalFontSizePx: Record<TerminalFontSize, number> = {
  small: 12,
  medium: 13,
  large: 14,
  xl: 16,
  xxl: 18,
}

export const defaultTerminalFontSize: TerminalFontSize = 'medium'

function isTerminalFontSize(value: string | null): value is TerminalFontSize {
  return terminalFontSizes.includes(value as TerminalFontSize)
}

// A module singleton like useTheme's currentTheme, shared by SettingsView's
// picker and every open terminal. No first-paint cache: a terminal that opens
// before hydration lands at the default and the watcher in useTerminalWindows
// re-applies, so the durable record in settings.yaml is the only store.
const currentSize: Ref<TerminalFontSize> = ref(defaultTerminalFontSize)

let hydrated = false
// Same staleness guard as useTheme: a selection made while the hydrating read
// is in flight must not be overwritten by its result.
let sizeVersion = 0
let persistChain: Promise<void> = Promise.resolve()

async function hydrate(): Promise<void> {
  const version = sizeVersion
  try {
    const settings = await GetAppearanceSettings()
    if (sizeVersion !== version) return
    if (isTerminalFontSize(settings.terminalFontSize)) currentSize.value = settings.terminalFontSize
  } catch (error) {
    // An unavailable binding keeps the default; nothing to heal.
    console.warn('Unable to load terminal font size from settings.yaml', error)
  }
}

export function setTerminalFontSize(next: TerminalFontSize): void {
  sizeVersion++
  currentSize.value = next
  // Chained so two quick selections cannot land out of order.
  persistChain = persistChain
    .then(async () => {
      await PersistTerminalFontSize(next)
    })
    .catch((error: unknown) => {
      console.warn('Unable to persist terminal font size to settings.yaml', error)
    })
}

// The ladder ends hold rather than wrap: a nudge past the last preset is a
// no-op, not a jump back to the smallest.
export function stepTerminalFontSize(delta: 1 | -1): void {
  const next = terminalFontSizes[terminalFontSizes.indexOf(currentSize.value) + delta]
  if (next) setTerminalFontSize(next)
}

export function resetTerminalFontSize(): void {
  if (currentSize.value !== defaultTerminalFontSize) setTerminalFontSize(defaultTerminalFontSize)
}

/** Where `size` sits on the ladder, for controls that offer to move it. */
export function terminalFontSizeState(size: TerminalFontSize): {
  canDecrease: boolean
  canIncrease: boolean
  isDefault: boolean
} {
  const at = terminalFontSizes.indexOf(size)
  return {
    canDecrease: at > 0,
    canIncrease: at < terminalFontSizes.length - 1,
    isDefault: size === defaultTerminalFontSize,
  }
}

export function useTerminalFont(): { size: Ref<TerminalFontSize>; px: ComputedRef<number> } {
  if (!hydrated) {
    hydrated = true
    void hydrate()
  }
  return { size: currentSize, px: computed(() => terminalFontSizePx[currentSize.value]) }
}

export function resetTerminalFontForTests(): void {
  currentSize.value = defaultTerminalFontSize
  hydrated = false
  sizeVersion = 0
  persistChain = Promise.resolve()
}
