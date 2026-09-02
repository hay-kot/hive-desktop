<script setup lang="ts">
// The form behind a `schedules:` entry. It writes the manifest through the
// pane, never directly, and every answer about whether the entry is valid
// comes from the Go side's preview call: this component knows nothing about
// cron syntax or Go templates, it only debounces the question and renders the
// answer beside the field that raised it.
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import IconCalendarClock from '~icons/lucide/calendar-clock'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import { SelectField, TextField, TextareaField, ToggleField } from '../pipeline/fields'
import type { SelectOption } from '../pipeline/fields'
import { useAgentSchedules } from '../composables/useAgentSchedules'
import type { AgentSchedule, ScheduleEditRequest } from '../lib/agentWorkspacesClient'

/** Long enough that a keystroke does not cost a round trip, short enough to feel live. */
const PREVIEW_DEBOUNCE_MS = 300

/** How many of the preview's occurrences the form shows. */
const PREVIEW_SHOWN = 3

const props = defineProps<{
  workspace: string
  /** null opens the form for a new schedule; an existing one has a fixed id. */
  schedule: AgentSchedule | null
  busy: boolean
  error: string
}>()
const emit = defineEmits<{ close: []; save: [edit: ScheduleEditRequest] }>()

const formId = useId()

const name = ref(props.schedule?.name ?? '')
const cron = ref(props.schedule?.cron ?? '0 9 * * 5')
const prompt = ref(props.schedule?.prompt ?? '')
const onMissed = ref<string>(props.schedule?.onMissed ?? 'run')
const enabled = ref(!props.schedule?.disabled)

// The preset is derived from the cron rather than stored beside it, which is
// what makes editing the cron field fall back to Custom without a second
// watcher keeping two values honest.
const CRON_PRESETS: SelectOption[] = [
  { value: '', label: 'Custom' },
  { value: '@hourly', label: 'Every hour' },
  { value: '0 9 * * *', label: 'Every day at 09:00' },
  { value: '0 9 * * 1-5', label: 'Weekdays at 09:00' },
  { value: '0 9 * * 1', label: 'Every Monday at 09:00' },
  { value: '0 9 * * 5', label: 'Every Friday at 09:00' },
]

const preset = computed(() => (CRON_PRESETS.some((option) => option.value !== '' && option.value === cron.value) ? cron.value : ''))

function pickPreset(value: string): void {
  if (value) cron.value = value
}

const ON_MISSED_OPTIONS: SelectOption[] = [
  { value: 'run', label: 'Run once when the app is back' },
  { value: 'skip', label: 'Skip' },
]

const PROMPT_VARIABLES = [
  '{{ .Now }}',
  '{{ .ScheduledFor }}',
  '{{ .LastRun }}',
  '{{ .Reason }}',
  '{{ .Schedule.Name }}',
  '{{ .Workspace.Name }}',
  '{{ date "2006-01-02" .Now }}',
]
const promptHint = `Go template. Available: ${PROMPT_VARIABLES.join(', ')}`

// A new schedule's id follows its name so the manifest reads as prose; an
// existing one is fixed, because the id is what the run history and the
// scheduler's cursor are keyed by.
function slugify(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
}

const id = computed(() => props.schedule?.id ?? slugify(name.value))

const { preview, schedules } = useAgentSchedules()

// The Go side upserts by id, so a new entry landing on an existing id would
// replace that schedule with no warning. Editing one is exempt: its id is
// fixed, and the match it finds is itself.
const idTaken = computed(() => !props.schedule && schedules.value.some((row) => row.id === id.value))

const nextRuns = ref<number[]>([])
const cronError = ref('')
const promptError = ref('')
let previewTimer: ReturnType<typeof setTimeout> | undefined
let previewToken = 0

async function runPreview(): Promise<void> {
  const token = ++previewToken
  try {
    const result = await preview({ workspace: props.workspace, cron: cron.value, prompt: prompt.value })
    if (token !== previewToken) return
    nextRuns.value = result.next.slice(0, PREVIEW_SHOWN)
    cronError.value = result.cronError
    promptError.value = result.promptError
  } catch {
    // A preview that could not be asked for is not itself a reason to block a
    // save: the Go side validates the edit again on the way in. The errors go
    // with the occurrences, since they answered an older cron and prompt and
    // would otherwise disable the button for good.
    if (token !== previewToken) return
    nextRuns.value = []
    cronError.value = ''
    promptError.value = ''
  }
}

watch([cron, prompt], () => {
  clearTimeout(previewTimer)
  previewTimer = setTimeout(() => { void runPreview() }, PREVIEW_DEBOUNCE_MS)
}, { immediate: true })

onBeforeUnmount(() => clearTimeout(previewTimer))

const complete = computed(() => !!name.value.trim() && !!cron.value.trim() && !!prompt.value.trim() && !!id.value)
const canSave = computed(() => complete.value && !idTaken.value && !cronError.value && !promptError.value && !props.busy)

function occurrence(at: number): string {
  return new Date(at).toLocaleString([], {
    weekday: 'short', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false,
  })
}

function submit(): void {
  if (!canSave.value) return
  emit('save', {
    workspace: props.workspace,
    id: id.value,
    name: name.value.trim(),
    cron: cron.value.trim(),
    prompt: prompt.value,
    disabled: !enabled.value,
    onMissed: onMissed.value,
  })
}
</script>

<template>
  <BaseModal
    :title="props.schedule ? 'Edit schedule' : 'New schedule'"
    :icon="IconCalendarClock"
    :width="520"
    :busy="props.busy"
    testid="schedule-editor"
    @close="emit('close')"
  >
    <form :id="formId" class="flex flex-col gap-4 px-5 py-4" @submit.prevent="submit">
      <div class="flex flex-col gap-1.5">
        <TextField
          v-model="name"
          label="Name"
          placeholder="Weekly product summary"
          testid="schedule-editor-name"
        />
        <p class="font-mono text-[11px] text-text-4" data-testid="schedule-editor-id">{{ id || 'id derives from the name' }}</p>
        <p v-if="idTaken" class="text-[11px] text-severity-error" data-testid="schedule-editor-id-taken">A schedule with this id already exists</p>
      </div>

      <SelectField
        :model-value="preset"
        label="Timetable"
        :options="CRON_PRESETS"
        testid="schedule-editor-preset"
        @update:model-value="pickPreset"
      />

      <TextField
        v-model="cron"
        label="Cron"
        monospace
        placeholder="0 9 * * 5"
        :error="cronError"
        hint="Five fields in local time, or @hourly / @daily / @weekly / @monthly."
        testid="schedule-editor-cron"
      />

      <div class="flex flex-col gap-1.5">
        <p class="text-[12.5px] text-text-2">Next runs</p>
        <ul v-if="nextRuns.length" class="flex flex-col gap-0.5" data-testid="schedule-editor-next">
          <li v-for="at in nextRuns" :key="at" class="font-mono text-[11.5px] text-text-3">{{ occurrence(at) }}</li>
        </ul>
        <p v-else class="text-[11.5px] text-text-4" data-testid="schedule-editor-next-empty">
          {{ cronError ? 'Nothing to show while the cron is invalid.' : 'No upcoming runs.' }}
        </p>
      </div>

      <TextareaField
        v-model="prompt"
        label="Prompt"
        :rows="6"
        monospace
        placeholder="Summarize what changed since the last run."
        :error="promptError"
        :hint="promptHint"
        testid="schedule-editor-prompt"
      />

      <SelectField
        v-model="onMissed"
        label="When the app was closed"
        :options="ON_MISSED_OPTIONS"
        testid="schedule-editor-on-missed"
      />

      <ToggleField v-model="enabled" label="Enabled" testid="schedule-editor-enabled" />

      <p v-if="props.error" class="text-xs leading-relaxed text-severity-error" data-testid="schedule-editor-error">{{ props.error }}</p>
    </form>
    <template #footer>
      <BaseButton
        class="flex-1"
        type="submit"
        :form="formId"
        :busy="props.busy"
        :disabled="!canSave"
        data-testid="schedule-editor-save"
      >Save schedule</BaseButton>
      <BaseButton variant="secondary" :disabled="props.busy" data-testid="schedule-editor-cancel" @click="emit('close')">Cancel</BaseButton>
    </template>
  </BaseModal>
</template>
