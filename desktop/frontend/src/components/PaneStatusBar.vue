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
import AppTooltip from './AppTooltip.vue'

withDefaults(defineProps<{
  /**
   * What the pane is working in, as the user names it. Empty drops the name
   * entirely: the Code view's sidebar already names the session in front of
   * you, so repeating it here is a row of chrome carrying nothing.
   */
  label?: string
  /** The full path, shown on hover. */
  path?: string
  /** A failed open or reveal, shown until the next attempt. */
  error?: string
  /** The configured editor's display name; empty hides the editor button. */
  editorTitle?: string
  /** Prefix for this bar's test ids, so each area keeps its own. */
  testid: string
}>(), { label: '', path: '', error: '', editorTitle: '' })

defineEmits<{ 'open-editor': []; reveal: [] }>()
</script>

<template>
  <!-- Every item in this row is a 24px-tall box carrying its own px-1.5, so the
       space between any two is their padding plus one gap and stays even
       whether the neighbours are text, an icon or a button. Growing the padding
       instead of the gap is what this avoids: the icon buttons are size-6 to
       match SessionStatusChips, and widening them would break that. The row's
       own px-1.5 completes the first and last item's padding to the 12px inset
       the bar had when it was px-3 with bare children. -->
  <div class="flex shrink-0 items-center gap-1 border-b border-border px-1.5 py-1" :data-testid="testid">
    <div v-if="label" class="flex h-6 min-w-0 items-center gap-1.5 px-1.5">
      <IconFolder class="size-3.5 shrink-0 text-text-4" aria-hidden="true" />
      <span
        class="min-w-0 truncate text-[11px] text-text-3"
        :title="path"
        :data-testid="`${testid}-workspace`"
      >{{ label }}</span>
    </div>

    <!-- Between the name and the buttons, so an area's own status reads as
         part of the same strip rather than as a second bar. -->
    <div class="flex min-w-0 flex-1 items-center overflow-hidden"><slot /></div>

    <span
      v-if="error"
      class="flex h-6 shrink-0 items-center truncate px-1.5 text-[11px] text-severity-error"
      :data-testid="`${testid}-error`"
    >{{ error }}</span>
    <!-- size-6/rounded-[7px] is also what SessionStatusChips' clickable chips
         use, so everything hoverable in this row shares one height and corner.
         Changing it here means changing it there. -->
    <AppTooltip v-if="editorTitle" :text="`Open in ${editorTitle}`">
      <button
        type="button"
        class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
        :aria-label="`Open in ${editorTitle}`"
        :data-testid="`${testid}-open-editor`"
        @click="$emit('open-editor')"
      ><IconCode class="size-3.5" /></button>
    </AppTooltip>
    <AppTooltip text="Show in Finder">
      <button
        type="button"
        class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
        aria-label="Show in Finder"
        :data-testid="`${testid}-reveal`"
        @click="$emit('reveal')"
      ><IconFolderOpen class="size-3.5" /></button>
    </AppTooltip>
  </div>
</template>
