<script setup lang="ts">
// A workspace's canvases: agent-written markdown and link blocks, read-only
// in the webview — writes arrive only through the hive-canvas MCP tools, so
// this pane re-reads on canvas:updated rather than ever mutating
// (ADR canvases-are-named-files-in-the-workspace-folder-served-over-their-own-mcp-entry).
import { computed, toRef, watch } from 'vue'
import IconX from '~icons/lucide/x'
import AppSelect from './AppSelect.vue'
import PanelResizeHandle from './PanelResizeHandle.vue'
import type { AppSelectOption } from './AppSelect.vue'
import { useAgentCanvas } from '../composables/useAgentCanvas'
import { useResizablePanel } from '../composables/useResizablePanel'
import { useWailsEvent } from '../composables/useWailsEvent'
import { renderGithubMarkdown } from '../lib/githubMarkdown'
import type { AgentWorkspacesClient, CanvasBlock } from '../lib/agentWorkspacesClient'

const props = defineProps<{
  /** The open chat, whose most recent canvas is the default pick. */
  session: number
  /** The open chat's workspace, whose canvases fill the picker. */
  workspace: string
  /** The route-pinned canvas name; null lets the default win. */
  name: string | null
  client: AgentWorkspacesClient | null
}>()
const emit = defineEmits<{ close: []; 'open-url': [url: string]; pick: [name: string] }>()

const { canvas, metas, shown, loading, error, show, wake } =
  useAgentCanvas(toRef(props, 'client'))

watch(() => [props.workspace, props.name, props.session] as const, ([dir, name, session]) => {
  show(dir, name, session)
}, { immediate: true })

// A pick pins the name in the route; the prop watcher above brings it back.
function pick(value: string): void {
  emit('pick', value)
}

// Every write re-reads both the shown canvas and the picker's listing: the
// signal's payload can be coalesced away, and both reads are cheap.
useWailsEvent('canvas:updated', () => wake())

const pickerOptions = computed<AppSelectOption[]>(() => {
  const options = metas.value.map((meta) => ({
    value: meta.name,
    label: meta.title || meta.name,
  }))
  const current = shown.value
  if (current && !options.some((option) => option.value === current)) {
    options.unshift({ value: current, label: current })
  }
  return options
})

// GFM from an agent is untrusted by default: renderGithubMarkdown escapes raw
// HTML and drops unsafe link schemes, so the result is safe for v-html.
function renderBody(block: CanvasBlock): string {
  return renderGithubMarkdown(block.body)
}

// Links must open in the user's real browser rather than navigate the webview
// away from the app — DetailPane's interception, for the same reason.
function onBodyClick(event: MouseEvent): void {
  const anchor = (event.target as HTMLElement).closest('a')
  if (!anchor) return
  event.preventDefault()
  const href = anchor.getAttribute('href') ?? ''
  if (/^(https?:|mailto:)/i.test(href)) emit('open-url', href)
}

function openLinkBlock(block: CanvasBlock): void {
  if (/^(https?:|mailto:)/i.test(block.url)) emit('open-url', block.url)
}

const { size: paneWidth, startResize: startPaneResize, step: stepPane } = useResizablePanel({
  storageKey: 'hive.panel.agents.canvas',
  defaultSize: 380,
  min: 300,
  max: 900,
  edge: 'left',
})
</script>

<template>
  <aside
    class="relative flex shrink-0 flex-col border-l border-border bg-pane"
    :style="{ width: paneWidth + 'px' }"
    data-testid="agent-canvas-pane"
  >
    <PanelResizeHandle edge="left" name="agents-canvas" :start="startPaneResize" :step="stepPane" />

    <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-3 py-1">
      <AppSelect
        class="min-w-0 flex-1"
        size="sm"
        aria-label="Canvas"
        testid="agent-canvas-picker"
        :model-value="shown ?? ''"
        :options="pickerOptions"
        @update:model-value="pick"
      />
      <button
        type="button"
        class="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-[7px] text-text-3 hover:bg-chip hover:text-text"
        title="Close canvas"
        aria-label="Close canvas"
        data-testid="agent-canvas-close"
        @click="emit('close')"
      ><IconX class="size-3.5" /></button>
    </div>

    <div class="hive-scroll min-h-0 flex-1 overflow-y-auto px-4 py-3">
      <template v-if="canvas && canvas.blocks.length">
        <article
          v-for="block in canvas.blocks"
          :key="block.id"
          class="canvas-block"
          :data-testid="'agent-canvas-block-' + block.id"
        >
          <template v-if="block.kind === 'markdown'">
            <h2 v-if="block.title" class="mb-2 text-[13.5px] font-semibold text-text">{{ block.title }}</h2>
            <div class="markdown-body text-[13.5px] leading-[1.65] text-text-2" @click="onBodyClick" v-html="renderBody(block)" />
          </template>
          <button
            v-else-if="block.kind === 'link'"
            type="button"
            class="canvas-link"
            :title="block.url"
            @click="openLinkBlock(block)"
          >
            <span class="truncate text-[13.5px] text-accent underline underline-offset-2">{{ block.title }}</span>
            <span class="truncate font-mono text-[10.5px] text-text-4">{{ block.url }}</span>
          </button>
        </article>
      </template>
      <p v-else-if="error" class="text-xs leading-relaxed text-severity-error" data-testid="agent-canvas-error">{{ error }}</p>
      <p v-else-if="!loading" class="text-xs leading-relaxed text-text-4" data-testid="agent-canvas-empty">
        The agent hasn't put anything here yet.
      </p>
    </div>
  </aside>
</template>

<style scoped>
.canvas-block { padding: 12px 0; }
.canvas-block + .canvas-block { border-top: 1px solid var(--color-border); }
.canvas-block:first-child { padding-top: 0; }
.canvas-link { display: flex; width: 100%; min-width: 0; cursor: pointer; flex-direction: column; align-items: flex-start; gap: 2px; text-align: left; }
.canvas-link:hover span:first-child { text-decoration-thickness: 2px; }
</style>
