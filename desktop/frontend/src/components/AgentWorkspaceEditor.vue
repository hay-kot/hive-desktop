<script setup lang="ts">
// Create/edit editor for an agent workspace's manifest, in the app's
// DrawerSheet editor shell (the ActionEditor pattern). It writes the fields
// it shows — name, agent, autonomy, and the mcps and skills lists (plus the
// directory name at creation); hand-written comments in the YAML survive the
// write untouched. Both capability lists work the same way: rows come from a
// merged catalogue (what the build ships plus what the user's library holds),
// the library is shared across workspaces, and the toggle is this workspace's
// own. Deleting the workspace also lives here — the editor
// is the workspace's whole management surface, so its sidebar row needs no
// menu. Delete follows FolderEditModal.vue's shape: a quiet footer action
// that expands into an InlineConfirm over a dimmed, inert form.
import { computed, nextTick, onMounted, ref } from 'vue'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconExternalLink from '~icons/lucide/external-link'
import IconFolderCog from '~icons/lucide/folder-cog'
import IconFolderOpen from '~icons/lucide/folder-open'
import IconTrash2 from '~icons/lucide/trash-2'
import IconTriangleAlert from '~icons/lucide/triangle-alert'
import IconX from '~icons/lucide/x'
import AppSelect, { type AppSelectOption } from './AppSelect.vue'
import AppSwitch from './AppSwitch.vue'
import BaseButton from './BaseButton.vue'
import DrawerSheet from './DrawerSheet.vue'
import InlineConfirm from './InlineConfirm.vue'
import { CodeField } from '../pipeline/fields'
import { useAgentWorkspaces } from '../composables/useAgentWorkspaces'
import type { AgentWorkspace, WorkspaceEditRequest } from '../lib/agentWorkspacesClient'

const props = defineProps<{
  /** The workspace being edited, or null to create one. */
  workspace?: AgentWorkspace | null
  agents: string[]
  busy?: boolean
  error?: string | null
}>()
const emit = defineEmits<{ close: []; save: [request: WorkspaceEditRequest]; delete: [dir: string] }>()

const {
  editor, mcpCatalogue, skillCatalogue, autonomyFlags,
  reloadMCPCatalogue, importMCPServers, removeMCPServer,
  reloadSkillCatalogue, revealSkillsLibrary,
  openWorkspaceInEditor, revealWorkspace,
} = useAgentWorkspaces()

const creating = computed(() => !props.workspace)

const dir = ref(props.workspace?.dir ?? '')
const name = ref(props.workspace?.name ?? '')
const agent = ref(props.workspace?.agent || props.agents[0] || '')
const autonomy = ref(props.workspace?.autonomy || 'ask')
const selectedMCPs = ref<string[]>([...(props.workspace?.mcps ?? [])])
const selectedSkills = ref<string[]>([...(props.workspace?.skills ?? [])])

const agentOptions = computed<AppSelectOption[]>(() => props.agents.map((a) => ({ value: a, label: a })))

// Every posture is laid out as a radio card rather than a dropdown, so the
// choice being made — especially full's dangerous bypass — is readable
// before it is selected. The flags line is the launch table's own projection
// for the chosen agent (ADR a-workspace-declares-its-own-authority): the UI shows what the posture actually
// runs, never a euphemism, and a posture the launch would refuse is disabled.
const AUTONOMY_META = [
  {
    value: 'ask',
    label: 'Ask',
    description: 'Every action needs your approval — the agent prompts before anything it is not sure of.',
    danger: false,
  },
  {
    value: 'auto',
    label: 'Auto',
    description: 'File edits are allowed without prompting; anything riskier still asks.',
    danger: false,
  },
  {
    value: 'full',
    label: 'Full — dangerously skip permissions',
    description: 'Bypasses the agent\'s permission prompts entirely. Unattended, it can take any action your user account can, including through every enabled MCP server.',
    danger: true,
  },
] as const

interface AutonomyOption {
  value: string
  label: string
  description: string
  danger: boolean
  flags: string
  unavailable: boolean
}

const autonomyOptions = computed<AutonomyOption[]>(() => {
  const known = autonomyFlags.value[agent.value]
  return AUTONOMY_META.map((meta) => {
    const flags = known?.[meta.value]
    return {
      ...meta,
      flags: flags?.length ? flags.join(' ') : '',
      // Only a loaded table can rule a posture out; with nothing loaded the
      // selector stays fully usable and simply shows no flag detail.
      unavailable: !!known && !flags,
    }
  })
})

function selectAutonomy(option: AutonomyOption): void {
  if (props.busy || option.unavailable) return
  autonomy.value = option.value
}

const valid = computed(() => !!name.value.trim() && !!agent.value && (!creating.value || !!dir.value.trim()))

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

// ── Skill rows ───────────────────────────────────────────────────────────────
// Same shape as the MCP rows, including the missing case: a slug the
// catalogue no longer resolves (a library skill deleted off disk) stays
// visible so it can be switched off, rather than silently doing nothing.
interface SkillRow {
  slug: string
  title: string
  description: string
  shipped: boolean
  shadows: string
  missing: boolean
}

const skillRows = computed<SkillRow[]>(() => {
  const rows: SkillRow[] = skillCatalogue.value.map((e) => ({
    slug: e.slug, title: e.title || e.slug, description: e.description,
    shipped: e.shipped, shadows: e.shadows, missing: false,
  }))
  const known = new Set(rows.map((r) => r.slug))
  for (const slug of selectedSkills.value) {
    if (!known.has(slug)) rows.push({ slug, title: slug, description: '', shipped: false, shadows: '', missing: true })
  }
  return rows
})

function skillEnabled(slug: string): boolean {
  return selectedSkills.value.includes(slug)
}

function toggleSkill(slug: string): void {
  selectedSkills.value = skillEnabled(slug)
    ? selectedSkills.value.filter((x) => x !== slug)
    : [...selectedSkills.value, slug]
}

// ── Skill groups ─────────────────────────────────────────────────────────────
// A group is the slug prefix before the first hyphen, and it exists only when
// two or more skills share one — hive-mcp and hive-flows make a "hive" group;
// a lone release-notes stays a plain row rather than becoming a group of one.
// This is presentation, derived here rather than declared in the catalogue: a
// group is not a thing a workspace can enable, and the manifest still names
// individual slugs. A collapsed group's header carries its enabled count, so
// a long list stays readable without expanding anything.
interface SkillGroup {
  name: string
  rows: SkillRow[]
  enabled: number
}

function groupNameOf(slug: string): string {
  const hyphen = slug.indexOf('-')
  return hyphen > 0 ? slug.slice(0, hyphen) : ''
}

const skillGroups = computed<SkillGroup[]>(() => {
  const counts = new Map<string, number>()
  for (const row of skillRows.value) {
    const name = groupNameOf(row.slug)
    if (name) counts.set(name, (counts.get(name) ?? 0) + 1)
  }

  const groups = new Map<string, SkillGroup>()
  for (const row of skillRows.value) {
    const name = groupNameOf(row.slug)
    if ((counts.get(name) ?? 0) < 2) continue
    const group = groups.get(name) ?? { name, rows: [], enabled: 0 }
    group.rows.push(row)
    if (skillEnabled(row.slug)) group.enabled += 1
    groups.set(name, group)
  }
  return [...groups.values()].sort((a, b) => a.name.localeCompare(b.name))
})

/**
 * The section as one flat list: each group's header, its rows while expanded,
 * then the rows no group claimed. Flat because a collapsed group contributes
 * nothing — filtering rows out of one list is the whole of collapsing — and
 * because it keeps a single row template rather than one per nesting level.
 */
type SkillListItem =
  | { kind: 'group', key: string, group: SkillGroup }
  | { kind: 'row', key: string, row: SkillRow, indented: boolean }

const skillListItems = computed<SkillListItem[]>(() => {
  const items: SkillListItem[] = []
  for (const group of skillGroups.value) {
    items.push({ kind: 'group', key: `group-${group.name}`, group })
    if (!skillGroupExpanded(group.name)) continue
    for (const row of group.rows) items.push({ kind: 'row', key: row.slug, row, indented: true })
  }
  const grouped = new Set(skillGroups.value.flatMap((g) => g.rows.map((r) => r.slug)))
  for (const row of skillRows.value) {
    if (!grouped.has(row.slug)) items.push({ kind: 'row', key: row.slug, row, indented: false })
  }
  return items
})

/** Groups the user has expanded or collapsed by hand, overriding the default. */
const skillGroupOverrides = ref<Record<string, boolean>>({})

// A group is collapsed by default — its header's count is the whole point, so
// the list stays short — except when it holds a slug the catalogue no longer
// resolves. That row carries a warning, and a warning folded out of sight is
// not a warning.
function skillGroupExpanded(name: string): boolean {
  const override = skillGroupOverrides.value[name]
  if (override !== undefined) return override
  return skillGroups.value.some((group) => group.name === name && group.rows.some((row) => row.missing))
}

function toggleSkillGroupExpanded(name: string): void {
  skillGroupOverrides.value = { ...skillGroupOverrides.value, [name]: !skillGroupExpanded(name) }
}

// A partly-enabled group switches fully on, so one click is always "give this
// workspace the whole set" rather than an ambiguous inversion.
function toggleSkillGroup(group: SkillGroup): void {
  const slugs = group.rows.map((row) => row.slug)
  selectedSkills.value = group.enabled === group.rows.length
    ? selectedSkills.value.filter((slug) => !slugs.includes(slug))
    : [...selectedSkills.value, ...slugs.filter((slug) => !skillEnabled(slug))]
}

const skillError = ref('')

async function openSkillsLibrary(): Promise<void> {
  skillError.value = ''
  try {
    await revealSkillsLibrary()
  } catch (failure) {
    skillError.value = failure instanceof Error ? failure.message : 'The skills library could not be opened.'
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
    agent: agent.value,
    autonomy: autonomy.value,
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
  void reloadSkillCatalogue()
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

      <div class="flex flex-col gap-1.5">
        <span class="text-xs text-text-3">Agent</span>
        <AppSelect
          v-model="agent"
          :options="agentOptions"
          placeholder="No agents configured"
          aria-label="Agent"
          testid="agent-workspace-editor-agent"
          :disabled="busy || !agents.length"
        />
      </div>

      <div class="flex flex-col gap-1.5">
        <span class="text-xs text-text-3">Autonomy</span>
        <div
          role="radiogroup"
          aria-label="Autonomy"
          class="flex flex-col divide-y divide-row rounded-lg border border-strong bg-raised"
          data-testid="agent-workspace-editor-autonomy"
        >
          <button
            v-for="option in autonomyOptions"
            :key="option.value"
            type="button"
            role="radio"
            :aria-checked="autonomy === option.value"
            :disabled="busy || option.unavailable"
            class="flex items-start gap-2.5 px-3 py-2.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-50 first:rounded-t-lg last:rounded-b-lg"
            :class="[
              busy || option.unavailable ? '' : 'cursor-pointer',
              autonomy === option.value && option.danger ? 'bg-severity-warning-tint' : '',
            ]"
            :data-testid="`agent-workspace-editor-autonomy-${option.value}`"
            @click="selectAutonomy(option)"
          >
            <span
              class="mt-0.5 flex size-3.5 shrink-0 items-center justify-center rounded-full border"
              :class="autonomy === option.value ? 'border-accent' : 'border-strong'"
            >
              <span v-if="autonomy === option.value" class="size-1.5 rounded-full bg-accent" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="flex items-center gap-1.5">
                <IconTriangleAlert v-if="option.danger" class="size-3.5 shrink-0 text-severity-warning" aria-hidden="true" />
                <span class="text-[13px]" :class="option.danger ? 'text-severity-warning' : 'text-text'">{{ option.label }}</span>
              </span>
              <span class="mt-0.5 block text-[11.5px] leading-relaxed text-text-3">{{ option.description }}</span>
              <span
                v-if="option.flags"
                class="mt-0.5 block truncate font-mono text-[11px]"
                :class="option.danger ? 'text-severity-warning' : 'text-text-4'"
                :title="option.flags"
              >{{ agent }} {{ option.flags }}</span>
              <span v-if="option.unavailable" class="mt-0.5 block text-[11px] text-text-4">not available for this agent</span>
            </span>
          </button>
        </div>
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
        <span class="text-xs text-text-3">Skills</span>
        <div v-if="skillRows.length" class="flex flex-col divide-y divide-row rounded-lg border border-strong bg-raised">
          <template v-for="item in skillListItems" :key="item.key">
            <div v-if="item.kind === 'group'" class="flex items-center gap-2.5 px-3 py-2.5">
              <AppSwitch
                size="sm"
                :model-value="item.group.enabled === item.group.rows.length"
                :aria-label="`Enable every ${item.group.name} skill`"
                :disabled="busy"
                :testid="`agent-workspace-editor-skill-group-${item.group.name}`"
                @update:model-value="toggleSkillGroup(item.group)"
              />
              <button
                type="button"
                class="flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 text-left"
                :aria-expanded="skillGroupExpanded(item.group.name)"
                :data-testid="`agent-workspace-editor-skill-group-toggle-${item.group.name}`"
                @click="toggleSkillGroupExpanded(item.group.name)"
              >
                <span class="truncate font-mono text-[13px] text-text">{{ item.group.name }}</span>
                <span class="shrink-0 text-[11px] text-text-4">{{ item.group.enabled }} of {{ item.group.rows.length }} on</span>
                <IconChevronDown class="size-3 shrink-0 text-text-4 transition-transform" :class="{ '-rotate-90': !skillGroupExpanded(item.group.name) }" />
              </button>
            </div>

            <div v-else class="flex items-start gap-2.5 py-2.5 pr-3" :class="item.indented ? 'pl-9' : 'pl-3'">
              <AppSwitch
                size="sm"
                class="mt-0.5"
                :model-value="skillEnabled(item.row.slug)"
                :aria-label="`Enable ${item.row.title}`"
                :disabled="busy"
                :testid="`agent-workspace-editor-skill-${item.row.slug}`"
                @update:model-value="toggleSkill(item.row.slug)"
              />
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1.5">
                  <span class="truncate text-[13px] text-text">{{ item.row.title }}</span>
                  <span class="shrink-0 rounded-full border border-card px-1.5 py-px text-[10px] text-text-4">{{ item.row.shipped ? 'shipped' : 'custom' }}</span>
                  <span v-if="item.row.shadows" class="shrink-0 text-[10px] text-severity-warning">replaces shipped</span>
                </div>
                <div v-if="item.row.description" class="text-[11.5px] leading-relaxed text-text-3">{{ item.row.description }}</div>
                <div v-if="item.row.missing" class="text-[11px] text-severity-warning">not in the catalogue — enabled slugs without an entry are skipped when the workspace opens</div>
              </div>
            </div>
          </template>
        </div>
        <span v-else class="text-xs text-text-4">No skills are available yet.</span>
        <button
          type="button"
          class="self-start cursor-pointer text-[12px] text-accent hover:underline"
          :disabled="busy"
          data-testid="agent-workspace-editor-skills-library"
          @click="openSkillsLibrary"
        >Open the skills library…</button>
        <span class="text-xs text-text-4">A skill in the library is a SKILL.md under .shared/skills — shared by every workspace, carried only by the ones that switch it on.</span>
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
        :description="`Every live chat in ${workspace!.name || workspace!.dir} closes and its chat history is removed. The directory itself stays on disk.`"
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
