<script setup lang="ts">
import { computed, ref } from 'vue'
import IconPlay from '~icons/lucide/play'
import AppSelect from './AppSelect.vue'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import type { SessionLaunchOptions } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'
import { useAutofocus } from '../composables/useAutofocus'

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
    <form class="flex flex-col gap-3 px-5 py-4" @submit.prevent="submit">
      <label class="flex flex-col gap-1.5 text-xs font-medium text-text-2">Repository
        <input v-model="repository" list="new-session-repositories" class="rounded-lg border border-strong bg-app px-3 py-2.5 text-[13px] text-text outline-none focus:border-accent" placeholder="https://github.com/owner/repository.git" data-testid="new-session-repository">
        <datalist id="new-session-repositories"><option v-for="repo in options.repositories" :key="repo.repository" :value="repo.repository">{{ repo.name }}</option></datalist>
      </label>
      <label class="flex flex-col gap-1.5 text-xs font-medium text-text-2">Session name
        <input ref="nameInput" v-model="name" class="rounded-lg border border-strong bg-app px-3 py-2.5 text-[13px] text-text outline-none focus:border-accent" placeholder="review-pr-123" data-testid="new-session-name">
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
      <BaseButton class="flex-1" :busy="busy" :disabled="!canSubmit" data-testid="new-session-submit" @click="submit">{{ busy ? 'Creating…' : 'Create session' }}</BaseButton>
      <BaseButton variant="secondary" :busy="busy" @click="emit('close')">Cancel</BaseButton>
    </template>
  </BaseModal>
</template>
