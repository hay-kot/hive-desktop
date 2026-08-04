<script setup lang="ts">
// Create/edit editor for an agent workspace's manifest, in the app's
// DrawerSheet editor shell (the ActionEditor pattern). It writes only the
// fields it shows — name, agent, autonomy (plus the directory name at
// creation); mcps, skills, and hand-written comments in the YAML survive the
// write untouched, which is why the hint points at the file for the rest.
// Deleting the workspace also lives here — the editor is the workspace's
// whole management surface, so its sidebar row needs no menu. Delete follows
// FolderEditModal.vue's shape: a quiet footer action that expands into an
// InlineConfirm over a dimmed, inert form instead of stacking a dialog.
import { computed, nextTick, onMounted, ref } from 'vue'
import IconFolderCog from '~icons/lucide/folder-cog'
import IconX from '~icons/lucide/x'
import AppSelect, { type AppSelectOption } from './AppSelect.vue'
import BaseButton from './BaseButton.vue'
import DrawerSheet from './DrawerSheet.vue'
import InlineConfirm from './InlineConfirm.vue'
import type { AgentWorkspace, WorkspaceEditRequest } from '../lib/agentWorkspacesClient'

const props = defineProps<{
  /** The workspace being edited, or null to create one. */
  workspace?: AgentWorkspace | null
  agents: string[]
  busy?: boolean
  error?: string | null
}>()
const emit = defineEmits<{ close: []; save: [request: WorkspaceEditRequest]; delete: [dir: string] }>()

const creating = computed(() => !props.workspace)

const dir = ref(props.workspace?.dir ?? '')
const name = ref(props.workspace?.name ?? '')
const agent = ref(props.workspace?.agent || props.agents[0] || '')
const autonomy = ref(props.workspace?.autonomy || 'ask')

const agentOptions = computed<AppSelectOption[]>(() => props.agents.map((a) => ({ value: a, label: a })))
const autonomyOptions: AppSelectOption[] = [
  { value: 'ask', label: 'ask — approve every action' },
  { value: 'auto', label: 'auto — edits allowed, asks for the rest' },
  { value: 'full', label: 'full — no prompts' },
]

const valid = computed(() => !!name.value.trim() && !!agent.value && (!creating.value || !!dir.value.trim()))

const confirming = ref(false)

function submit(): void {
  if (props.busy || confirming.value || !valid.value) return
  emit('save', {
    dir: creating.value ? dir.value.trim() : props.workspace!.dir,
    name: name.value.trim(),
    agent: agent.value,
    autonomy: autonomy.value,
  })
}

function cancel(): void {
  if (!props.busy && !confirming.value) emit('close')
}

const nameInput = ref<HTMLInputElement | null>(null)
const dirInput = ref<HTMLInputElement | null>(null)
onMounted(async () => {
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
        <span class="text-xs text-text-4">A new directory under the workspace root.</span>
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
        <AppSelect
          v-model="autonomy"
          :options="autonomyOptions"
          aria-label="Autonomy"
          testid="agent-workspace-editor-autonomy"
          :disabled="busy"
        />
      </div>

      <p v-if="!creating" class="text-xs leading-relaxed text-text-4">
        MCP servers, skills, and anything else in agent-workspace.yaml stay as written — edit the file for those.
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
