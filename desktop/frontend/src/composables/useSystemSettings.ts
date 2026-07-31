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

  const experimentalTerminal = ref(false)
  // A moved data or config directory is the startup-only choice on this
  // screen; it arrives as a bootstrap.* row of the same list.
  const restartRequired = computed(() => pending.value.length > 0)

  // A Go slice crosses the bridge as null when it is empty, so every read of
  // the pending list normalizes before it lands in the ref — the rest of this
  // composable and the view treat it as a plain array.
  async function loadPending(): Promise<RestartPendingField[]> {
    return (await LoadRestartPending()) ?? []
  }

  async function refresh(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const [locations, buildInfo, status, experimental, restartPending] = await Promise.all([
        Info(), Build(), Status(), LoadExperimentalSettings(), loadPending(),
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

  async function setExperimentalTerminal(value: boolean): Promise<void> {
    const previous = experimentalTerminal.value
    experimentalTerminal.value = value
    error.value = ''
    try {
      const effective = await SetExperimentalTerminal(value)
      experimentalTerminal.value = effective.terminal
      pending.value = await loadPending()
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
