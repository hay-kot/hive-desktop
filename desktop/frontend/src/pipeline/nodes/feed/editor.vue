<script setup lang="ts">
// A feed node's identity is still its node id — the fields here are purely
// cosmetic sidebar presentation: the glyph shown in the tree and a hover
// tooltip that explains the feed's context (handy for LLM-generated feeds).
import { SelectField, TextareaField, TextField, ToggleField } from '../../fields'
import { defaultFeedIcon, feedIconOptions } from '../../../lib/feedIcons'
import { defaultNotifyTitle, descriptionMaxLen, severities, type Config, type Notify } from './config'

const props = defineProps<{ config: Config; errors?: string[] }>()
const emit = defineEmits<{ 'update:config': [config: Config] }>()

const iconOptions = feedIconOptions.map((o) => ({ value: o.value, label: o.label, icon: o.component }))

const severityOptions = severities.map((value) => ({
  value,
  label: { info: 'Info', success: 'Success', warning: 'Warning', error: 'Error' }[value],
}))

function setIcon(value: string) {
  emit('update:config', { ...props.config, icon: value || undefined })
}

function setDescription(e: Event) {
  const value = (e.target as HTMLTextAreaElement).value
  emit('update:config', { ...props.config, description: value || undefined })
}

// The notify block's presence is the on switch, so turning notifications off
// drops it entirely rather than leaving a disabled block's settings behind.
// Turning it on seeds the title a notification needs — the item's own — rather
// than starting from an invalid empty one.
function setNotify(enabled: boolean) {
  emit('update:config', { ...props.config, notify: enabled ? { title: defaultNotifyTitle } : undefined })
}

function updateNotify<K extends keyof Notify>(key: K, value: Notify[K]) {
  if (!props.config.notify) return
  emit('update:config', { ...props.config, notify: { ...props.config.notify, [key]: value } })
}
</script>

<template>
  <div class="flex flex-col gap-4 text-[13px] leading-relaxed" data-testid="feed-node-editor">
    <p class="text-text-2">
      Messages arriving here upsert into this feed as unread items. The feed
      appears in the sidebar under <span class="font-medium text-text">FEEDS</span>,
      named after this node.
    </p>

    <SelectField
      label="Sidebar icon"
      :model-value="config.icon || defaultFeedIcon"
      :options="iconOptions"
      searchable
      search-placeholder="Search icons…"
      hint="Shown next to the feed in the sidebar tree."
      testid="feed-editor-icon"
      @update:model-value="setIcon"
    />

    <div>
      <div class="mb-1.5 text-[12px] text-text-2">Description</div>
      <textarea
        :value="config.description ?? ''"
        rows="3"
        :maxlength="descriptionMaxLen"
        placeholder="Optional context shown as a tooltip when hovering the feed — useful for explaining generated feeds."
        class="w-full resize-y rounded-lg border border-strong bg-app px-[11px] py-[9px] text-[13px] text-text outline-none placeholder:text-text-4 focus:border-accent"
        data-testid="feed-editor-description"
        @input="setDescription"
      />
    </div>

    <div class="border-t border-border pt-4">
      <ToggleField
        label="Notify on new items"
        :model-value="!!config.notify"
        hint="Raise a system notification when something new lands here. Clicking it opens the item."
        testid="feed-editor-notify"
        @update:model-value="setNotify"
      />

      <div v-if="config.notify" class="mt-4 flex flex-col gap-4" data-testid="feed-editor-notify-options">
        <TextField
          label="Title"
          :model-value="config.notify.title ?? ''"
          placeholder="Review requested"
          hint="Go template over the item. Required — a notification without a title is rejected by the OS."
          monospace
          testid="feed-editor-notify-title"
          @update:model-value="(value: string) => updateNotify('title', value)"
        />

        <TextareaField
          label="Body"
          :model-value="config.notify.body ?? ''"
          :rows="2"
          placeholder="{{ .Payload.repo }} #{{ .Payload.num }} · {{ .Payload.title }}"
          hint="Go template over the item, same fields an action's templates see."
          monospace
          testid="feed-editor-notify-body"
          @update:model-value="(value: string) => updateNotify('body', value || undefined)"
        />

        <SelectField
          label="Severity"
          :model-value="config.notify.severity ?? 'info'"
          :options="severityOptions"
          hint="Warning and error are delivered as time-sensitive by the operating system."
          testid="feed-editor-notify-severity"
          @update:model-value="(value: string) => updateNotify('severity', value === 'info' ? undefined : value)"
        />

        <ToggleField
          label="Play sound"
          :model-value="config.notify.sound ?? true"
          hint="Silences this feed's notifications. The app's sound preference still applies."
          testid="feed-editor-notify-sound"
          @update:model-value="(value: boolean) => updateNotify('sound', value ? undefined : false)"
        />

        <p class="text-text-3">
          Only genuinely new activity notifies — a poll re-observing an unchanged
          item does not. Notifications obey the app's notification settings; a
          feed can never override the kill switch or the delivery mode.
        </p>
      </div>
    </div>
  </div>
</template>
