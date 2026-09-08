<script setup lang="ts">
// Create/edit editor for an agent workspace's manifest, in the app's
// DrawerSheet editor shell (the ActionEditor pattern). It writes the fields
// it shows — name, command, and the mcps and skills lists (plus the
// directory name at creation); hand-written comments in the YAML survive the
// write untouched. Both capability lists work the same way: a shared library
// on disk declares what exists (mcps.yaml, skills.yml) and the toggle is this
// workspace's own. The skills list names packages, not individual skills
// (ADR skill-packages-are-the-unit-a-workspace-enables). Deleting the workspace also lives here — the editor
// is the workspace's whole management surface, so its sidebar row needs no
// menu. Delete follows FolderEditModal.vue's shape: a quiet footer action
// that expands into an InlineConfirm over a dimmed, inert form.
import { computed, h, markRaw, nextTick, onMounted, ref, watch } from 'vue'
import type { Component } from 'vue'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconExternalLink from '~icons/lucide/external-link'
import IconFolderCog from '~icons/lucide/folder-cog'
import IconFolderOpen from '~icons/lucide/folder-open'
import IconPencil from '~icons/lucide/pencil'
import IconTrash2 from '~icons/lucide/trash-2'
import IconTriangleAlert from '~icons/lucide/triangle-alert'
import IconX from '~icons/lucide/x'
import AgentIcon, { agentHasIcon } from './AgentIcon.vue'
import AppSelect, { type AppSelectOption } from './AppSelect.vue'
import AppSwitch from './AppSwitch.vue'
import BaseButton from './BaseButton.vue'
import DrawerSheet from './DrawerSheet.vue'
import InlineConfirm from './InlineConfirm.vue'
import { CodeField } from '../pipeline/fields'
import { useAgentWorkspaces } from '../composables/useAgentWorkspaces'
import type { AgentWorkspace, SkillPackageMember, WorkspaceEditRequest } from '../lib/agentWorkspacesClient'

const props = defineProps<{
  /** The workspace being edited, or null to create one. */
  workspace?: AgentWorkspace | null
  busy?: boolean
  error?: string | null
}>()
const emit = defineEmits<{ close: []; save: [request: WorkspaceEditRequest]; delete: [dir: string] }>()

const {
  root, editor, mcpCatalogue, skillPackages, skillNames, skillPackagesProblem, presets,
  reloadMCPCatalogue, importMCPServers, removeMCPServer,
  reloadSkillPackages, revealSkillPackages, revealSharedSkills,
  openWorkspaceInEditor, revealWorkspace,
} = useAgentWorkspaces()

const creating = computed(() => !props.workspace)

// The confirm strip names the folder it is about to delete, in full: the
// directory name alone reads as a label in the app, not as a path on disk.
const deletedPath = computed(() => (root.value ? `${root.value}/${props.workspace?.dir ?? ''}` : props.workspace?.dir ?? ''))

const dir = ref(props.workspace?.dir ?? '')
const name = ref(props.workspace?.name ?? '')
const command = ref(props.workspace?.command ?? '')
const selectedMCPs = ref<string[]>([...(props.workspace?.mcps ?? [])])
const selectedSkills = ref<string[]>([...(props.workspace?.skills ?? [])])

const CUSTOM = '__custom__'

// Without this the derived selection below snaps back to a suggestion the
// moment the typed command matches one again.
const editingCommand = ref(false)

const selection = computed<string>({
  get: () => (editingCommand.value ? CUSTOM : presets.value.find((p) => p.command === command.value)?.id ?? CUSTOM),
  set(id) {
    if (id === CUSTOM) {
      editingCommand.value = true
      return
    }
    const preset = presets.value.find((p) => p.id === id)
    if (!preset) return
    command.value = preset.command
    editingCommand.value = false
  },
})

// AppSelect takes a bare component, with no props to pass an agent through.
// Memoized: a fresh component identity on every render remounts every row.
const agentIcons = new Map<string, Component>()
function agentIcon(agent: string): Component | undefined {
  if (!agentHasIcon(agent)) return undefined
  let icon = agentIcons.get(agent)
  if (!icon) {
    icon = markRaw(() => h(AgentIcon, { id: agent }))
    agentIcons.set(agent, icon)
  }
  return icon
}

// A hive-seeded preset's label is its profile key, which is already the agent
// when the profile just runs that CLI.
const presetOptions = computed<AppSelectOption[]>(() => [
  ...presets.value.map((preset) => ({
    value: preset.id,
    label: preset.label === preset.agent ? preset.agent : `${preset.agent} · ${preset.label}`,
    hint: preset.command,
    icon: agentIcon(preset.agent),
  })),
  { value: CUSTOM, label: 'Custom', hint: 'Write the invocation yourself', icon: markRaw(IconPencil) },
])

// A watch, not an initializer: presets arrive with the area's overview, which
// can land after this sheet is mounted.
watch(presets, (rows) => {
  if (!creating.value || command.value || !rows.length) return
  command.value = rows[0].command
}, { immediate: true })

// DANGEROUS_FLAGS mirrors agentws's own list. It is duplicated rather than
// served because it only drives a warning: a flag missing here shows no
// banner, which is the same "not recognized" the Go side means, and the
// manifest's own danger flag still labels the saved row.
const DANGEROUS_FLAGS = [
  '--dangerously-skip-permissions',
  '--dangerously-bypass-approvals-and-sandbox',
  '--yolo',
  '--full-auto',
]
const commandIsDangerous = computed(() => DANGEROUS_FLAGS.some((flag) => command.value.includes(flag)))

// Kept in step with agentws.LaunchData.
const TEMPLATE_FIELDS = [
  { field: '{{ .Dir }}', means: 'the workspace directory on disk' },
  { field: '{{ .MCPConfig }}', means: 'the MCP config Hive generates for this workspace' },
  { field: '{{ .SessionID }}', means: 'the id Hive minted for this chat' },
  { field: '{{ .Resume }}', means: 'true when reopening a chat, false on a new one' },
  { field: 'shq', means: 'quotes a value for the shell — pipe every path through it' },
]

const valid = computed(
  () => !!name.value.trim() && !!command.value.trim() && (!creating.value || !!dir.value.trim()),
)

const confirming = ref(false)

// ── MCP rows ─────────────────────────────────────────────────────────────────
// The catalogue's rows plus any declared id the catalogue no longer resolves,
// so a hand-authored entry that went missing is visible and removable rather
// than silently kept.
interface MCPRow {
  id: string
  title: string
  command: string
  problem: string
  shipped: boolean
  stability: string
  shadows: string
  missing: boolean
}

const mcpRows = computed<MCPRow[]>(() => {
  const rows: MCPRow[] = mcpCatalogue.value.map((e) => ({
    id: e.id, title: e.title || e.id, command: e.command, problem: e.problem,
    shipped: e.shipped, stability: e.stability, shadows: e.shadows, missing: false,
  }))
  const known = new Set(rows.map((r) => r.id))
  for (const id of selectedMCPs.value) {
    if (!known.has(id)) rows.push({ id, title: id, command: '', problem: '', shipped: false, stability: '', shadows: '', missing: true })
  }
  return rows
})

function mcpEnabled(id: string): boolean {
  return selectedMCPs.value.includes(id)
}

function toggleMCP(id: string): void {
  selectedMCPs.value = mcpEnabled(id)
    ? selectedMCPs.value.filter((x) => x !== id)
    : [...selectedMCPs.value, id]
}

const mcpError = ref('')

async function removeServer(id: string): Promise<void> {
  mcpError.value = ''
  try {
    await removeMCPServer(id)
    selectedMCPs.value = selectedMCPs.value.filter((x) => x !== id)
  } catch (failure) {
    mcpError.value = failure instanceof Error ? failure.message : 'The server could not be removed.'
  }
}

// ── Skill package rows ───────────────────────────────────────────────────────
// A workspace enables packages, not skills: skills.yml defines each package as
// glob patterns, and the rows show what those patterns select right now. An
// enabled name skills.yml no longer defines still rows, so it can be switched
// off rather than silently selecting nothing.
interface SkillPackageRow {
  name: string
  title: string
  description: string
  members: SkillPackageMember[]
  missing: boolean
  /** Why the name does not resolve, when it does not. */
  warning: string
}

const skillRows = computed<SkillPackageRow[]>(() => {
  const rows: SkillPackageRow[] = skillPackages.value.map((pkg) => ({
    name: pkg.name, title: pkg.title || pkg.name, description: pkg.description,
    members: pkg.members, missing: false, warning: '',
  }))
  const known = new Set(rows.map((r) => r.name))
  const bySlug = new Map(skillNames.value.map((skill) => [skill.slug, skill]))
  for (const name of selectedSkills.value) {
    if (known.has(name)) continue
    rows.push({ name, title: name, description: '', members: [], missing: true, warning: missingSkillWarning(name, bySlug.get(name)?.selectedBy) })
  }
  return rows
})

// A name skills.yml does not define is a package that never existed *or* a
// skill slug from a manifest written before packages were the enablement unit
// (#307). Only the second has a fix on screen, and saying "not defined in
// skills.yml" for both hides it.
function missingSkillWarning(name: string, selectedBy: string[] | undefined): string {
  if (!selectedBy) return 'not defined in skills.yml — an enabled package without a definition brings nothing'
  if (!selectedBy.length) return 'a skill, not a package — no package selects it yet, so define one in skills.yml'
  const packages = selectedBy.map((pkg) => `"${pkg}"`).join(' or ')
  return `a skill, not a package — the ${packages} package selects it, so enable that and switch this off`
}

function skillEnabled(name: string): boolean {
  return selectedSkills.value.includes(name)
}

function toggleSkill(name: string): void {
  selectedSkills.value = skillEnabled(name)
    ? selectedSkills.value.filter((x) => x !== name)
    : [...selectedSkills.value, name]
}

// A package's members are worth seeing before enabling it — it is the whole
// authority the package grants — but not worth the height by default, so the
// list expands on demand.
const expandedSkillPackages = ref<string[]>([])

function skillPackageExpanded(name: string): boolean {
  return expandedSkillPackages.value.includes(name)
}

function toggleSkillPackageExpanded(name: string): void {
  expandedSkillPackages.value = skillPackageExpanded(name)
    ? expandedSkillPackages.value.filter((x) => x !== name)
    : [...expandedSkillPackages.value, name]
}

const skillError = ref('')

async function openSkillPackages(): Promise<void> {
  skillError.value = ''
  try {
    await revealSkillPackages()
  } catch (failure) {
    skillError.value = failure instanceof Error ? failure.message : 'skills.yml could not be opened.'
  }
}

async function openSharedSkills(): Promise<void> {
  skillError.value = ''
  try {
    await revealSharedSkills()
  } catch (failure) {
    skillError.value = failure instanceof Error ? failure.message : 'The skills folder could not be opened.'
  }
}

// ── Paste-in JSON import ─────────────────────────────────────────────────────
const importOpen = ref(false)
const importText = ref('')
const importBusy = ref(false)
const importPlaceholder = '{"mcpServers": {"my-server": {"command": "npx", "args": ["-y", "…"]}}}'

function formatImportJSON(): void {
  mcpError.value = ''
  try {
    importText.value = JSON.stringify(JSON.parse(importText.value), null, 2)
  } catch (failure) {
    mcpError.value = failure instanceof Error ? `Not valid JSON: ${failure.message}` : 'Not valid JSON.'
  }
}

async function submitImport(): Promise<void> {
  if (!importText.value.trim() || importBusy.value) return
  importBusy.value = true
  mcpError.value = ''
  try {
    const added = await importMCPServers(importText.value)
    for (const id of added) {
      if (!mcpEnabled(id)) selectedMCPs.value = [...selectedMCPs.value, id]
    }
    importText.value = ''
    importOpen.value = false
  } catch (failure) {
    mcpError.value = failure instanceof Error ? failure.message : 'The pasted configuration could not be imported.'
  } finally {
    importBusy.value = false
  }
}

// ── Open the directory outside the app ───────────────────────────────────────
const actionError = ref('')

async function openInEditor(): Promise<void> {
  actionError.value = ''
  try {
    await openWorkspaceInEditor(props.workspace!.dir)
  } catch (failure) {
    actionError.value = failure instanceof Error ? failure.message : 'The editor could not be opened.'
  }
}

async function reveal(): Promise<void> {
  actionError.value = ''
  try {
    await revealWorkspace(props.workspace!.dir)
  } catch (failure) {
    actionError.value = failure instanceof Error ? failure.message : 'The directory could not be opened.'
  }
}

function submit(): void {
  if (props.busy || confirming.value || !valid.value) return
  emit('save', {
    dir: creating.value ? dir.value.trim() : props.workspace!.dir,
    name: name.value.trim(),
    command: command.value.trim(),
    mcps: selectedMCPs.value,
    skills: selectedSkills.value,
  })
}

function cancel(): void {
  if (!props.busy && !confirming.value) emit('close')
}

const nameInput = ref<HTMLInputElement | null>(null)
const dirInput = ref<HTMLInputElement | null>(null)
onMounted(async () => {
  void reloadMCPCatalogue()
  void reloadSkillPackages()
  await nextTick()
  ;(creating.value ? dirInput.value : nameInput.value)?.focus()
})
</script>

<template>
  <DrawerSheet
    :ariaLabel="creating ? 'New workspace' : 'Edit workspace'"
    testid="agent-workspace-editor"
    :default-size="460"
    :close-on-escape="!confirming"
    :close-on-backdrop="!confirming"
    @close="cancel"
  >
    <template #header>
      <div class="flex items-center gap-3">
        <span class="flex size-[38px] items-center justify-center rounded-[10px] bg-accent text-accent-contrast"><IconFolderCog class="size-[18px]" /></span>
        <div class="min-w-0 flex-1">
          <div class="text-[15px] font-semibold tracking-[-.01em]">{{ creating ? 'New workspace' : 'Edit workspace' }}</div>
          <div class="truncate font-mono text-[12px] text-text-3">{{ creating ? 'A directory an agent works in' : workspace!.dir }}</div>
        </div>
        <button class="text-text-3 hover:text-text disabled:opacity-50" aria-label="Close" :disabled="busy || confirming" @click="cancel"><IconX class="size-4" /></button>
      </div>
    </template>

    <!-- While a delete is pending the form recedes: dimmed and inert, so the
         two states can't be misread for each other (FolderEditModal's rule). -->
    <div class="flex flex-col gap-4 transition-opacity" :class="{ 'pointer-events-none opacity-45': confirming }">
      <div v-if="!creating" class="flex flex-col gap-1.5">
        <div class="flex items-center gap-2">
          <button
            v-if="editor.command"
            type="button"
            class="flex cursor-pointer items-center gap-1.5 rounded-[7px] border border-card px-2.5 py-1.5 text-[12px] font-medium text-text-2 hover:border-strong hover:text-text disabled:opacity-50"
            :disabled="busy"
            data-testid="agent-workspace-editor-open-editor"
            @click="openInEditor"
          ><IconExternalLink class="size-3.5" />Open in {{ editor.title }}</button>
          <button
            type="button"
            class="flex cursor-pointer items-center gap-1.5 rounded-[7px] border border-card px-2.5 py-1.5 text-[12px] font-medium text-text-2 hover:border-strong hover:text-text disabled:opacity-50"
            :disabled="busy"
            data-testid="agent-workspace-editor-reveal"
            @click="reveal"
          ><IconFolderOpen class="size-3.5" />Show in Finder</button>
        </div>
        <span v-if="!editor.command" class="text-xs text-text-4">Pick a default editor in Settings › General to open this directory in it.</span>
        <p v-if="actionError" class="text-xs text-severity-error" data-testid="agent-workspace-editor-action-error">{{ actionError }}</p>
      </div>

      <div v-if="creating" class="flex flex-col gap-1.5">
        <label for="agent-workspace-dir" class="text-xs text-text-3">Directory name</label>
        <input
          id="agent-workspace-dir"
          ref="dirInput"
          v-model="dir"
          type="text"
          placeholder="my-project"
          autocapitalize="off"
          autocorrect="off"
          spellcheck="false"
          :disabled="busy"
          class="w-full rounded-lg border border-strong bg-raised px-3 py-2.5 font-mono text-[13px] text-text outline-none focus:border-accent"
          data-testid="agent-workspace-editor-dir"
          @keydown.enter="submit"
        >
        <span class="text-xs text-text-4">A new directory under the workspace root, seeded with an AGENTS.md to shape.</span>
      </div>

      <div class="flex flex-col gap-1.5">
        <label for="agent-workspace-name" class="text-xs text-text-3">Name</label>
        <input
          id="agent-workspace-name"
          ref="nameInput"
          v-model="name"
          type="text"
          :disabled="busy"
          class="w-full rounded-lg border border-strong bg-raised px-3 py-2.5 text-[13.5px] text-text outline-none focus:border-accent"
          data-testid="agent-workspace-editor-name"
          @keydown.enter="submit"
        >
      </div>

      <div class="flex flex-col gap-1.5" data-testid="agent-workspace-editor-command">
        <span class="text-xs text-text-3">Command</span>
        <AppSelect
          v-model="selection"
          :options="presetOptions"
          searchable
          search-placeholder="Search commands…"
          aria-label="Command"
          testid="agent-workspace-editor-command-preset"
          :disabled="busy"
        />
        <p
          v-if="selection !== CUSTOM"
          class="truncate font-mono text-[11px] text-text-4"
          :title="command"
          data-testid="agent-workspace-editor-command-preview"
        >{{ command }}</p>
        <template v-else>
          <textarea
            v-model="command"
            rows="4"
            spellcheck="false"
            :disabled="busy"
            placeholder="claude --session-id {{ .SessionID }}"
            class="w-full resize-y rounded-lg border bg-raised px-3 py-2.5 font-mono text-[12px] leading-relaxed text-text outline-none focus:border-accent"
            :class="commandIsDangerous ? 'border-severity-warning' : 'border-strong'"
            data-testid="agent-workspace-editor-command-input"
          />
          <div class="flex flex-col gap-1 rounded-lg border border-card px-3 py-2.5" data-testid="agent-workspace-editor-command-fields">
            <p class="text-[11px] leading-relaxed text-text-3">Hive renders this as a Go template each time a chat starts, and fills in:</p>
            <dl class="flex flex-col gap-1">
              <div v-for="entry in TEMPLATE_FIELDS" :key="entry.field" class="flex flex-wrap items-baseline gap-x-2">
                <dt class="shrink-0 font-mono text-[11px] text-text-2">{{ entry.field }}</dt>
                <dd class="min-w-0 flex-1 text-[11px] leading-relaxed text-text-4">{{ entry.means }}</dd>
              </div>
            </dl>
          </div>
        </template>
        <p
          v-if="commandIsDangerous"
          class="flex items-start gap-1.5 text-[11.5px] leading-relaxed text-severity-warning"
          data-testid="agent-workspace-editor-command-danger"
        >
          <IconTriangleAlert class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
          <span>This command bypasses the agent's permission prompts. Unattended, it can take any action your user account can, including through every enabled MCP server.</span>
        </p>
      </div>

      <div class="flex flex-col gap-1.5" data-testid="agent-workspace-editor-mcps">
        <span class="text-xs text-text-3">MCP servers</span>
        <div v-if="mcpRows.length" class="flex flex-col divide-y divide-row rounded-lg border border-strong bg-raised">
          <div v-for="row in mcpRows" :key="row.id" class="flex items-start gap-2.5 px-3 py-2.5">
            <AppSwitch
              size="sm"
              class="mt-0.5"
              :model-value="mcpEnabled(row.id)"
              :aria-label="`Enable ${row.title}`"
              :disabled="busy"
              :testid="`agent-workspace-editor-mcp-${row.id}`"
              @update:model-value="toggleMCP(row.id)"
            />
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-1.5">
                <span class="truncate text-[13px] text-text">{{ row.title }}</span>
                <span
                  class="shrink-0 rounded-full border border-card px-1.5 py-px text-[10px] text-text-4"
                >{{ row.shipped ? row.stability : 'custom' }}</span>
                <span v-if="row.shadows" class="shrink-0 text-[10px] text-severity-warning">replaces shipped</span>
              </div>
              <div v-if="row.command" class="truncate font-mono text-[11px] text-text-4" :title="row.command">{{ row.command }}</div>
              <div v-if="row.problem" class="text-[11px] text-severity-warning">{{ row.problem }}</div>
              <div v-if="row.missing" class="text-[11px] text-severity-warning">not in the catalogue — enabled ids without an entry are skipped at launch</div>
            </div>
            <button
              v-if="!row.shipped && !row.missing"
              type="button"
              class="mt-0.5 shrink-0 cursor-pointer text-text-4 hover:text-severity-error disabled:opacity-50"
              :title="`Remove ${row.title} from mcps.yaml (every workspace loses it)`"
              :aria-label="`Remove ${row.title}`"
              :disabled="busy"
              :data-testid="`agent-workspace-editor-mcp-remove-${row.id}`"
              @click="removeServer(row.id)"
            ><IconTrash2 class="size-3.5" /></button>
          </div>
        </div>

        <div v-if="importOpen" class="flex flex-col gap-1.5">
          <CodeField
            v-model="importText"
            :rows="8"
            :placeholder="importPlaceholder"
            testid="agent-workspace-editor-mcp-import-text"
          />
          <div class="flex items-center gap-2">
            <BaseButton size="sm" :busy="importBusy" :disabled="!importText.trim()" data-testid="agent-workspace-editor-mcp-import-submit" @click="submitImport">Add servers</BaseButton>
            <BaseButton
              variant="secondary"
              size="sm"
              :disabled="importBusy || !importText.trim()"
              data-testid="agent-workspace-editor-mcp-import-format"
              @click="formatImportJSON"
            >Format JSON</BaseButton>
            <BaseButton variant="secondary" size="sm" :disabled="importBusy" @click="importOpen = false">Cancel</BaseButton>
          </div>
          <span class="text-xs text-text-4">Pasted servers land in mcps.yaml — the library every workspace picks from — and switch on here.</span>
        </div>
        <button
          v-else
          type="button"
          class="self-start cursor-pointer text-[12px] text-accent hover:underline"
          :disabled="busy"
          data-testid="agent-workspace-editor-mcp-import"
          @click="importOpen = true"
        >Add servers from JSON…</button>
        <p v-if="mcpError" class="text-xs text-severity-error" data-testid="agent-workspace-editor-mcp-error">{{ mcpError }}</p>
      </div>

      <div class="flex flex-col gap-1.5" data-testid="agent-workspace-editor-skills">
        <span class="text-xs text-text-3">Skill packages</span>
        <div v-if="skillRows.length" class="flex flex-col divide-y divide-row rounded-lg border border-strong bg-raised">
          <div v-for="row in skillRows" :key="row.name" class="flex flex-col">
            <div class="flex items-start gap-2.5 px-3 py-2.5">
              <AppSwitch
                size="sm"
                class="mt-0.5"
                :model-value="skillEnabled(row.name)"
                :aria-label="`Enable ${row.title}`"
                :disabled="busy"
                :testid="`agent-workspace-editor-skill-${row.name}`"
                @update:model-value="toggleSkill(row.name)"
              />
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1.5">
                  <span class="truncate text-[13px] text-text">{{ row.title }}</span>
                  <button
                    v-if="!row.missing"
                    type="button"
                    class="flex shrink-0 cursor-pointer items-center gap-1 text-[11px] text-text-4 hover:text-text-3"
                    :aria-expanded="skillPackageExpanded(row.name)"
                    :data-testid="`agent-workspace-editor-skill-members-${row.name}`"
                    @click="toggleSkillPackageExpanded(row.name)"
                  >
                    {{ row.members.length }} {{ row.members.length === 1 ? 'skill' : 'skills' }}
                    <IconChevronDown class="size-3 transition-transform" :class="{ '-rotate-90': !skillPackageExpanded(row.name) }" />
                  </button>
                </div>
                <div v-if="row.description" class="text-[11.5px] leading-relaxed text-text-3">{{ row.description }}</div>
                <div v-if="!row.missing && !row.members.length" class="text-[11px] text-severity-warning">matches no skill — check its patterns in skills.yml</div>
                <div v-if="row.warning" class="text-[11px] text-severity-warning">{{ row.warning }}</div>
              </div>
            </div>
            <ul v-if="skillPackageExpanded(row.name) && row.members.length" class="flex flex-col gap-1 border-t border-row px-3 py-2 pl-9">
              <li v-for="member in row.members" :key="member.slug" class="flex items-center gap-1.5">
                <span class="truncate font-mono text-[11.5px] text-text-3">{{ member.slug }}</span>
                <span class="shrink-0 rounded-full border border-card px-1.5 py-px text-[10px] text-text-4">{{ member.shipped ? 'shipped' : 'custom' }}</span>
              </li>
            </ul>
          </div>
        </div>
        <span v-else class="text-xs text-text-4">No packages are defined yet.</span>
        <div class="flex items-center gap-3">
          <button
            type="button"
            class="cursor-pointer text-[12px] text-accent hover:underline"
            :disabled="busy"
            data-testid="agent-workspace-editor-skills-packages"
            @click="openSkillPackages"
          >Edit skills.yml…</button>
          <button
            type="button"
            class="cursor-pointer text-[12px] text-accent hover:underline"
            :disabled="busy"
            data-testid="agent-workspace-editor-skills-shared"
            @click="openSharedSkills"
          >Open the skills folder…</button>
        </div>
        <span class="text-xs text-text-4">A package is glob patterns over skill names in skills.yml. Names come from the skills Hive ships and the SKILL.md files under .shared/skills.</span>
        <p v-if="skillPackagesProblem" class="text-xs text-severity-error" data-testid="agent-workspace-editor-skills-problem">{{ skillPackagesProblem }}</p>
        <p v-if="skillError" class="text-xs text-severity-error" data-testid="agent-workspace-editor-skill-error">{{ skillError }}</p>
      </div>

      <p v-if="!creating" class="text-xs leading-relaxed text-text-4">
        Saving rewrites these fields in agent-workspace.yaml and re-syncs the workspace's
        generated files. Comments and anything else in the file stay as written — edit the
        file for those.
      </p>
      <p
        v-if="error && !confirming"
        class="rounded border border-severity-error bg-severity-error-tint px-3 py-2 text-xs text-severity-error"
        data-testid="agent-workspace-editor-error"
      >{{ error }}</p>
    </div>

    <template #footer>
      <!-- The strip escapes the footer's own padding so it reads as the
           sheet's bottom edge, the way InlineConfirm is designed to sit. -->
      <InlineConfirm
        v-if="confirming"
        class="-mx-[18px] -my-[13px]"
        title="Delete this workspace?"
        :description="`Every live chat in ${workspace!.name || workspace!.dir} closes, its chat history is removed, and ${deletedPath} is deleted from disk with everything in it — AGENTS.md, docs, canvases, and anything else written there. This cannot be undone.`"
        confirm-label="Delete"
        :busy="busy"
        :error="error"
        testid="agent-workspace-editor-delete-confirm"
        @confirm="emit('delete', workspace!.dir)"
        @cancel="confirming = false"
      />
      <div v-else class="flex items-center gap-2.5">
        <button
          v-if="!creating"
          type="button"
          class="cursor-pointer text-[12.5px] text-text-3 hover:text-severity-error disabled:opacity-50"
          :disabled="busy"
          data-testid="agent-workspace-editor-delete"
          @click="confirming = true"
        >Delete workspace</button>
        <div class="flex-1" />
        <BaseButton variant="secondary" size="sm" :busy="busy" data-testid="agent-workspace-editor-cancel" @click="cancel">Cancel</BaseButton>
        <BaseButton size="sm" :busy="busy" :disabled="!valid" data-testid="agent-workspace-editor-save" @click="submit">
          {{ creating ? 'Create workspace' : 'Save' }}
        </BaseButton>
      </div>
    </template>
  </DrawerSheet>
</template>
