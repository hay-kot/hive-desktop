<script setup lang="ts">
// Terminal settings: everything that governs a terminal wherever one is drawn
// — the Code tab, the pop-up panel, and the Agents area's chats — plus the
// opt-in that gates them all.
import { computed, defineAsyncComponent, onMounted } from 'vue'
import AppSelect from './AppSelect.vue'
import AppSwitch from './AppSwitch.vue'
import SettingsError from './settings/SettingsError.vue'
import SettingsPage from './settings/SettingsPage.vue'
import SettingsRow from './settings/SettingsRow.vue'
import SettingsSection from './settings/SettingsSection.vue'
import SettingsSegmented from './settings/SettingsSegmented.vue'
import ExperimentalToggle from './settings/ExperimentalToggle.vue'
import {
  loadInstalledMonospaceFonts,
  setTerminalFontFamily,
  setTerminalFontSize,
  setTerminalFontWeight,
  setTerminalFontWeightBold,
  setTerminalLetterSpacing,
  setTerminalLineHeight,
  terminalFontSizeLabels,
  terminalFontSizePx,
  terminalFontSizes,
  terminalFontWeightLabels,
  terminalFontWeights,
  terminalLetterSpacings,
  terminalLineHeights,
  useTerminalFont,
  type TerminalFontSize,
  type TerminalFontWeight,
  type TerminalLetterSpacing,
  type TerminalLineHeight,
} from '../composables/useTerminalFont'
import { TERMINAL_FONT } from '../lib/terminalFaces'
import { setTerminalPoolSize, terminalPoolSizes, useTerminalPoolSize } from '../composables/useTerminalPoolSize'
import { setTerminalShowWindows, useTerminalShowWindows } from '../composables/useTerminalShowWindows'
import { useExperimentalSettings } from '../composables/useExperimentalSettings'

// Async so xterm and its addons stay on the terminal chunk rather than joining
// the bundle everyone who opens any settings pane pays for.
const TerminalPreview = defineAsyncComponent(() => import('./settings/TerminalPreview.vue'))

const { terminal, terminalRestartPending, setTerminal, error, refresh } = useExperimentalSettings()
const {
  size: fontSize,
  selectedFamily: fontFamily,
  installedFamilies: fontFamilies,
  weight: fontWeight,
  weightBold: fontWeightBold,
  lineHeight,
  letterSpacing,
} = useTerminalFont()
const { showWindows } = useTerminalShowWindows()
const { poolSize } = useTerminalPoolSize()

const fontSizeOptions = terminalFontSizes.map((value) => ({
  value,
  label: `${terminalFontSizePx[value]}px`,
  title: terminalFontSizeLabels[value],
}))
// The bundled face leads the list whether or not it is also installed
// system-wide, so the shipped default is always the first thing offered.
const fontFamilyOptions = computed(() => [
  { value: TERMINAL_FONT, label: `${TERMINAL_FONT} · bundled` },
  ...fontFamilies.value
    .filter((family) => family !== TERMINAL_FONT)
    .map((family) => ({ value: family, label: family })),
])
const fontWeightOptions = terminalFontWeights.map((value) => ({
  value: String(value),
  label: terminalFontWeightLabels[value],
}))
const lineHeightOptions = terminalLineHeights.map((value) => ({
  value: String(value),
  label: value.toFixed(1),
}))
const letterSpacingOptions = terminalLetterSpacings.map((value) => ({
  value: String(value),
  label: value === 0 ? 'None' : `+${value}`,
}))
const poolSizeOptions = terminalPoolSizes.map((value) => ({ value: String(value), label: String(value) }))

function onFontSizeChange(value: string): void {
  setTerminalFontSize(value as TerminalFontSize)
}

function onFontWeightChange(value: string): void {
  setTerminalFontWeight(Number(value) as TerminalFontWeight)
}

function onFontWeightBoldChange(value: string): void {
  setTerminalFontWeightBold(Number(value) as TerminalFontWeight)
}

function onLineHeightChange(value: string): void {
  setTerminalLineHeight(Number(value) as TerminalLineHeight)
}

function onLetterSpacingChange(value: string): void {
  setTerminalLetterSpacing(Number(value) as TerminalLetterSpacing)
}

function onPoolSizeChange(value: string): void {
  setTerminalPoolSize(Number(value))
}

// Scanning every monospace font on the machine is not worth doing until this
// pane is the one on screen.
onMounted(() => {
  loadInstalledMonospaceFonts()
  void refresh()
})
</script>

<template>
  <SettingsPage testid="settings-terminal">
    <SettingsError v-if="error" :message="error" testid="terminal-error" />

    <SettingsSection
      title="Terminal mode"
      description="Attach to a session's tmux windows inside the app. Off by default; changes apply after restarting Hive."
      boxed
      testid="terminal-mode"
    >
      <SettingsRow
        label="Terminal mode"
        hint="Turns on the Code tab in the title bar and the pop-up terminal panel. Needs tmux 3.2 or newer on your PATH; closing Hive leaves the tmux sessions running."
      >
        <ExperimentalToggle
          :model-value="terminal"
          :restart-pending="terminalRestartPending"
          ariaLabel="Terminal mode"
          testid="terminal-mode-enabled"
          @update:model-value="setTerminal"
        />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection
      title="Typography"
      description="How terminal text is drawn, everywhere one appears. Changes apply to open terminals immediately."
      boxed
    >
      <SettingsRow
        label="Font"
        hint="Monospace families installed on this machine. The powerline and devicon glyphs agent TUIs draw with come from a bundled symbol face, so a family that lacks them still renders them."
        testid="settings-terminal-font-family"
      >
        <AppSelect
          class="w-[220px]"
          :model-value="fontFamily"
          :options="fontFamilyOptions"
          searchable
          search-placeholder="Search fonts"
          aria-label="Terminal font"
          testid="settings-terminal-font-family-select"
          @update:model-value="setTerminalFontFamily"
        />
      </SettingsRow>
      <SettingsRow
        label="Font size"
        hint="Applies immediately to open terminals; tmux re-fits their grid."
      >
        <SettingsSegmented
          :model-value="fontSize"
          :options="fontSizeOptions"
          aria-label="Font size"
          testid="settings-terminal-font-size"
          @update:model-value="onFontSizeChange"
        />
      </SettingsRow>
      <SettingsRow
        label="Font weight"
        hint="The weight normal text draws at. A family that ships fewer weights renders the nearest one it has."
      >
        <SettingsSegmented
          :model-value="String(fontWeight)"
          :options="fontWeightOptions"
          aria-label="Font weight"
          testid="settings-terminal-font-weight"
          @update:model-value="onFontWeightChange"
        />
      </SettingsRow>
      <SettingsRow label="Bold weight" hint="The weight bold text draws at.">
        <SettingsSegmented
          :model-value="String(fontWeightBold)"
          :options="fontWeightOptions"
          aria-label="Bold weight"
          testid="settings-terminal-font-weight-bold"
          @update:model-value="onFontWeightBoldChange"
        />
      </SettingsRow>
      <SettingsRow
        label="Line height"
        hint="Multiplies the row height. Taller rows are easier to scan; each one costs a row of grid in the same pane."
      >
        <SettingsSegmented
          :model-value="String(lineHeight)"
          :options="lineHeightOptions"
          aria-label="Line height"
          testid="settings-terminal-line-height"
          @update:model-value="onLineHeightChange"
        />
      </SettingsRow>
      <SettingsRow
        label="Letter spacing"
        hint="Extra tracking in device pixels — half a point per step on a Retina display. Wider cells fit fewer columns."
      >
        <SettingsSegmented
          :model-value="String(letterSpacing)"
          :options="letterSpacingOptions"
          aria-label="Letter spacing"
          testid="settings-terminal-letter-spacing"
          @update:model-value="onLetterSpacingChange"
        />
      </SettingsRow>
      <div class="px-4 py-3.5"><TerminalPreview /></div>
    </SettingsSection>

    <SettingsSection
      title="Behaviour"
      description="What the terminal keeps on hand while you work."
      boxed
    >
      <SettingsRow
        label="Always show windows"
        hint="List every active session's windows in the session tree, not just the attached one's."
      >
        <AppSwitch
          :model-value="showWindows"
          aria-label="Always show windows"
          testid="settings-terminal-show-windows"
          @update:model-value="setTerminalShowWindows"
        />
      </SettingsRow>
      <SettingsRow
        label="Warm sessions"
        hint="Sessions kept attached in the background so switching back is instant. Each holds a tmux client, its stream, and its terminals."
      >
        <SettingsSegmented
          :model-value="String(poolSize)"
          :options="poolSizeOptions"
          aria-label="Warm sessions"
          testid="settings-terminal-pool-size"
          @update:model-value="onPoolSizeChange"
        />
      </SettingsRow>
    </SettingsSection>
  </SettingsPage>
</template>
