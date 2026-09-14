<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import IconBug from '~icons/lucide/bug'
import IconCheck from '~icons/lucide/check'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import AppSwitch from './AppSwitch.vue'
import { useReportProblem } from '../composables/useReportProblem'

const emit = defineEmits<{ close: [] }>()

const { preview, loading, saving, error, saved, loadPreview, save, reveal, openIssue } = useReportProblem()

// Every surface defaults off. The bundle is attached to a public issue, and
// redaction removes credentials, not identity: a log tail names the user's
// home directory, repositories and branches.
const includeLogs = ref(false)
const includeSettings = ref(false)
const includeFlows = ref(false)
const includeActions = ref(false)

const logKb = computed(() => Math.max(1, Math.ceil((preview.value?.logBytes ?? 0) / 1024)))

onMounted(() => {
  void loadPreview()
})

async function onSave(): Promise<void> {
  if (saving.value) return
  await save({
    includeLogs: includeLogs.value,
    includeSettings: includeSettings.value,
    includeFlows: includeFlows.value,
    includeActions: includeActions.value,
  })
}
</script>

<template>
  <BaseModal
    title="Report a problem"
    :icon="IconBug"
    :width="520"
    :busy="saving"
    testid="report-dialog"
    @close="emit('close')"
  >
    <div v-if="saved" class="flex flex-col gap-3 px-5 py-5" data-testid="report-success">
      <div class="flex items-center gap-2 text-severity-success">
        <IconCheck class="size-4" />
        <span class="text-[14px] font-semibold">Bundle saved</span>
      </div>
      <p class="text-[13px] text-text-2">
        The issue form is open in your browser. Review this file, then drag it into the
        <span class="font-semibold">Diagnostic bundle or logs</span> box.
      </p>
      <code class="select-all break-all rounded-lg border border-strong bg-app px-3 py-2.5 font-mono text-[12px] text-text" data-testid="report-path">{{ saved.path }}</code>
      <p v-if="error" class="text-[12px] text-severity-error" data-testid="report-error">{{ error }}</p>
    </div>

    <form v-else class="flex flex-col gap-4 px-5 py-4" @submit.prevent="onSave">
      <p class="text-[13px] text-text-2">
        Hive writes a diagnostic bundle to disk and opens a GitHub issue with your version and
        platform filled in. Nothing is sent anywhere. You choose what the file holds and whether
        to attach it.
      </p>

      <div class="overflow-hidden rounded-lg border border-card bg-raised">
        <div class="border-b border-row px-3.5 py-3">
          <div class="text-[13px] font-semibold text-text">Build and system info</div>
          <div class="mt-0.5 text-[11.5px] text-text-3">Version, commit, channel, OS and architecture. Always included.</div>
        </div>

        <div v-if="preview" class="flex flex-col">
          <div v-if="preview.hasLogs" class="flex items-center justify-between gap-3 border-b border-row px-3.5 py-3">
            <div class="min-w-0">
              <div class="text-[13px] text-text">Recent logs ({{ logKb }} KB)</div>
              <div class="mt-0.5 text-[11.5px] text-text-3">Names your home directory, repositories and branches.</div>
            </div>
            <AppSwitch
              :model-value="includeLogs"
              aria-label="Include recent logs"
              testid="report-include-logs"
              @update:model-value="includeLogs = $event"
            />
          </div>

          <div v-if="preview.hasSettings" class="flex items-center justify-between gap-3 border-b border-row px-3.5 py-3">
            <div class="min-w-0">
              <div class="text-[13px] text-text">Settings</div>
              <div class="mt-0.5 text-[11.5px] text-text-3">Names your hosts and storage locations.</div>
            </div>
            <AppSwitch
              :model-value="includeSettings"
              aria-label="Include settings"
              testid="report-include-settings"
              @update:model-value="includeSettings = $event"
            />
          </div>

          <div v-if="preview.flowCount > 0" class="flex items-center justify-between gap-3 border-b border-row px-3.5 py-3">
            <div class="min-w-0">
              <div class="text-[13px] text-text">Flows ({{ preview.flowCount }})</div>
              <div class="mt-0.5 text-[11.5px] text-text-3">Names the orgs, repositories and queries you follow.</div>
            </div>
            <AppSwitch
              :model-value="includeFlows"
              aria-label="Include flows"
              testid="report-include-flows"
              @update:model-value="includeFlows = $event"
            />
          </div>

          <div v-if="preview.hasActions" class="flex items-center justify-between gap-3 px-3.5 py-3">
            <div class="min-w-0">
              <div class="text-[13px] text-text">Actions</div>
              <div class="mt-0.5 text-[11.5px] text-text-3">Names the commands and targets you run.</div>
            </div>
            <AppSwitch
              :model-value="includeActions"
              aria-label="Include actions"
              testid="report-include-actions"
              @update:model-value="includeActions = $event"
            />
          </div>
        </div>
        <div v-else-if="loading" class="px-3.5 py-3 text-[12px] text-text-3">Preparing…</div>
      </div>

      <p class="text-[11.5px] text-text-3">
        Tokens, secrets and API keys are stripped from everything above. Names are not, so read
        the file before you attach it to a public issue.
      </p>
      <p v-if="error" class="text-[12px] text-severity-error" data-testid="report-error">{{ error }}</p>
    </form>

    <template #footer>
      <template v-if="saved">
        <BaseButton class="flex-1" data-testid="report-reveal" @click="reveal">Show file</BaseButton>
        <BaseButton variant="secondary" data-testid="report-open-issue" @click="openIssue">Open issue again</BaseButton>
        <BaseButton variant="secondary" data-testid="report-done" @click="emit('close')">Done</BaseButton>
      </template>
      <template v-else>
        <BaseButton
          class="flex-1"
          :busy="saving"
          :disabled="loading"
          data-testid="report-submit"
          @click="onSave"
        >{{ saving ? 'Saving…' : 'Save bundle & open issue' }}</BaseButton>
        <BaseButton variant="secondary" :busy="saving" @click="emit('close')">Cancel</BaseButton>
      </template>
    </template>
  </BaseModal>
</template>
