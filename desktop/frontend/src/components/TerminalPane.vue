<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { TerminalPane } from '../composables/useTerminalWindows'

const props = defineProps<{ pane: TerminalPane; active: boolean }>()
const emit = defineEmits<{ mount: [paneId: string, host: HTMLElement]; select: [paneId: string] }>()

const host = ref<HTMLElement | null>(null)

onMounted(() => {
  if (host.value) emit('mount', props.pane.paneId, host.value)
})
</script>

<template>
  <!-- Positioned by the window that holds it, in cells of tmux's grid. A
       press anywhere in the pane's box is an intent to type in it: xterm only
       takes focus from a press inside its own screen element, which is a
       whole number of cells, and the part-cell remainder would otherwise
       swallow it. -->
  <div
    class="terminal-pane absolute overflow-hidden"
    data-testid="terminal-pane-host"
    :data-pane-id="pane.paneId"
    :data-active-pane="active ? 'true' : undefined"
    @mousedown="emit('select', pane.paneId)"
  >
    <div ref="host" class="size-full" />
  </div>
</template>

<style scoped>
/* The pane's box is exactly its canvas, which xterm draws over the viewport,
   so a scrollbar would sit under it and show nowhere while the size vote had
   to keep a column free for it. Hidden, the wheel and the scrolled-up pill
   are the way through scrollback; xterm's own wheel handling scrolls the
   viewport element either way. */
.terminal-pane :deep(.xterm-viewport) { scrollbar-width: none; }
.terminal-pane :deep(.xterm-viewport::-webkit-scrollbar) { display: none; }
</style>
