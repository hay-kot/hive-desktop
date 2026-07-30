<script setup lang="ts">
import { ref } from 'vue'
import IconPlay from '~icons/lucide/play'
import ActionInputFields from './ActionInputFields.vue'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import type { InputSpec } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/actions/models'
import { type ActionInputValues, initialActionInputs, validateActionInputs } from '../lib/actionInputs'

const props = defineProps<{ actionLabel: string; inputs: InputSpec[]; busy: boolean; error: string | null }>()
const emit = defineEmits<{ close: []; submit: [values: ActionInputValues] }>()

const values = ref<ActionInputValues>(initialActionInputs(props.inputs))
const validationError = ref('')

function submit(): void {
  if (props.busy) return
  const problem = validateActionInputs(props.inputs, values.value)
  if (problem) {
    validationError.value = problem
    return
  }
  validationError.value = ''
  emit('submit', { ...values.value })
}
</script>

<template>
  <BaseModal
    :title="actionLabel"
    :icon="IconPlay"
    :width="460"
    :busy="busy"
    testid="action-inputs-dialog"
    @close="emit('close')"
  >
    <form class="grid gap-3 px-5 py-4" @submit.prevent="submit">
      <ActionInputFields v-model="values" :inputs="inputs" />
      <p v-if="validationError || error" class="text-xs text-severity-error" data-testid="action-inputs-error">{{ validationError || error }}</p>
    </form>
    <template #footer>
      <BaseButton class="flex-1" :busy="busy" data-testid="action-inputs-submit" @click="submit">{{ busy ? 'Running…' : 'Run' }}</BaseButton>
      <BaseButton variant="secondary" :busy="busy" @click="emit('close')">Cancel</BaseButton>
    </template>
  </BaseModal>
</template>
