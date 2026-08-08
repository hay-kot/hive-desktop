<script setup lang="ts">
// The app-wide answer to "an operation failed and the user needs the details".
// A caller raises one through useErrorDialog(); this renders it with the two
// affordances that make a failure actionable — the full text on the clipboard,
// and a diagnostic report filed in one click (ADR in-app-problem-reporting).
//
// The backdrop does not dismiss it: a failure the app decided to interrupt for
// should not close on a stray click before it has been read.
import { computed, onMounted } from 'vue'
import IconCheck from '~icons/lucide/check'
import IconCircleAlert from '~icons/lucide/circle-alert'
import IconCopy from '~icons/lucide/copy'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import { useClipboard } from '../composables/useClipboard'
import { useReportProblem } from '../composables/useReportProblem'
import { errorDetailsText, type ErrorDetails } from '../composables/useErrorDialog'

const props = defineProps<{ error: ErrorDetails }>()
const emit = defineEmits<{ close: [] }>()

const { copy, status: copyStatus } = useClipboard()
const { preview, submitting, error: reportError, reportId, loadPreview, submit } = useReportProblem()

const text = computed(() => errorDetailsText(props.error))
const detail = computed(() => props.error.detail.trim() || 'No further detail was reported.')
const contextEntries = computed(() => Object.entries(props.error.context ?? {}))
const reportUnavailable = computed(() => preview.value !== null && !preview.value.available)

const copyLabel = computed(() => {
  if (copyStatus.value === 'success') return 'Copied'
  if (copyStatus.value === 'error') return 'Copy failed'
  return 'Copy error details'
})

onMounted(() => {
  void loadPreview()
})

// One click: the error text becomes the description and everything the manual
// reporter offers is attached, because nobody triaging a failure they did not
// choose to describe is served by a narrower bundle.
async function sendReport(): Promise<void> {
  if (submitting.value || reportId.value) return
  await submit({
    description: text.value,
    contact: '',
    includeBasics: true,
    includeSettings: true,
    includeFlows: true,
    includeActions: true,
  })
}
</script>

<template>
  <BaseModal
    :title="error.title"
    :icon="IconCircleAlert"
    tone="danger"
    aria-role="alertdialog"
    :width="560"
    :busy="submitting"
    :close-on-backdrop="false"
    testid="error-dialog"
    @close="emit('close')"
  >
    <div class="flex flex-col gap-3 px-5 py-4">
      <p v-if="error.summary" class="text-[13px] leading-relaxed text-text-2" data-testid="error-dialog-summary">{{ error.summary }}</p>

      <dl v-if="contextEntries.length" class="flex flex-wrap gap-x-4 gap-y-1 text-[12px]" data-testid="error-dialog-context">
        <div v-for="[key, value] in contextEntries" :key="key" class="flex gap-1.5">
          <dt class="text-text-3">{{ key }}</dt>
          <dd class="font-mono text-text-2">{{ value }}</dd>
        </div>
      </dl>

      <!-- Error-toned, not a neutral code block: this is the failure itself,
           and Copy belongs beside it rather than in the footer among the
           dialog's own actions. -->
      <div class="flex items-start gap-2.5 rounded-lg border border-severity-error-border bg-severity-error-tint px-3 py-2.5" data-testid="error-dialog-message">
        <IconCircleAlert class="mt-px size-4 shrink-0 text-severity-error" />
        <pre
          class="hive-scroll max-h-[240px] min-w-0 flex-1 select-text overflow-auto whitespace-pre-wrap break-words font-mono text-[12.5px] leading-relaxed text-severity-error"
          data-testid="error-dialog-detail"
        >{{ detail }}</pre>
        <button
          type="button"
          class="-mr-1 -mt-0.5 flex size-[26px] shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-severity-error/70 hover:bg-severity-error/15 hover:text-severity-error"
          :title="copyLabel"
          :aria-label="copyLabel"
          data-testid="error-dialog-copy"
          @click="copy(text)"
        ><component :is="copyStatus === 'success' ? IconCheck : IconCopy" class="size-[15px]" /></button>
      </div>

      <div v-if="reportId" class="flex flex-col gap-1.5" data-testid="error-dialog-report-sent">
        <div class="flex items-center gap-2 text-severity-success">
          <IconCheck class="size-4" />
          <span class="text-[13px] font-semibold">Report sent</span>
        </div>
        <code class="select-all rounded-lg border border-strong bg-app px-3 py-2.5 font-mono text-[13px] text-text" data-testid="error-dialog-report-id">{{ reportId }}</code>
      </div>
      <p v-else-if="reportUnavailable" class="text-[11.5px] text-severity-warning" data-testid="error-dialog-report-unavailable">
        Problem reporting isn't enabled in this build.
      </p>
      <p v-else class="text-[11.5px] text-text-3">
        Sending a report attaches this error, recent logs, and a redacted copy of your configuration.
      </p>

      <p v-if="reportError" class="text-[12px] text-severity-error" data-testid="error-dialog-report-error">{{ reportError }}</p>
    </div>

    <template #footer>
      <BaseButton class="flex-1" :disabled="submitting" data-testid="error-dialog-close" @click="emit('close')">Close</BaseButton>
      <BaseButton
        v-if="!reportId"
        variant="secondary"
        :busy="submitting"
        :disabled="reportUnavailable"
        data-testid="error-dialog-report"
        @click="sendReport"
      >{{ submitting ? 'Sending…' : 'Send report' }}</BaseButton>
    </template>
  </BaseModal>
</template>
