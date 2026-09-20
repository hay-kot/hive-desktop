import { computed, ref } from 'vue'
import {
  InspectWorkspace,
  Save,
  Setup,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/hiveconfigservice'
import { ChooseDirectory } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/systemservice'
import type { HiveSetup } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/models'
import type { AgentOption, Profile, Workspace } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/hiveconf/models'

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/**
 * A workspace in the draft. `repos` is the count the backend found when the
 * folder was added or loaded; it is a confirmation that the folder is the one
 * the user meant, not a promise about the launcher's list.
 */
export interface DraftWorkspace {
  path: string
  exists: boolean
  repos: number
}

/**
 * useHiveSetup holds an editable draft of the Hive CLI configuration — the
 * agents a session can start with and the folders holding the repositories it
 * can start in.
 *
 * The draft is deliberately whole rather than a set of pending changes: the
 * backend reconciles the file to exactly what it is given, so the profiles
 * this never shows a control for (a hand-written one with a custom command)
 * ride along untouched instead of being dropped by a save about workspaces.
 *
 * Every caller gets its own instance. The two screens that use it — first run
 * and Settings ▸ Hive CLI — are never open at the same time, and a shared
 * draft between them would mean a half-finished first run leaking into the
 * settings form.
 */
export function useHiveSetup() {
  const setup = ref<HiveSetup | null>(null)
  const loading = ref(false)
  const saving = ref(false)
  const error = ref('')

  // The draft. `profiles` holds every profile the file declares, keyed edits
  // included; `selected` is the subset of catalog agents the picker shows as
  // chosen, derived from it.
  const profiles = ref<Profile[]>([])
  const workspaces = ref<DraftWorkspace[]>([])
  const defaultAgent = ref('')
  const skipPermissions = ref(false)

  const agents = computed<AgentOption[]>(() => setup.value?.agents ?? [])
  const path = computed(() => setup.value?.config.path ?? '')
  const usable = computed(() => setup.value?.config.usable ?? false)
  const unreadable = computed(() => setup.value?.config.unreadable ?? '')
  const defaultAgentOverride = computed(() => setup.value?.defaultAgentOverride ?? '')

  /** The catalog agents currently in the draft, by name. */
  const selectedAgents = computed(() => new Set(profiles.value.map(p => p.name)))

  /**
   * Profiles in the draft that the agent picker has no row for: a
   * hand-written profile, or one from a hive version that knows an agent this
   * build does not. They are listed so a user can see what a save will keep.
   */
  const customProfiles = computed(() => {
    const known = new Set(agents.value.map(a => a.name))
    return profiles.value.filter(p => !known.has(p.name))
  })

  /**
   * Whether the draft differs from what is on disk. Compared field by field
   * rather than by a JSON round-trip so key order in the generated bindings
   * cannot make an untouched form look edited.
   */
  const dirty = computed(() => {
    const saved = setup.value?.config
    if (!saved) return false
    if ((saved.defaultAgent || '') !== defaultAgent.value) return true
    const savedWorkspaces = (saved.workspaces ?? []).map(w => w.path)
    if (savedWorkspaces.length !== workspaces.value.length) return true
    if (savedWorkspaces.some((path, i) => path !== workspaces.value[i]?.path)) return true
    const savedProfiles = saved.profiles ?? []
    if (savedProfiles.length !== profiles.value.length) return true
    return savedProfiles.some((saved, i) => {
      const draft = profiles.value[i]
      if (!draft || saved.name !== draft.name || saved.command !== draft.command) return true
      const a = saved.flags ?? []
      const b = draft.flags ?? []
      return a.length !== b.length || a.some((flag, j) => flag !== b[j])
    })
  })

  const canSave = computed(() =>
    profiles.value.length > 0
    && workspaces.value.length > 0
    && profiles.value.some(p => p.name === defaultAgent.value),
  )

  async function load(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const next = await Setup()
      setup.value = next
      resetDraft(next)
    } catch (err) {
      error.value = errText(err)
    } finally {
      loading.value = false
    }
  }

  function resetDraft(next: HiveSetup): void {
    profiles.value = (next.config.profiles ?? []).map(p => ({ ...p, flags: p.flags ? [...p.flags] : null }))
    workspaces.value = (next.config.workspaces ?? []).map(toDraftWorkspace)
    defaultAgent.value = next.config.defaultAgent || profiles.value[0]?.name || ''
    // Reflect what the file already says rather than defaulting the toggle
    // off: a user who turned skip-permissions on should not have it silently
    // turned back off by opening the form and saving.
    //
    // Read from catalog profiles only. A hand-written profile carrying flags
    // of its own says nothing about this toggle, and letting it light the box
    // up would arm a control that then rewrites the profiles it does own.
    const known = new Set((next.agents ?? []).map(a => a.name))
    skipPermissions.value = profiles.value.some(p => known.has(p.name) && (p.flags?.length ?? 0) > 0)
    if (!profiles.value.length) seedFromInstalledAgent(next)
  }

  // A first run with nothing configured starts on the agent this machine
  // actually has, so the common case is one click. With none installed it
  // starts on nothing and the user picks — guessing an agent that is not here
  // would be worse than asking.
  function seedFromInstalledAgent(next: HiveSetup): void {
    const installed = (next.agents ?? []).find(a => a.installed)
    if (!installed) return
    toggleAgent(installed, true)
    defaultAgent.value = installed.name
  }

  function toDraftWorkspace(w: Workspace): DraftWorkspace {
    return { path: w.path, exists: w.exists, repos: w.repos }
  }

  /** Flags for an agent under the current skip-permissions setting. */
  function flagsFor(agent: AgentOption): string[] | null {
    if (!skipPermissions.value) return []
    return agent.skipPermissionFlags?.length ? [...agent.skipPermissionFlags] : []
  }

  function toggleAgent(agent: AgentOption, on: boolean): void {
    if (on) {
      if (selectedAgents.value.has(agent.name)) return
      profiles.value = [...profiles.value, { name: agent.name, command: agent.name, flags: flagsFor(agent) }]
      if (!defaultAgent.value) defaultAgent.value = agent.name
      return
    }
    profiles.value = profiles.value.filter(p => p.name !== agent.name)
    // Removing the default would save a config hive refuses to load, so the
    // next remaining profile takes over rather than leaving it dangling.
    if (defaultAgent.value === agent.name) defaultAgent.value = profiles.value[0]?.name ?? ''
  }

  /**
   * Re-derive flags for every catalog profile in the draft. A custom profile
   * keeps whatever flags it was written with: the toggle is about the presets
   * this app offers, not about rewriting someone's own command line.
   */
  function setSkipPermissions(on: boolean): void {
    skipPermissions.value = on
    const catalog = new Map(agents.value.map(a => [a.name, a]))
    profiles.value = profiles.value.map((p) => {
      const agent = catalog.get(p.name)
      return agent ? { ...p, flags: flagsFor(agent) } : p
    })
  }

  /** Opens the native folder picker and adds what it returns. */
  async function addWorkspace(): Promise<void> {
    error.value = ''
    let chosen = ''
    try {
      chosen = await ChooseDirectory('Choose the folder that holds your repositories')
    } catch (err) {
      error.value = errText(err)
      return
    }
    if (!chosen) return
    await addWorkspacePath(chosen)
  }

  async function addWorkspacePath(path: string): Promise<void> {
    const trimmed = path.trim()
    if (!trimmed) return
    if (workspaces.value.some(w => w.path === trimmed)) return
    error.value = ''
    try {
      const found = await InspectWorkspace(trimmed)
      workspaces.value = [...workspaces.value, toDraftWorkspace(found)]
    } catch (err) {
      error.value = errText(err)
    }
  }

  function removeWorkspace(path: string): void {
    workspaces.value = workspaces.value.filter(w => w.path !== path)
  }

  /**
   * Writes the draft and reloads the running Hive services from it. Returns
   * whether the save landed, so a caller driving a wizard can advance only on
   * success.
   */
  async function save(): Promise<boolean> {
    if (saving.value) return false
    saving.value = true
    error.value = ''
    try {
      const next = await Save({
        defaultAgent: defaultAgent.value,
        profiles: profiles.value,
        workspaces: workspaces.value.map(w => w.path),
      })
      setup.value = next
      resetDraft(next)
      return true
    } catch (err) {
      error.value = errText(err)
      return false
    } finally {
      saving.value = false
    }
  }

  return {
    setup,
    loading,
    saving,
    error,
    agents,
    profiles,
    workspaces,
    defaultAgent,
    skipPermissions,
    selectedAgents,
    customProfiles,
    canSave,
    dirty,
    path,
    usable,
    unreadable,
    defaultAgentOverride,
    load,
    toggleAgent,
    setSkipPermissions,
    addWorkspace,
    addWorkspacePath,
    removeWorkspace,
    save,
  }
}
