<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import IconTerminal from '~icons/lucide/terminal'
import AppMenu from './AppMenu.vue'
import { SessionLaunchOptions } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'
import type { MenuEntry } from '../types/menu'

const props = defineProps<{ running: boolean; flip: boolean; ignore: (HTMLElement | null)[] }>()
const emit = defineEmits<{ close: []; terminal: []; agent: [profile: string] }>()
const agents = ref<string[]>([])
const defaultAgent = ref('')
const loading = ref(true)
const failed = ref(false)

async function load(): Promise<void> {
  loading.value = true
  failed.value = false
  try {
    const options = await SessionLaunchOptions()
    agents.value = options.agents ?? []
    defaultAgent.value = options.defaultAgent
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}
onMounted(load)

const entries = computed<MenuEntry[]>(() => [
  { kind: 'action', id: 'terminal', label: props.running ? 'New terminal' : 'Start session', icon: IconTerminal, testid: 'new-window-terminal' },
  { kind: 'separator' },
  { kind: 'label', text: 'New agent · shared checkout' },
  ...(!props.running ? [{ kind: 'label' as const, text: 'Start the session to add an agent' }] : []),
  ...(loading.value
    ? [{ kind: 'label' as const, text: 'Loading agents…' }]
    : failed.value
      ? [{ kind: 'action' as const, id: 'retry', label: 'Could not load agents. Retry', testid: 'new-window-retry' }]
      : agents.value.length
        ? agents.value.map((agent) => ({ kind: 'action' as const, id: `agent:${agent}`, label: agent === defaultAgent.value ? `${agent} (default)` : agent, disabled: !props.running, testid: `new-window-agent-${agent}` }))
        : [{ kind: 'label' as const, text: 'No agents configured' }]),
])

function select(id: string): void {
  if (id === 'retry') {
    void load()
    return
  }
  emit('close')
  if (id === 'terminal') emit('terminal')
  else if (id.startsWith('agent:')) emit('agent', id.slice(6))
}
</script>

<template>
  <AppMenu :entries="entries" :flip="flip" :ignore="ignore" width="min(260px, 100%)" testid="new-window-menu" @select="select" @close="emit('close')" />
</template>
