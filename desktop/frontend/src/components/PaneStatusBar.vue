<script setup lang="ts">
// The strip above a chat's or a session's terminal: which directory the pane
// is working in, and the two ways out of the app into it.
//
// Presentation only, and deliberately so — the Agents area reaches its backend
// over the loopback HTTP client and the terminal view over Wails bindings, so
// a component that fetched anything could only serve one of them. Each area
// passes what it resolved and handles the two events.
import IconCode from '~icons/lucide/code'
import IconFolder from '~icons/lucide/folder'
import IconFolderOpen from '~icons/lucide/folder-open'

withDefaults(defineProps<{
  /** What the pane is working in, as the user names it. */
  label: string
  /** The full path, shown on hover. */
  path?: string
  /** A failed open or reveal, shown until the next attempt. */
  error?: string
  /** The configured editor's display name; empty hides the editor button. */
  editorTitle?: string
  /** Prefix for this bar's test ids, so each area keeps its own. */
  testid: string
}>(), { path: '', error: '', editorTitle: '' })

defineEmits<{ 'open-editor': []; reveal: [] }>()
</script>

<template>
  <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-3 py-1" :data-testid="testid">
    <div class="flex min-w-0 items-center gap-1.5">
      <IconFolder class="size-3.5 shrink-0 text-text-4" aria-hidden="true" />
      <span
        class="min-w-0 truncate text-[11px] text-text-3"
        :title="path"
        :data-testid="`${testid}-workspace`"
      >{{ label }}</span>
    </div>

    <!-- Between the name and the buttons, so an area's own status reads as
         part of the same strip rather than as a second bar. -->
    <div class="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden"><slot /></div>

    <span
      v-if="error"
      class="truncate text-[11px] text-severity-error"
      :data-testid="`${testid}-error`"
    >{{ error }}</span>
    <button
      v-if="editorTitle"
      type="button"
      class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
      :title="`Open in ${editorTitle}`"
      :aria-label="`Open in ${editorTitle}`"
      :data-testid="`${testid}-open-editor`"
      @click="$emit('open-editor')"
    ><IconCode class="size-3.5" /></button>
    <button
      type="button"
      class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
      title="Show in Finder"
      aria-label="Show in Finder"
      :data-testid="`${testid}-reveal`"
      @click="$emit('reveal')"
    ><IconFolderOpen class="size-3.5" /></button>
  </div>
</template>
