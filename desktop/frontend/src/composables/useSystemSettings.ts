import { computed, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import {
  Build,
  ChooseDirectory,
  ClearConfigDir,
  ClearDataDir,
  Info,
  OpenPath,
  Quit,
  RevealPath,
  SetConfigDir,
  SetDataDir,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice'
import {
  CheckNow,
  SetEnabled,
  Status,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/updaterservice'
import {
  ExperimentalSettings as LoadExperimentalSettings,
  SetExperimentalAgents,
  SetExperimentalTerminal,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice'
import { Enabled as TerminalModeEnabled } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice'
import { Enabled as AgentsModeEnabled } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice'
import type { BuildInfo, SystemInfo, UpdateInfo } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// useSystemSettings drives the System settings screen: it reads the effective
// on-disk locations from the SystemService and wraps the open/reveal actions
// and the point-only data/config directory overrides. Overrides are applied on
// the next launch, so any successful change flips restartRequired.
export function useSystemSettings() {
  const info = ref<SystemInfo | null>(null)
  const build = ref<BuildInfo | null>(null)
  const loading = ref(false)
  const error = ref('')
  const restartRequired = ref(false)

  // Auto-update state. autoUpdate mirrors the persisted toggle; update holds
  // the last check result so the view can render "up to date" / "vX available"
  // inline. checkingUpdate guards the manual Check button; checkedOnce lets the
  // view distinguish "never checked" from an up-to-date result.
  const autoUpdate = ref(true)
  const update = ref<UpdateInfo | null>(null)
  const checkingUpdate = ref(false)
  const checkedOnce = ref(false)

  // The experimental.terminal opt-in (ADR 0037) is read once at startup, so
  // the toggle tracks two values: what is persisted and what this run mounted.
  // They differ exactly while a relaunch is pending. experimental.agents
  // (ADR 0061) follows the identical shape.
  const experimentalTerminal = ref(false)
  const terminalModeRunning = ref(false)
  const terminalRestartPending = computed(() => experimentalTerminal.value !== terminalModeRunning.value)

  const experimentalAgents = ref(false)
  const agentsModeRunning = ref(false)
  const agentsRestartPending = computed(() => experimentalAgents.value !== agentsModeRunning.value)

  async function refresh(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const [locations, buildInfo, status, experimental, terminalRunning, agentsRunning] = await Promise.all([
        Info(), Build(), Status(), LoadExperimentalSettings(), TerminalModeEnabled(), AgentsModeEnabled(),
      ])
      info.value = locations
      build.value = buildInfo
      update.value = status
      autoUpdate.value = status.enabled
      experimentalTerminal.value = experimental.terminal
      terminalModeRunning.value = terminalRunning
      experimentalAgents.value = experimental.agents
      agentsModeRunning.value = agentsRunning
    } catch (err) {
      error.value = errText(err)
    } finally {
      loading.value = false
    }
  }

  // setExperimentalTerminal persists the opt-in; the running app is unchanged
  // until relaunch, which is what terminalRestartPending surfaces. The stored
  // value comes from the reply so a process env override cannot drift the
  // switch from what the backend resolved.
  async function setExperimentalTerminal(value: boolean): Promise<void> {
    const previous = experimentalTerminal.value
    experimentalTerminal.value = value
    error.value = ''
    try {
      const effective = await SetExperimentalTerminal(value)
      experimentalTerminal.value = effective.terminal
    } catch (err) {
      experimentalTerminal.value = previous
      error.value = errText(err)
    }
  }

  // setExperimentalAgents persists the Agents-area opt-in; the running app is
  // unchanged until relaunch, which agentsRestartPending surfaces.
  async function setExperimentalAgents(value: boolean): Promise<void> {
    const previous = experimentalAgents.value
    experimentalAgents.value = value
    error.value = ''
    try {
      const effective = await SetExperimentalAgents(value)
      experimentalAgents.value = effective.agents
    } catch (err) {
      experimentalAgents.value = previous
      error.value = errText(err)
    }
  }

  // setAutoUpdate persists the toggle through the service (which also starts or
  // stops the background ticker). On failure the previous value is restored so
  // the switch never drifts from the backend.
  async function setAutoUpdate(value: boolean): Promise<void> {
    const previous = autoUpdate.value
    autoUpdate.value = value
    error.value = ''
    try {
      await SetEnabled(value)
    } catch (err) {
      autoUpdate.value = previous
      error.value = errText(err)
    }
  }

  // checkForUpdates runs a manual check and stores the result for inline
  // display next to the Version row.
  async function checkForUpdates(): Promise<void> {
    checkingUpdate.value = true
    error.value = ''
    try {
      update.value = await CheckNow()
      checkedOnce.value = true
    } catch (err) {
      error.value = errText(err)
    } finally {
      checkingUpdate.value = false
    }
  }

  // openExternal opens a build-info URL in the system browser. Guarded by a
  // non-empty url (dev builds have no releaseUrl), so the view only wires links
  // when there is somewhere to go.
  async function openExternal(url: string | undefined): Promise<void> {
    if (!url) return
    error.value = ''
    try {
      await Browser.OpenURL(url)
    } catch (err) {
      error.value = errText(err)
    }
  }

  async function openPath(path: string): Promise<void> {
    error.value = ''
    try {
      await OpenPath(path)
    } catch (err) {
      error.value = errText(err)
    }
  }

  async function revealPath(path: string): Promise<void> {
    error.value = ''
    try {
      await RevealPath(path)
    } catch (err) {
      error.value = errText(err)
    }
  }

  async function changeDir(title: string, setter: (path: string) => Promise<void>): Promise<void> {
    error.value = ''
    try {
      const chosen = await ChooseDirectory(title)
      if (!chosen) return
      await setter(chosen)
      restartRequired.value = true
      await refresh()
    } catch (err) {
      error.value = errText(err)
    }
  }

  async function resetDir(clear: () => Promise<void>): Promise<void> {
    error.value = ''
    try {
      await clear()
      restartRequired.value = true
      await refresh()
    } catch (err) {
      error.value = errText(err)
    }
  }

  return {
    info,
    build,
    loading,
    error,
    restartRequired,
    autoUpdate,
    update,
    checkingUpdate,
    checkedOnce,
    experimentalTerminal,
    terminalRestartPending,
    setExperimentalTerminal,
    experimentalAgents,
    agentsRestartPending,
    setExperimentalAgents,
    setAutoUpdate,
    checkForUpdates,
    refresh,
    openReleaseNotes: () => openExternal(build.value?.releaseUrl),
    openRepo: () => openExternal(build.value?.repoUrl),
    openPath,
    revealPath,
    changeDataDir: () => changeDir('Choose data directory', SetDataDir),
    changeConfigDir: () => changeDir('Choose config directory', SetConfigDir),
    resetDataDir: () => resetDir(ClearDataDir),
    resetConfigDir: () => resetDir(ClearConfigDir),
    quit: () => {
      void Quit()
    },
  }
}
