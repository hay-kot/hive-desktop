import { ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import { Build } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice'
import {
  CheckNow,
  SetEnabled,
  Status,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/updaterservice'
import type { BuildInfo, UpdateInfo } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/models'

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

// useAboutSettings drives the About pane: which build is running, where it came
// from, and the auto-update controls that replace it. autoUpdate mirrors the
// persisted toggle; update holds the last check result so the pane can render
// "up to date" / "vX available" inline, and checkedOnce distinguishes "never
// checked" from an up-to-date answer.
export function useAboutSettings() {
  const build = ref<BuildInfo | null>(null)
  const update = ref<UpdateInfo | null>(null)
  const autoUpdate = ref(true)
  const checking = ref(false)
  const checkedOnce = ref(false)
  const error = ref('')

  async function refresh(): Promise<void> {
    error.value = ''
    try {
      const [buildInfo, status] = await Promise.all([Build(), Status()])
      build.value = buildInfo
      update.value = status
      autoUpdate.value = status.enabled
    } catch (err) {
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

  async function checkForUpdates(): Promise<void> {
    checking.value = true
    error.value = ''
    try {
      update.value = await CheckNow()
      checkedOnce.value = true
    } catch (err) {
      error.value = errText(err)
    } finally {
      checking.value = false
    }
  }

  // Guarded by a non-empty url — dev builds have no releaseUrl, so the pane
  // only wires a link when there is somewhere to go.
  async function openExternal(url: string | undefined): Promise<void> {
    if (!url) return
    error.value = ''
    try {
      await Browser.OpenURL(url)
    } catch (err) {
      error.value = errText(err)
    }
  }

  return {
    build,
    update,
    autoUpdate,
    checking,
    checkedOnce,
    error,
    refresh,
    setAutoUpdate,
    checkForUpdates,
    openReleaseNotes: () => openExternal(build.value?.releaseUrl),
    openRepo: () => openExternal(build.value?.repoUrl),
  }
}
