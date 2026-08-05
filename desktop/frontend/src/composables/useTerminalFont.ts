import { computed, ref, type ComputedRef, type Ref } from 'vue'
import {
  AppearanceSettings as GetAppearanceSettings,
  MonospaceFonts as GetMonospaceFonts,
  SetTerminalFontFamily as PersistTerminalFontFamily,
  SetTerminalFontSize as PersistTerminalFontSize,
  SetTerminalFontWeights as PersistTerminalFontWeights,
  SetTerminalLetterSpacing as PersistTerminalLetterSpacing,
  SetTerminalLineHeight as PersistTerminalLineHeight,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice'
import { TERMINAL_FONT } from '../lib/terminalFaces'

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

// The weights the bundled face ships, which is what makes each one a distinct
// rendering rather than a label over the same outlines: CSS matches a requested
// weight to the nearest declared face and never synthesizes a lighter one. A
// system font with fewer faces collapses some of these together — that is the
// font's doing, not a bug here.
export const terminalFontWeights = [300, 350, 400, 600, 700] as const
export type TerminalFontWeight = (typeof terminalFontWeights)[number]

export const terminalFontWeightLabels: Record<TerminalFontWeight, string> = {
  300: 'Light',
  350: 'Semilight',
  400: 'Regular',
  600: 'Semibold',
  700: 'Bold',
}

// Semilight, half a step under Regular. Terminal panes rasterise through a
// canvas atlas (ADR terminal-atlas-renderer) and Canvas2D text does not inherit the
// `-webkit-font-smoothing: antialiased` the rest of the app is drawn with, so a
// face renders heavier here than the same face does in the DOM — which is what
// #181 reports as "everything is bold". Regular is what that issue was filed
// about and Light reads too thin at these sizes on a dark background, so the
// usable range is narrow and the default sits between them.
export const defaultTerminalFontWeight: TerminalFontWeight = 350
export const defaultTerminalFontWeightBold: TerminalFontWeight = 700

// Multiplies the cell height. Box drawing still meets the cell edges above 1:
// the atlas strokes a custom glyph across the padded cell and offsets it by
// exactly what the renderer centres the char box by, so the two cancel
// (ADR terminal-line-height-and-letter-spacing).
export const terminalLineHeights = [1, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6] as const
export type TerminalLineHeight = (typeof terminalLineHeights)[number]

// Widens the cell by whole *device* pixels — xterm adds this to the device
// char width and rounds it, so on a 2x display a step is half a CSS pixel and
// a fractional setting would quantise to nothing.
export const terminalLetterSpacings = [0, 1, 2, 3] as const
export type TerminalLetterSpacing = (typeof terminalLetterSpacings)[number]

export const defaultTerminalLineHeight: TerminalLineHeight = 1.2
// Must stay 0: it doubles as the "nothing persisted" value in settings.yaml.
export const defaultTerminalLetterSpacing: TerminalLetterSpacing = 0

function isTerminalFontSize(value: string | null): value is TerminalFontSize {
  return terminalFontSizes.includes(value as TerminalFontSize)
}

function isTerminalFontWeight(value: number | null): value is TerminalFontWeight {
  return terminalFontWeights.includes(value as TerminalFontWeight)
}

function isTerminalLineHeight(value: number | null): value is TerminalLineHeight {
  return terminalLineHeights.includes(value as TerminalLineHeight)
}

function isTerminalLetterSpacing(value: number | null): value is TerminalLetterSpacing {
  return terminalLetterSpacings.includes(value as TerminalLetterSpacing)
}

// Module singletons like useTheme's currentTheme, shared by SettingsView's
// pickers and every open terminal. No first-paint cache: a terminal that opens
// before hydration lands at the defaults and the watchers in useTerminalWindows
// re-apply, so the durable record in settings.yaml is the only store.
const currentSize: Ref<TerminalFontSize> = ref(defaultTerminalFontSize)
// Empty is the bundled face rather than a sentinel name, so a settings.yaml
// written before this setting existed reads as "shipped default".
const currentFamily: Ref<string> = ref('')
const currentWeight: Ref<TerminalFontWeight> = ref(defaultTerminalFontWeight)
const currentWeightBold: Ref<TerminalFontWeight> = ref(defaultTerminalFontWeightBold)
const currentLineHeight: Ref<TerminalLineHeight> = ref(defaultTerminalLineHeight)
const currentLetterSpacing: Ref<TerminalLetterSpacing> = ref(defaultTerminalLetterSpacing)
const installedFamilies: Ref<string[]> = ref([])

let hydrated = false
// Same staleness guard as useTheme: a selection made while the hydrating read
// is in flight must not be overwritten by its result.
let version = 0
let persistChain: Promise<void> = Promise.resolve()

async function hydrate(): Promise<void> {
  const started = version
  try {
    const settings = await GetAppearanceSettings()
    if (version !== started) return
    if (isTerminalFontSize(settings.terminalFontSize)) currentSize.value = settings.terminalFontSize
    if (settings.terminalFontFamily) currentFamily.value = settings.terminalFontFamily
    if (isTerminalFontWeight(settings.terminalFontWeight)) {
      currentWeight.value = settings.terminalFontWeight
    }
    if (isTerminalFontWeight(settings.terminalFontWeightBold)) {
      currentWeightBold.value = settings.terminalFontWeightBold
    }
    if (isTerminalLineHeight(settings.terminalLineHeight)) {
      currentLineHeight.value = settings.terminalLineHeight
    }
    if (isTerminalLetterSpacing(settings.terminalLetterSpacing)) {
      currentLetterSpacing.value = settings.terminalLetterSpacing
    }
  } catch (error) {
    // An unavailable binding keeps the defaults; nothing to heal.
    console.warn('Unable to load terminal font settings from settings.yaml', error)
  }
}

// Scanning every font file on the machine takes tens of milliseconds and the Go
// side caches the result for the process, so this runs once, lazily, when a
// picker first needs it — never on the path a terminal opens through.
let fontsRequested = false

export function loadInstalledMonospaceFonts(): void {
  if (fontsRequested) return
  fontsRequested = true
  void GetMonospaceFonts()
    .then((families) => {
      installedFamilies.value = families ?? []
    })
    .catch((error: unknown) => {
      // A failed scan leaves the picker with the bundled face alone, which is
      // still a working terminal.
      console.warn('Unable to list installed monospace fonts', error)
    })
}

function persist(write: () => Promise<void>): void {
  version++
  // Chained so two quick selections cannot land out of order.
  persistChain = persistChain.then(write).catch((error: unknown) => {
    console.warn('Unable to persist terminal font settings to settings.yaml', error)
  })
}

export function setTerminalFontSize(next: TerminalFontSize): void {
  currentSize.value = next
  persist(() => PersistTerminalFontSize(next))
}


export function setTerminalFontFamily(next: string): void {
  // The bundled face is stored as empty so it tracks the shipped font rather
  // than pinning today's name into a user's settings.yaml.
  const family = next === TERMINAL_FONT ? '' : next
  currentFamily.value = family
  persist(() => PersistTerminalFontFamily(family))
}

export function setTerminalFontWeight(next: TerminalFontWeight): void {
  currentWeight.value = next
  persist(() => PersistTerminalFontWeights(next, currentWeightBold.value))
}

export function setTerminalFontWeightBold(next: TerminalFontWeight): void {
  currentWeightBold.value = next
  persist(() => PersistTerminalFontWeights(currentWeight.value, next))
}

export function setTerminalLineHeight(next: TerminalLineHeight): void {
  currentLineHeight.value = next
  persist(() => PersistTerminalLineHeight(next))
}

export function setTerminalLetterSpacing(next: TerminalLetterSpacing): void {
  currentLetterSpacing.value = next
  persist(() => PersistTerminalLetterSpacing(next))
}

/**
 * Every typography value that moves a cell's width or height, as one
 * comparable key. A size vote is only counted in the metrics it was measured
 * against, so a remembered one is keyed by this.
 */
export function terminalCellMetrics(): string {
  return [
    terminalFontSizePx[currentSize.value],
    currentFamily.value,
    currentWeight.value,
    currentWeightBold.value,
    currentLineHeight.value,
    currentLetterSpacing.value,
  ].join('|')
}

export function useTerminalFont(): {
  size: Ref<TerminalFontSize>
  px: ComputedRef<number>
  family: Ref<string>
  /** The family to show selected: the bundled face stands in for empty. */
  selectedFamily: ComputedRef<string>
  installedFamilies: Ref<string[]>
  weight: Ref<TerminalFontWeight>
  weightBold: Ref<TerminalFontWeight>
  lineHeight: Ref<TerminalLineHeight>
  letterSpacing: Ref<TerminalLetterSpacing>
} {
  if (!hydrated) {
    hydrated = true
    void hydrate()
  }
  return {
    size: currentSize,
    px: computed(() => terminalFontSizePx[currentSize.value]),
    family: currentFamily,
    selectedFamily: computed(() => currentFamily.value || TERMINAL_FONT),
    installedFamilies,
    weight: currentWeight,
    weightBold: currentWeightBold,
    lineHeight: currentLineHeight,
    letterSpacing: currentLetterSpacing,
  }
}

export function resetTerminalFontForTests(): void {
  currentSize.value = defaultTerminalFontSize
  currentFamily.value = ''
  currentWeight.value = defaultTerminalFontWeight
  currentWeightBold.value = defaultTerminalFontWeightBold
  currentLineHeight.value = defaultTerminalLineHeight
  currentLetterSpacing.value = defaultTerminalLetterSpacing
  installedFamilies.value = []
  fontsRequested = false
  hydrated = false
  version = 0
  persistChain = Promise.resolve()
}
