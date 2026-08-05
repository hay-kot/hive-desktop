<script setup lang="ts">
// Create/edit editor for an agent workspace's manifest, in the app's
// DrawerSheet editor shell (the ActionEditor pattern). It writes the fields
// it shows — name, agent, autonomy, and the mcps list (plus the directory
// name at creation); skills and hand-written comments in the YAML survive
// the write untouched. The MCP rows come from the merged catalogue (shipped
// entries plus mcps.yaml), and pasting MCP JSON lands new servers in
// mcps.yaml before enabling them here — the library is shared, the toggle is
// this workspace's own. Deleting the workspace also lives here — the editor
// is the workspace's whole management surface, so its sidebar row needs no
// menu. Delete follows FolderEditModal.vue's shape: a quiet footer action
// that expands into an InlineConfirm over a dimmed, inert form.
import { computed, nextTick, onMounted, ref } from 'vue'
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
  editor, mcpCatalogue, autonomyFlags,
  reloadMCPCatalogue, importMCPServers, removeMCPServer,
  openWorkspaceInEditor, revealWorkspace,
} = useAgentWorkspaces()

const creating = computed(() => !props.workspace)

const dir = ref(props.workspace?.dir ?? '')
const name = ref(props.workspace?.name ?? '')
const agent = ref(props.workspace?.agent || props.agents[0] || '')
const autonomy = ref(props.workspace?.autonomy || 'ask')
const selectedMCPs = ref<string[]>([...(props.workspace?.mcps ?? [])])

const agentOptions = computed<AppSelectOption[]>(() => props.agents.map((a) => ({ value: a, label: a })))

// Every posture is laid out as a radio card rather than a dropdown, so the
// choice being made — especially full's dangerous bypass — is readable
// before it is selected. The flags line is the launch table's own projection
// for the chosen agent (ADR 0061): the UI shows what the posture actually
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
  })
}

function cancel(): void {
  if (!props.busy && !confirming.value) emit('close')
}

const nameInput = ref<HTMLInputElement | null>(null)
const dirInput = ref<HTMLInputElement | null>(null)
onMounted(async () => {
  void reloadMCPCatalogue()
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

      <p v-if="!creating" class="text-xs leading-relaxed text-text-4">
        Saving rewrites these fields in agent-workspace.yaml and re-syncs the workspace's
        generated files. Skills, comments, and anything else in the file stay as written —
        edit the file for those.
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
