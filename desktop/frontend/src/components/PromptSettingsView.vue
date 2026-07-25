<script setup lang="ts">
// The one place that answers "what can I hand to an agent to configure this
// app?". Every configurable surface — flows, actions, webhook sources,
// keyboard shortcuts, app settings — is listed with a paste-ready prompt that
// already names this install's real config paths.
//
// The list is whatever the prompts service reports, so adding a prompt is a
// template plus a registry entry in internal/app/prompts; this component
// needs no edit. It builds no prompt text of its own.
import { onMounted, ref } from 'vue'
import IconCheck from '~icons/lucide/check'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconCopy from '~icons/lucide/copy'
import BaseButton from './BaseButton.vue'
import BaseCard from './BaseCard.vue'
import SettingsSection from './settings/SettingsSection.vue'
import { useClipboard } from '../composables/useClipboard'
import { usePromptCatalog } from '../composables/usePrompts'

const { prompts, loading, error, refresh } = usePromptCatalog()
const { copy, status: copyStatus } = useClipboard({ resetDelay: 2500 })

// Which prompt's full text is expanded. Only one at a time — these run to
// hundreds of lines, and the point of the preview is a glance before copying,
// not side-by-side reading.
const expandedId = ref<string | null>(null)
// The prompt whose copy button was last pressed, so the tick appears on that
// row rather than on all of them.
const copiedId = ref<string | null>(null)

function toggle(id: string): void {
  expandedId.value = expandedId.value === id ? null : id
}

/** True while this row is showing its post-copy confirmation. */
function justCopied(id: string): boolean {
  return copiedId.value === id && copyStatus.value === 'success'
}

function copyFailed(id: string): boolean {
  return copiedId.value === id && copyStatus.value === 'error'
}

async function onCopy(id: string, text: string): Promise<void> {
  copiedId.value = id
  await copy(text)
}

onMounted(() => void refresh())
</script>

<template>
  <div class="mx-auto max-w-[720px]" data-testid="prompt-settings">
    <SettingsSection
      title="LLM prompts"
      description="Hive's configuration is plain text, so an agent can write it for you. Each prompt below is self-contained — schema, rules, a worked example, and this machine's real file paths — so you can paste it into a coding agent and describe what you want."
      class="mb-5"
    />

    <p v-if="loading" class="text-xs text-text-4" data-testid="prompt-settings-loading">Loading prompts…</p>
    <p
      v-else-if="error"
      class="rounded border border-severity-error bg-severity-error-tint px-3 py-2 text-xs text-severity-error"
      data-testid="prompt-settings-error"
    >{{ error }}</p>

    <div v-else class="flex flex-col gap-3">
      <BaseCard
        v-for="prompt in prompts"
        :key="prompt.id"
        class="flex-col items-stretch rounded-lg border border-border bg-raised"
        :data-testid="`prompt-${prompt.id}`"
      >
        <div class="flex items-start gap-4">
          <div class="min-w-0 flex-1">
            <div class="text-[13.5px] font-semibold text-text">{{ prompt.title }}</div>
            <p class="mt-1 text-xs leading-relaxed text-text-3">{{ prompt.description }}</p>
            <div
              v-if="prompt.target"
              class="mt-1.5 truncate font-mono text-[10.5px] text-text-4"
              :title="prompt.target"
            >{{ prompt.target }}</div>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <button
              type="button"
              class="flex cursor-pointer items-center gap-1 rounded-md px-2 py-1.5 text-[11.5px] font-medium text-text-3 hover:bg-chip hover:text-text"
              :aria-expanded="expandedId === prompt.id"
              :data-testid="`prompt-${prompt.id}-preview`"
              @click="toggle(prompt.id)"
            >
              Preview
              <IconChevronDown
                class="size-3.5 transition-transform"
                :class="expandedId === prompt.id ? 'rotate-180' : ''"
              />
            </button>
            <BaseButton
              size="sm"
              variant="secondary"
              :data-testid="`prompt-${prompt.id}-copy`"
              @click="onCopy(prompt.id, prompt.text)"
            >
              <template #icon>
                <IconCheck v-if="justCopied(prompt.id)" class="size-3.5" :stroke-width="2.4" />
                <IconCopy v-else class="size-3.5" :stroke-width="2.2" />
              </template>
              <!-- Both labels share one grid cell, so the button always
                   reserves the width of the longer one and confirming a copy
                   cannot reflow the row. A fixed min-width would do it too,
                   but breaks the moment the font or the wording changes. The
                   sizer is visibility:hidden, so it is not announced. -->
              <span class="grid text-center">
                <span class="invisible col-start-1 row-start-1">Copied</span>
                <span class="col-start-1 row-start-1" :data-testid="`prompt-${prompt.id}-copy-label`">
                  {{ justCopied(prompt.id) ? 'Copied' : 'Copy' }}
                </span>
              </span>
            </BaseButton>
          </div>
        </div>

        <!-- Only the failure path adds a row. It is rare and worth the jolt;
             the success path above is the one that must not move. -->
        <p
          v-if="copyFailed(prompt.id)"
          class="mt-2 text-[11.5px] text-severity-error"
          :data-testid="`prompt-${prompt.id}-copy-error`"
        >Could not copy to the clipboard.</p>

        <pre
          v-if="expandedId === prompt.id"
          class="hive-scroll mt-3 max-h-[320px] overflow-auto rounded-md border border-border bg-canvas p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-text-2"
          :data-testid="`prompt-${prompt.id}-text`"
        >{{ prompt.text }}</pre>
      </BaseCard>
    </div>
  </div>
</template>
