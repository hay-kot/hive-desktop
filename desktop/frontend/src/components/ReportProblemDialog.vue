<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import IconBug from '~icons/lucide/bug'
import IconCheck from '~icons/lucide/check'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import AppSwitch from './AppSwitch.vue'
import TextField from '../pipeline/fields/TextField.vue'
import TextareaField from '../pipeline/fields/TextareaField.vue'
import { useReportProblem } from '../composables/useReportProblem'

const emit = defineEmits<{ close: [] }>()

const { preview, loading, submitting, error, reportId, loadPreview, submit } = useReportProblem()

const description = ref('')
const contact = ref('')
const includeBasics = ref(true)
const includeConfig = ref(true)
const includeSettings = ref(true)
const includeFlows = ref(true)
const includeActions = ref(true)

const logKb = computed(() => Math.max(1, Math.ceil((preview.value?.logBytes ?? 0) / 1024)))
const hasConfig = computed(() =>
  !!preview.value && (preview.value.hasSettings || preview.value.flowCount > 0 || preview.value.hasActions),
)

onMounted(() => {
  void loadPreview()
})

async function onSubmit(): Promise<void> {
  if (submitting.value) return
  await submit({
    description: description.value,
    contact: contact.value,
    includeBasics: includeBasics.value,
    includeSettings: includeConfig.value && includeSettings.value,
    includeFlows: includeConfig.value && includeFlows.value,
    includeActions: includeConfig.value && includeActions.value,
  })
}
</script>

<template>
  <BaseModal
    title="Report a problem"
    :icon="IconBug"
    :width="520"
    :busy="submitting"
    testid="report-dialog"
    @close="emit('close')"
  >
    <div v-if="reportId" class="flex flex-col gap-3 px-5 py-5" data-testid="report-success">
      <div class="flex items-center gap-2 text-severity-success">
        <IconCheck class="size-4" />
        <span class="text-[14px] font-semibold">Report sent</span>
      </div>
      <p class="text-[13px] text-text-2">Thanks — we received your report. Keep this reference id if you follow up.</p>
      <code class="select-all rounded-lg border border-strong bg-app px-3 py-2.5 font-mono text-[13px] text-text" data-testid="report-id">{{ reportId }}</code>
    </div>

    <form v-else class="flex flex-col gap-4 px-5 py-4" @submit.prevent="onSubmit">
      <TextareaField
        v-model="description"
        label="What happened?"
        placeholder="Describe the problem, what you expected, and any steps to reproduce it."
        hint="Optional, but the more detail the better."
        :rows="4"
        testid="report-description"
      />
      <TextField
        v-model="contact"
        label="Contact"
        placeholder="email or handle (optional)"
        hint="Only if you want us to be able to follow up."
        testid="report-contact"
      />

      <div v-if="preview" class="overflow-hidden rounded-lg border border-card bg-raised">
        <div class="px-3.5 py-3">
          <div class="flex items-center justify-between gap-3">
            <div class="text-[13px] font-semibold text-text">Include basic info</div>
            <AppSwitch
              :model-value="includeBasics"
              aria-label="Include basic info"
              testid="report-include-basics"
              @update:model-value="includeBasics = $event"
            />
          </div>
          <ul class="mt-2 flex flex-col gap-1 text-[12px] text-text-2 transition-opacity" :class="{ 'opacity-40': !includeBasics }">
            <li>• Build and system info</li>
            <li v-if="preview.hasLogs">• Recent logs ({{ logKb }} KB)</li>
            <li v-if="preview.accountCount > 0">• Connected accounts ({{ preview.accountCount }})</li>
          </ul>
        </div>

        <div v-if="hasConfig" class="border-t border-row px-3.5 py-3">
          <div class="flex items-center justify-between gap-3">
            <div class="min-w-0">
              <div class="text-[13px] font-semibold text-text">Include user configuration</div>
              <div class="mt-0.5 text-[11.5px] text-text-3">Redacted for common tokens and keys before sending.</div>
            </div>
            <AppSwitch
              :model-value="includeConfig"
              aria-label="Include user configuration"
              testid="report-include-config"
              @update:model-value="includeConfig = $event"
            />
          </div>
          <div v-if="includeConfig" class="mt-3 flex flex-col gap-2 border-t border-row pt-3">
            <div v-if="preview.hasSettings" class="flex items-center justify-between gap-3">
              <span class="text-[13px]" :class="includeSettings ? 'text-text' : 'text-text-2'">Settings</span>
              <AppSwitch
                size="sm"
                :model-value="includeSettings"
                aria-label="Include settings"
                testid="report-include-settings"
                @update:model-value="includeSettings = $event"
              />
            </div>
            <div v-if="preview.flowCount > 0" class="flex items-center justify-between gap-3">
              <span class="text-[13px]" :class="includeFlows ? 'text-text' : 'text-text-2'">Flows ({{ preview.flowCount }})</span>
              <AppSwitch
                size="sm"
                :model-value="includeFlows"
                aria-label="Include flows"
                testid="report-include-flows"
                @update:model-value="includeFlows = $event"
              />
            </div>
            <div v-if="preview.hasActions" class="flex items-center justify-between gap-3">
              <span class="text-[13px]" :class="includeActions ? 'text-text' : 'text-text-2'">Actions</span>
              <AppSwitch
                size="sm"
                :model-value="includeActions"
                aria-label="Include actions"
                testid="report-include-actions"
                @update:model-value="includeActions = $event"
              />
            </div>
          </div>
        </div>
      </div>
      <div v-else-if="loading" class="rounded-lg border border-card bg-raised px-3.5 py-3 text-[12px] text-text-3">Preparing…</div>

      <p v-if="preview && !preview.available" class="text-[12px] text-severity-warning" data-testid="report-unavailable">
        Problem reporting isn't enabled in this build.
      </p>
      <p v-if="error" class="text-[12px] text-severity-error" data-testid="report-error">{{ error }}</p>
    </form>

    <template #footer>
      <template v-if="reportId">
        <BaseButton class="flex-1" data-testid="report-done" @click="emit('close')">Done</BaseButton>
      </template>
      <template v-else>
        <BaseButton
          class="flex-1"
          :busy="submitting"
          :disabled="loading || !(preview?.available)"
          data-testid="report-submit"
          @click="onSubmit"
        >{{ submitting ? 'Sending…' : 'Send report' }}</BaseButton>
        <BaseButton variant="secondary" :busy="submitting" @click="emit('close')">Cancel</BaseButton>
      </template>
    </template>
  </BaseModal>
</template>
