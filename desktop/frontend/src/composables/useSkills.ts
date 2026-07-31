import { ref } from 'vue'
import {
  Catalog,
  InstallTarget,
  SetAutoUpdate,
  SetTargetDir,
  Sync,
  UninstallTarget,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/skillsservice'
import type {
  SkillsCatalog,
  SkillsSyncResult,
  SkillsTargetResult,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/models'
import { commands } from '../keybindings/catalog'

// The Skills settings surface installs the paste-ready prompts as agent skills.
// Management is per agent, all-or-nothing: install every skill to an agent or
// remove them all. Like usePrompts it supplies the one fact Go cannot know — the
// bindable command catalog the keyboard-shortcuts skill renders from — and is
// otherwise transport, updating the same reactive catalog on every call.
//
// The catalog it sends includes the user's own launchers, so the shortcuts
// skill an agent reads lists the launcher ids it can actually bind.

function promptInput() {
  return {
    commands: commands.value.map((command) => ({
      id: command.id,
      title: command.title,
      group: command.group,
      context: command.context,
      defaultCombos: command.defaultCombos,
    })),
    webhookPath: '',
    webhookSample: '',
  }
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : 'Something went wrong.'
}

export function useSkills() {
  const catalog = ref<SkillsCatalog | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  // The key of the in-flight mutation, so a view can disable just the control
  // that fired it rather than the whole page.
  const busy = ref<string | null>(null)

  async function refresh(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      catalog.value = await Catalog(promptInput())
    } catch (err) {
      error.value = errText(err)
      catalog.value = null
    } finally {
      loading.value = false
    }
  }

  async function run<T extends { catalog: SkillsCatalog }>(key: string, op: () => Promise<T>): Promise<T | null> {
    busy.value = key
    error.value = null
    try {
      const result = await op()
      catalog.value = result.catalog
      return result
    } catch (err) {
      error.value = errText(err)
      return null
    } finally {
      busy.value = null
    }
  }

  async function setTargetDir(targetID: string, dir: string): Promise<void> {
    busy.value = `target:${targetID}`
    error.value = null
    try {
      catalog.value = await SetTargetDir(promptInput(), targetID, dir)
    } catch (err) {
      error.value = errText(err)
    } finally {
      busy.value = null
    }
  }

  async function setAutoUpdate(enabled: boolean): Promise<void> {
    busy.value = 'auto-update'
    error.value = null
    try {
      catalog.value = await SetAutoUpdate(promptInput(), enabled)
    } catch (err) {
      error.value = errText(err)
    } finally {
      busy.value = null
    }
  }

  return {
    catalog,
    loading,
    error,
    busy,
    refresh,
    setTargetDir,
    setAutoUpdate,
    installTarget: (targetID: string): Promise<SkillsTargetResult | null> =>
      run(`target:${targetID}`, () => InstallTarget(promptInput(), targetID)),
    uninstallTarget: (targetID: string): Promise<SkillsTargetResult | null> =>
      run(`target:${targetID}`, () => UninstallTarget(promptInput(), targetID)),
    sync: (): Promise<SkillsSyncResult | null> => run('sync', () => Sync(promptInput())),
  }
}
