<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { TerminalWindowTab } from '../composables/useTerminalWindows'

const props = defineProps<{ tab: TerminalWindowTab; active: boolean }>()
const emit = defineEmits<{ mount: [windowId: string, host: HTMLElement] }>()

const host = ref<HTMLElement | null>(null)

onMounted(() => {
  if (host.value) emit('mount', props.tab.windowId, host.value)
})
</script>

<template>
  <!-- Hidden rather than unmounted: a Terminal binds to one element for its
       lifetime, so unmounting an inactive tab would cost it its screen. -->
  <div
    v-show="active"
    class="min-h-0 min-w-0 flex-1 overflow-hidden bg-app px-2 py-1.5"
    data-terminal-input-scope
    data-testid="terminal-pane"
    :data-window-id="tab.windowId"
  >
    <div ref="host" class="size-full" />
  </div>
</template>
