<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import IconPlay from '~icons/lucide/play'
import AppSelect from './AppSelect.vue'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import RepositorySelect from './RepositorySelect.vue'
import type { SessionLaunchOptions } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'
import { useAutofocus } from '../composables/useAutofocus'
import { formatCombo } from '../composables/useKeybindings'
import { useSubmitShortcut } from '../composables/useSubmitShortcut'

const props = defineProps<{
  options: SessionLaunchOptions
  initial: { repository: string; name: string; prompt: string }
  busy: boolean
  error: string | null
}>()
const emit = defineEmits<{
  close: []
  submit: [input: { repository: string; name: string; prompt: string; agent?: string }]
}>()

// The footer sits outside the form, so the submit button claims it by id —
// which is also what makes Enter in a single-line field submit.
const formId = useId()
const submitHint = formatCombo('mod+enter')
const repository = ref(props.initial.repository || props.options.defaultRepository)
const name = ref(props.initial.name)
const prompt = ref(props.initial.prompt)
const agent = ref(props.options.defaultAgent)
const validationError = ref('')
const nameInput = ref<HTMLInputElement | null>(null)
const canSubmit = computed(() => repository.value.trim() !== '' && name.value.trim() !== '')
const agentOptions = computed(() => [{ value: '', label: 'Default agent' }, ...(props.options.agents ?? []).map((key) => ({ value: key, label: key }))])

function submit() {
  if (props.busy) return
  const repo = repository.value.trim()
  const sessionName = name.value.trim()
  if (!repo) {
    validationError.value = 'Repository is required.'
    return
  }
  if (!sessionName) {
    validationError.value = 'Session name is required.'
    return
  }
  if (!/^[a-zA-Z0-9][a-zA-Z0-9 _.:/\-]*$/.test(sessionName)) {
    validationError.value = 'Use letters, numbers, spaces, and - _ : . /.'
    return
  }
  validationError.value = ''
  emit('submit', { repository: repo, name: sessionName, prompt: prompt.value.trim(), ...(agent.value ? { agent: agent.value } : {}) })
}

useAutofocus(nameInput)
useSubmitShortcut(submit)
</script>

<template>
  <BaseModal
    title="New session"
    :icon="IconPlay"
    :width="520"
    :busy="busy"
    testid="new-session-dialog"
    @close="emit('close')"
  >
    <form :id="formId" class="flex flex-col gap-3 px-5 py-4" @submit.prevent="submit">
      <div class="flex flex-col gap-1.5 text-xs font-medium text-text-2">Repository
        <RepositorySelect
          :model-value="repository"
          :repositories="options.repositories"
          testid="new-session-repository"
          @update:model-value="repository = $event"
        />
      </div>
      <label class="flex flex-col gap-1.5 text-xs font-medium text-text-2">Session name
        <!-- A session name slugs into a tmux name and a directory path, so the
             webview's text substitutions must not touch what was typed. -->
        <input ref="nameInput" v-model="name" autocapitalize="off" autocorrect="off" spellcheck="false" class="rounded-lg border border-strong bg-app px-3 py-2.5 text-[13px] text-text outline-none focus:border-accent" placeholder="review-pr-123" data-testid="new-session-name">
      </label>
      <label class="flex flex-col gap-1.5 text-xs font-medium text-text-2">Prompt <span class="font-normal text-text-4">(optional)</span>
        <textarea v-model="prompt" rows="6" class="resize-y rounded-lg border border-strong bg-app px-3 py-2.5 text-[13px] leading-relaxed text-text outline-none focus:border-accent" placeholder="Describe the task for the agent…" data-testid="new-session-prompt" />
      </label>
      <div class="flex flex-col gap-1.5 text-xs font-medium text-text-2">Agent <span class="font-normal text-text-4">(optional)</span>
        <AppSelect
          :model-value="agent"
          :options="agentOptions"
          testid="new-session-agent"
          aria-label="Agent"
          @update:model-value="agent = $event"
        />
      </div>
      <p v-if="validationError || error" class="text-xs text-severity-error" data-testid="new-session-error">{{ validationError || error }}</p>
    </form>
    <template #footer>
      <BaseButton class="flex-1" type="submit" :form="formId" :busy="busy" :disabled="!canSubmit" data-testid="new-session-submit">
        {{ busy ? 'Creating…' : 'Create session' }}
        <kbd v-if="!busy" class="rounded bg-black/15 px-1 py-0.5 font-mono text-[10.5px] leading-none">{{ submitHint }}</kbd>
      </BaseButton>
      <BaseButton variant="secondary" :busy="busy" @click="emit('close')">Cancel</BaseButton>
    </template>
  </BaseModal>
</template>
