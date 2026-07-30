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
  RestartPending as LoadRestartPending,
  SetExperimentalTerminal,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/settingsservice'
import type { BuildInfo, RestartPendingField, SystemInfo, UpdateInfo } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'
import { useWailsEvent } from './useWailsEvent'

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// useSystemSettings drives the System settings screen: it reads the effective
// on-disk locations from the SystemService and wraps the open/reveal actions
// and the point-only data/config directory overrides.
//
// Nothing here decides what needs a relaunch. SettingsService.RestartPending is
// the backend's single answer — which persisted values this process is not
// running — and every hint on the screen is a filter over that list.
export function useSystemSettings() {
  const info = ref<SystemInfo | null>(null)
  const build = ref<BuildInfo | null>(null)
  const loading = ref(false)
  const error = ref('')
  const pending = ref<RestartPendingField[]>([])

  // Auto-update state. autoUpdate mirrors the persisted toggle; update holds
  // the last check result so the view can render "up to date" / "vX available"
  // inline. checkingUpdate guards the manual Check button; checkedOnce lets the
  // view distinguish "never checked" from an up-to-date result.
  const autoUpdate = ref(true)
  const update = ref<UpdateInfo | null>(null)
  const checkingUpdate = ref(false)
  const checkedOnce = ref(false)

  // The experimental.terminal opt-in (ADR 0037) is read once at startup, so the
  // switch shows the persisted value and the backend reports whether this run
  // is already on it.
  const experimentalTerminal = ref(false)
  const terminalRestartPending = computed(() => pendingField('experimental.terminal') !== undefined)
  // A moved data or config directory is the other startup-only choice on this
  // screen; both arrive as bootstrap.* rows of the same list.
  const restartRequired = computed(() => pending.value.length > 0)

  function pendingField(field: string): RestartPendingField | undefined {
    return pending.value.find((entry) => entry.field === field)
  }

  async function refresh(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const [locations, buildInfo, status, experimental, restartPending] = await Promise.all([
        Info(), Build(), Status(), LoadExperimentalSettings(), LoadRestartPending(),
      ])
      info.value = locations
      build.value = buildInfo
      update.value = status
      autoUpdate.value = status.enabled
      experimentalTerminal.value = experimental.terminal
      pending.value = restartPending
    } catch (err) {
      error.value = errText(err)
    } finally {
      loading.value = false
    }
  }

  useWailsEvent('settings:updated', () => {
    void refresh()
  })

  // setExperimentalTerminal persists the opt-in; the running app is unchanged
  // until relaunch, which is what terminalRestartPending surfaces. The stored
  // value comes from the reply so a process env override cannot drift the
  // switch from what the backend resolved, and the pending list is re-read
  // because only the backend knows whether this run already mounted it.
  async function setExperimentalTerminal(value: boolean): Promise<void> {
    const previous = experimentalTerminal.value
    experimentalTerminal.value = value
    error.value = ''
    try {
      const effective = await SetExperimentalTerminal(value)
      experimentalTerminal.value = effective.terminal
      pending.value = await LoadRestartPending()
    } catch (err) {
      experimentalTerminal.value = previous
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
      await refresh()
    } catch (err) {
      error.value = errText(err)
    }
  }

  async function resetDir(clear: () => Promise<void>): Promise<void> {
    error.value = ''
    try {
      await clear()
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
    restartPending: pending,
    restartRequired,
    autoUpdate,
    update,
    checkingUpdate,
    checkedOnce,
    experimentalTerminal,
    terminalRestartPending,
    setExperimentalTerminal,
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
