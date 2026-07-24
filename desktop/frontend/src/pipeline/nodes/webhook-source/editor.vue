<script setup lang="ts">
// webhook-source has no runtime.ts (deliveries are ingested by the Go
// listener). Beyond the config fields the editor surfaces backend-derived
// affordances: the endpoint URL to paste into the sending system, the last
// captured delivery with the non-blocking feed-shape hint, and the copyable
// LLM prompt for authoring a downstream function-node transform. Backend
// access goes through the `client` prop (defaulting to the generated Wails
// bindings) so tests inject fakes instead of mocking module imports.
import { computed, onMounted, ref } from 'vue'
import { Capture, Info } from '../../../../bindings/github.com/hay-kot/hive-desktop/desktop/webhookservice'
import BaseButton from '../../../components/BaseButton.vue'
import { useClipboard } from '../../../composables/useClipboard'
import { defaultWebhookSourceIcon, feedIconOptions } from '../../../lib/feedIcons'
import { SearchableSelectField, TextField } from '../../fields'
import type { Config } from './config'
import { buildTransformPrompt } from './prompt'

export interface WebhookInfoView {
  running: boolean
  port: number
  baseUrl: string
}

export interface WebhookCaptureView {
  receivedAt: number
  body: string
  feedShaped: boolean
  missingFields: string[] | null
}

export interface WebhookEditorClient {
  info(): Promise<WebhookInfoView>
  capture(flowId: string, nodeId: string): Promise<WebhookCaptureView>
}

const props = defineProps<{
  config: Config
  errors?: string[]
  /** Node identity, threaded through NodeEditorDrawer for the capture lookup. */
  flowId?: string
  nodeId?: string
  client?: WebhookEditorClient
}>()

const emit = defineEmits<{ 'update:config': [config: Config] }>()

const client: WebhookEditorClient = props.client ?? {
  async info() { return await Info() },
  async capture(flowId, nodeId) { return await Capture(flowId, nodeId) },
}

const info = ref<WebhookInfoView | null>(null)
const capture = ref<WebhookCaptureView | null>(null)

onMounted(async () => {
  try {
    info.value = await client.info()
  } catch { /* endpoint URL row simply stays hidden */ }
  if (!props.flowId || !props.nodeId) return
  try {
    capture.value = await client.capture(props.flowId, props.nodeId)
  } catch { /* capture section falls back to "none yet" */ }
})

function updatePath(path: string) {
  emit('update:config', { ...props.config, path })
}

function updateSecret(secret: string) {
  emit('update:config', { ...props.config, secret: secret || undefined })
}

const iconOptions = feedIconOptions.map((o) => ({ value: o.value, label: o.label, icon: o.component }))

function updateIcon(icon: string) {
  emit('update:config', { ...props.config, icon: icon || undefined })
}

const endpointUrl = computed(() => {
  if (!info.value || !props.config.path?.trim()) return ''
  return info.value.baseUrl + props.config.path
})

const hasCapture = computed(() => (capture.value?.receivedAt ?? 0) > 0)
const capturedAt = computed(() => hasCapture.value ? new Date(capture.value!.receivedAt).toLocaleString() : '')
const missingFields = computed(() => capture.value?.missingFields ?? [])

const capturePreview = computed(() => {
  if (!hasCapture.value) return ''
  try {
    return JSON.stringify(JSON.parse(capture.value!.body), null, 2)
  } catch {
    return capture.value!.body
  }
})

const { copy: copyUrl, copied: urlCopied } = useClipboard()
const { copy: copyPrompt, copied: promptCopied } = useClipboard({ resetDelay: 2500 })

function onCopyPrompt() {
  void copyPrompt(buildTransformPrompt({
    path: props.config.path ?? '',
    sample: hasCapture.value ? capture.value!.body : undefined,
  }))
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <TextField
      label="Path"
      :model-value="config.path ?? ''"
      placeholder="ci-alerts"
      hint="Served at /hooks/<path> on the local webhook listener."
      monospace
      testid="webhook-source-editor-path"
      @update:model-value="updatePath"
    />
    <TextField
      label="Secret"
      :model-value="config.secret ?? ''"
      placeholder="optional shared secret"
      hint="When set, requests must send this value in the X-Hive-Secret header."
      monospace
      testid="webhook-source-editor-secret"
      @update:model-value="updateSecret"
    />
    <SearchableSelectField
      label="Item icon"
      :model-value="config.icon || defaultWebhookSourceIcon"
      :options="iconOptions"
      search-placeholder="Search icons…"
      hint="Shown on this source's items in feeds."
      testid="webhook-source-editor-icon"
      @update:model-value="updateIcon"
    />

    <div v-if="endpointUrl">
      <div class="mb-1.5 text-[12px] text-text-2">Endpoint</div>
      <div class="flex items-center gap-2">
        <code
          class="min-w-0 flex-1 truncate rounded-lg border border-strong bg-app px-3 py-2 font-mono text-[12px] text-text-2"
          data-testid="webhook-source-editor-url"
        >{{ endpointUrl }}</code>
        <BaseButton variant="secondary" size="sm" class="whitespace-nowrap" data-testid="webhook-source-editor-copy-url" @click="copyUrl(endpointUrl)">
          {{ urlCopied ? 'Copied' : 'Copy' }}
        </BaseButton>
      </div>
      <p v-if="info && !info.running" class="mt-1.5 text-[11.5px] text-text-4">
        The listener is not running in this session; the URL applies to a live desktop run.
      </p>
    </div>

    <div>
      <div class="mb-1.5 text-[12px] text-text-2">Last delivery</div>
      <template v-if="hasCapture">
        <div class="mb-1.5 font-mono text-[11px] text-text-4" data-testid="webhook-source-editor-captured-at">{{ capturedAt }}</div>
        <div
          v-if="missingFields.length > 0"
          class="mb-2 rounded-lg border border-strong bg-selection px-3 py-2.5 text-[12px] leading-relaxed text-text-2"
          data-testid="webhook-source-editor-shape-warning"
        >
          This payload isn't feed-item-shaped (missing <span class="font-mono">{{ missingFields.join(', ') }}</span>).
          It ingests fine, but will render minimally in feeds — add a <span class="font-mono">function</span> node
          to reshape it, or copy the LLM prompt below to have one written for you.
        </div>
        <pre
          class="max-h-48 overflow-auto rounded-lg border border-row bg-app px-3 py-2.5 font-mono text-[11.5px] leading-relaxed text-text-2"
          data-testid="webhook-source-editor-capture"
        >{{ capturePreview }}</pre>
      </template>
      <p v-else class="text-[12px] text-text-4" data-testid="webhook-source-editor-no-capture">
        Nothing captured yet — POST JSON to the endpoint and reopen this editor to see the payload here.
      </p>
    </div>

    <div class="flex items-center gap-2.5">
      <BaseButton variant="secondary" size="sm" class="whitespace-nowrap" data-testid="webhook-source-editor-copy-prompt" @click="onCopyPrompt">
        Copy LLM prompt
      </BaseButton>
      <span v-if="promptCopied" class="text-[11.5px] text-text-4">Copied — paste into your coding agent</span>
      <span v-else class="text-[11.5px] text-text-4">Generates a function-node transform for this payload</span>
    </div>
  </div>
</template>
