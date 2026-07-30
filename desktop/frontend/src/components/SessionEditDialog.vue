<script setup lang="ts">
// One dialog for the session's single-field edits — its name and its group.
// They differ only in their copy and in whether empty is a valid value, so the
// host passes both rather than there being two near-identical dialogs.
import { nextTick, onMounted, ref } from 'vue'
import IconPencil from '~icons/lucide/pencil'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import type { SessionEdit } from '../composables/useSessionActions'

const props = defineProps<{
  edit: SessionEdit
  busy?: boolean
  error?: string | null
}>()
const emit = defineEmits<{ close: []; save: [value: string] }>()

const value = ref(props.edit.value)
const inputRef = ref<HTMLInputElement | null>(null)

function submit(): void {
  if (props.busy) return
  const trimmed = value.value.trim()
  if (!trimmed && !props.edit.allowEmpty) return
  emit('save', trimmed)
}

onMounted(async () => {
  await nextTick()
  inputRef.value?.focus()
  inputRef.value?.select()
})
</script>

<template>
  <BaseModal
    :title="edit.title"
    :icon="IconPencil"
    :width="440"
    :busy="busy"
    :testid="`${edit.testid}-dialog`"
    @close="emit('close')"
  >
    <div class="flex flex-col gap-1.5 px-5 py-4">
      <label :for="`${edit.testid}-input`" class="text-xs text-text-3">{{ edit.label }}</label>
      <input
        :id="`${edit.testid}-input`"
        ref="inputRef"
        v-model="value"
        type="text"
        :disabled="busy"
        class="w-full rounded-lg border border-strong bg-raised px-3 py-2.5 text-[13.5px] text-text outline-none focus:border-accent"
        :data-testid="`${edit.testid}-input`"
        @keydown.enter="submit"
      >
      <span class="text-xs text-text-4">{{ edit.hint }}</span>
      <p
        v-if="error"
        class="mt-1 rounded border border-severity-error bg-severity-error-tint px-3 py-2 text-xs text-severity-error"
        :data-testid="`${edit.testid}-error`"
      >{{ error }}</p>
    </div>
    <template #footer>
      <div class="flex-1" />
      <BaseButton variant="secondary" :busy="busy" :data-testid="`${edit.testid}-cancel`" @click="emit('close')">Cancel</BaseButton>
      <BaseButton
        :busy="busy"
        :disabled="!value.trim() && !edit.allowEmpty"
        :data-testid="`${edit.testid}-save`"
        @click="submit"
      >{{ edit.confirmLabel }} ↵</BaseButton>
    </template>
  </BaseModal>
</template>
