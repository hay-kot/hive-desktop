<script setup lang="ts">
import { onMounted, ref } from 'vue'
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
const includeLogs = ref(true)

onMounted(() => {
  void loadPreview(true)
})

async function onSubmit(): Promise<void> {
  if (submitting.value) return
  await submit({ description: description.value, contact: contact.value, includeLogs: includeLogs.value })
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

      <div class="rounded-lg border border-card bg-raised px-3.5 py-3">
        <div class="flex items-start gap-3">
          <AppSwitch
            :model-value="includeLogs"
            aria-label="Attach recent logs"
            testid="report-include-logs"
            @update:model-value="includeLogs = $event"
          />
          <div class="min-w-0 flex-1">
            <div class="text-[13px] font-semibold text-text">Attach recent logs</div>
            <div class="mt-0.5 text-[11.5px] text-text-3">Secrets and tokens are removed before anything is sent.</div>
          </div>
        </div>

        <div v-if="preview" class="mt-3 border-t border-row pt-3">
          <div class="text-[11px] font-semibold uppercase tracking-[.08em] text-text-3">Attached</div>
          <ul class="mt-1.5 flex flex-col gap-1 text-[12px] text-text-2">
            <li v-for="item in preview.attachments ?? []" :key="item">• {{ item }}</li>
          </ul>
          <pre
            v-if="includeLogs && preview.logContent"
            class="mt-2.5 max-h-40 overflow-auto rounded-md border border-strong bg-app p-2.5 font-mono text-[11px] leading-relaxed text-text-3"
            data-testid="report-log-preview"
          >{{ preview.logContent }}</pre>
        </div>
        <div v-else-if="loading" class="mt-3 text-[12px] text-text-3">Preparing preview…</div>
      </div>

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
