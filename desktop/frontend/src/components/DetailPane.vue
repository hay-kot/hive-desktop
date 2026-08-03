<script setup lang="ts">
import { computed, ref } from 'vue'
import ActionCard from './ActionCard.vue'
import ItemActionMenu from './ItemActionMenu.vue'
import PanelResizeHandle from './PanelResizeHandle.vue'
import SourceMark from './SourceMark.vue'
import { useResizablePanel } from '../composables/useResizablePanel'
import { body, byline, container, containerLine, kind, kindIcon, kindLabel, kindStyle, presentationFor } from '../lib/itemPresentation'
import { relativeAge } from '../lib/age'
import { renderGithubMarkdown } from '../lib/githubMarkdown'
import IconEllipsis from '~icons/lucide/ellipsis'
import IconSettings from '~icons/lucide/settings'
import type { InboxEvent, InboxItem } from '../types/feed'
import type { ActionView } from '../types/action'
import type { ActionRunView } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

const props = defineProps<{ item: InboxItem | null; actions: ActionView[]; events?: InboxEvent[]; pendingAction?: string | null; actionRuns?: Record<string, ActionRunView>; sourceIcons?: Record<string, string>; sourceImages?: Record<string, string> }>()
const emit = defineEmits<{
  'run-action': [actionId: string]
  'open-browser': []
  'open-url': [url: string]
  'set-unread': [unread: boolean]
  'toggle-archive': []
  'toggle-ignored': []
  'copy-link': []
  'copy-contents': []
  'create-session': []
  edit: []
}>()

const itemMenuToggle = ref<HTMLElement | null>(null)
const itemMenuOpen = ref(false)

// The detail header leads with the item's source badge and type. The badge
// already identifies the provider (via the sourceKind-keyed adapter
// registry), so the adjacent context stays focused on the repository and
// item number.
const presentation = computed(() => presentationFor(props.item?.sourceKind))
// The github/default adapters never dereference `item`; SourceMark only
// renders inside the `v-if="item"` branch below, so a null item here is
// never actually resolved to the webhook adapter (which does).
const markContext = computed(() => ({ sourceIcons: props.sourceIcons, sourceImages: props.sourceImages }))
const sourceMark = computed(() => presentation.value.mark(props.item!, markContext.value))
const sourceMarkImage = computed(() => presentation.value.markImage?.(props.item!, markContext.value))
// The kind pill mirrors the feed row's type pill: PR and Issue keep their
// GitHub styling, anything else (webhook items, custom kinds) is neutral.
const itemKind = computed(() => (props.item ? kind(props.item) : ''))
const itemKindLabel = computed(() => (props.item ? kindLabel(props.item) : ''))
const itemKindStyle = computed(() => (props.item ? kindStyle(props.item) : 'neutral'))
const itemKindIcon = computed(() => (props.item ? kindIcon(props.item) : undefined))
const itemContainer = computed(() => (props.item ? container(props.item) : ''))
const itemContainerLine = computed(() => (props.item ? containerLine(props.item) : ''))
const itemByline = computed(() => (props.item ? byline(props.item) : ''))

// Issue/PR bodies are GitHub-flavored markdown from untrusted authors;
// renderGithubMarkdown parses the GFM and escapes raw HTML / unsafe links, so
// the result is safe to inject with v-html.
const bodyHtml = computed(() => (props.item ? renderGithubMarkdown(body(props.item)) : ''))

// Links inside the rendered body must open in the user's real browser rather
// than navigate the webview away from the app. Intercept anchor clicks and
// hand the href up to the parent, which routes it through Wails.
function onBodyClick(event: MouseEvent) {
  const anchor = (event.target as HTMLElement).closest('a')
  if (!anchor) return
  event.preventDefault()
  const href = anchor.getAttribute('href') ?? ''
  if (/^(https?:|mailto:)/i.test(href)) emit('open-url', href)
}

// DetailPane is docked on the right, so its handle sits on its left border —
// dragging left (toward the FeedList) grows the pane.
// max is generous so the preview can take over most of the window like an
// email client's reading pane; the FeedList (flex-1, min-w-0) yields the space.
const { size: paneWidth, startResize: startPaneResize, step: stepPane } = useResizablePanel({
  storageKey: 'hive.panel.detailpane',
  defaultSize: 466,
  min: 360,
  max: 1100,
  edge: 'left',
})

// The markdown body is a fixed-height, scrollable reading pane (email-client
// style): the user drags the divider below it to set the height — larger than
// the content (blank space) or smaller (the body scrolls) — and it's
// persisted, so a long issue/PR description never buries the ACTIONS section.
const { size: bodyHeight, startResize: startBodyResize, step: stepBody } = useResizablePanel({
  storageKey: 'hive.panel.detailbody',
  defaultSize: 240,
  min: 96,
  max: 640,
  edge: 'bottom',
})
</script>

<template>
  <aside class="hive-scroll relative flex shrink-0 flex-col overflow-y-auto bg-pane" :style="{ width: paneWidth + 'px' }" data-testid="detail-pane">
    <PanelResizeHandle edge="left" name="detailpane" :start="startPaneResize" :step="stepPane" />
    <template v-if="item">
      <div class="relative border-b border-border px-5 pb-4 pt-[18px]">
        <div class="mb-[11px] flex items-center gap-[9px]">
          <span class="source-badge" :data-source="item.sourceKind" data-testid="source-badge"><SourceMark :icon="sourceMark" :image="sourceMarkImage" class="size-[15px]" /></span>
          <span class="kind-pill shrink-0 whitespace-nowrap" :class="'kind-pill-' + itemKindStyle" data-testid="kind-pill">
            <component :is="itemKindIcon" v-if="itemKindIcon" class="size-[13px]" />
            {{ itemKindLabel }}
          </span>
          <span v-if="itemContainer" class="min-w-0 truncate font-mono text-xs text-text-3">{{ itemContainerLine }}</span>
          <span class="flex-1" />
          <div class="relative shrink-0">
            <button ref="itemMenuToggle" class="more-button" aria-label="Item actions" aria-haspopup="menu" data-testid="item-actions-toggle" :aria-expanded="itemMenuOpen" @click="itemMenuOpen = !itemMenuOpen"><IconEllipsis class="size-4" /></button>
            <ItemActionMenu
              v-if="itemMenuOpen"
              :item="item"
              :actions="actions"
              :ignore="[itemMenuToggle]"
              testid="item-actions-menu"
              @close="itemMenuOpen = false"
              @set-unread="(value) => emit('set-unread', value)"
              @toggle-archive="emit('toggle-archive')"
              @toggle-ignored="emit('toggle-ignored')"
              @open-browser="emit('open-browser')"
              @copy-link="emit('copy-link')"
              @copy-contents="emit('copy-contents')"
              @create-session="emit('create-session')"
              @run-action="(actionId) => emit('run-action', actionId)"
            />
          </div>
        </div>
        <h1 class="text-[17px] font-semibold leading-[1.3] tracking-[-.01em]">{{ item.title }}</h1>
        <p class="mt-[9px] text-xs text-text-3"><template v-if="itemByline"><span class="text-text-2">{{ itemByline }}</span> · </template>{{ relativeAge(item.lastEventAt) === 'now' ? 'now' : `${relativeAge(item.lastEventAt)} ago` }}</p>
        <div v-if="bodyHtml" class="markdown-body hive-scroll mt-3 overflow-y-auto text-[14px] leading-[1.65] text-text-2" :style="{ height: bodyHeight + 'px' }" data-testid="detail-body" @click="onBodyClick" v-html="bodyHtml" />
        <!-- The border-b line below is draggable: it sets the description's
             reading-pane height (persisted), so long bodies never bury the actions. -->
        <PanelResizeHandle v-if="bodyHtml" edge="bottom" name="detailbody" :start="startBodyResize" :step="stepBody" />
      </div>

      <div class="px-5 pb-5 pt-4">
        <!-- Actions are capability-gated, not provider-gated: the block shows
             whenever the backend offered at least one applicable action
             (ActionApplicability — applies_to + payload capability), so it
             never renders an empty header for an item with zero matches. -->
        <template v-if="actions.length">
          <div class="mb-[13px] flex items-center gap-2">
            <span class="font-mono text-[10.5px] tracking-[.12em] text-accent">ACTIONS</span>
            <span class="font-mono text-[10.5px] text-text-4">· for {{ itemKind }}</span>
            <span class="flex-1" />
            <button class="edit-button" @click="emit('edit')"><IconSettings class="size-3" /> Edit</button>
          </div>
          <div class="action-list">
            <ActionCard v-for="action in actions" :key="action.id" :action="action" :pending="pendingAction === action.id" :run="actionRuns?.[action.id]" @run="emit('run-action', action.id)" />
          </div>
        </template>
        <section v-if="(events ?? []).length" class="mt-6 border-t border-border pt-4" data-testid="observed-activity">
          <h2 class="mb-3 font-mono text-[10.5px] tracking-[.12em] text-accent">ACTIVITY</h2>
          <ol class="space-y-2"><li v-for="event in events ?? []" :key="event.id" class="text-xs text-text-3"><span class="text-text-2">{{ event.summary || event.kind }}</span><span v-if="event.summary && event.kind !== 'observed'" class="ml-1 font-mono text-[10px] text-text-4">{{ event.kind.replaceAll('_', ' ') }}</span><span class="ml-2 font-mono text-[10px]">{{ relativeAge(event.createdAt) }}</span></li></ol>
        </section>
      </div>
    </template>
    <div v-else class="m-auto font-mono text-xs text-text-4">Select an item to inspect</div>
  </aside>
</template>

<style scoped>
.source-badge { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 26px; height: 26px; border-radius: 7px; background: var(--color-chip); border: 1px solid var(--color-strong); color: var(--color-text); }
.kind-pill { display: inline-flex; align-items: center; gap: 6px; height: 22px; padding: 0 9px 0 7px; border-radius: 6px; font-size: 11px; font-weight: 600; }
.kind-pill-pr { background: var(--color-kind-pr-tint); color: var(--color-kind-pr); }
.kind-pill-issue { background: var(--color-kind-issue-tint); color: var(--color-kind-issue); }
.kind-pill-neutral { background: var(--color-chip); color: var(--color-text-2); }
.edit-button, .more-button { display: inline-flex; align-items: center; gap: 4px; cursor: pointer; border: 1px solid var(--color-card); border-radius: 4px; padding: 2px 7px; color: var(--color-text-2); font-family: var(--font-mono); font-size: 11px; }
.edit-button { border-radius: 5px; padding: 3px 8px; font-family: var(--font-sans); }
.more-button { height: 24px; padding: 0 5px; }
.edit-button:hover, .more-button:hover, .more-button[aria-expanded="true"] { border-color: var(--color-strong); color: var(--color-text); }
.action-list { overflow: hidden; border: 1px solid var(--color-card); border-radius: 9px; background: var(--color-raised); }
.action-footer-meta { display: grid; grid-template-columns: 12px minmax(0, 1fr); column-gap: 8px; align-items: start; }

/* Rendered issue/PR body (GitHub-flavored markdown). Its height is set inline
   from the user-adjustable bodyHeight, so a long description scrolls internally
   instead of burying the ACTIONS section. */
.markdown-body :deep(h1), .markdown-body :deep(h2), .markdown-body :deep(h3),
.markdown-body :deep(h4), .markdown-body :deep(h5), .markdown-body :deep(h6) {
  margin: 20px 0 8px; color: var(--color-text); font-weight: 650; line-height: 1.3;
}
.markdown-body :deep(h1) { font-size: 19px; }
.markdown-body :deep(h2) { font-size: 16.5px; }
.markdown-body :deep(h3) { font-size: 15px; }
.markdown-body :deep(h4), .markdown-body :deep(h5), .markdown-body :deep(h6) { font-size: 14px; }
.markdown-body :deep(*:first-child) { margin-top: 0; }
.markdown-body :deep(*:last-child) { margin-bottom: 0; }
.markdown-body :deep(p) { margin: 10px 0; }
.markdown-body :deep(a) { color: var(--color-accent); text-decoration: underline; text-underline-offset: 2px; cursor: pointer; }
.markdown-body :deep(a:hover) { text-decoration-thickness: 2px; }
.markdown-body :deep(ul), .markdown-body :deep(ol) { margin: 10px 0; padding-left: 22px; }
.markdown-body :deep(ul) { list-style: disc; }
.markdown-body :deep(ol) { list-style: decimal; }
.markdown-body :deep(li) { margin: 4px 0; }
.markdown-body :deep(li)::marker { color: var(--color-text-4); }
/* Task-list checkboxes: a small custom control instead of the bright, oversized
   native checkbox. Rendered read-only (the plugin emits `disabled`). */
.markdown-body :deep(ul.contains-task-list) { padding-left: 4px; }
.markdown-body :deep(li.task-list-item) { list-style: none; }
.markdown-body :deep(li.task-list-item input) {
  appearance: none; -webkit-appearance: none;
  position: relative; box-sizing: border-box;
  width: 14px; height: 14px; margin: 0 8px 0 0; vertical-align: -2px;
  border: 1.5px solid var(--color-strong); border-radius: 4px;
  background: var(--color-app);
}
.markdown-body :deep(li.task-list-item input:checked) { background: var(--color-accent); border-color: var(--color-accent); }
.markdown-body :deep(li.task-list-item input:checked)::after {
  content: ''; position: absolute; left: 4px; top: 1px;
  width: 3.5px; height: 7px; border: solid var(--color-accent-contrast);
  border-width: 0 2px 2px 0; transform: rotate(45deg);
}
.markdown-body :deep(strong) { color: var(--color-text); font-weight: 650; }
.markdown-body :deep(s) { color: var(--color-text-3); }
.markdown-body :deep(blockquote) {
  margin: 10px 0; padding: 2px 14px; border-left: 3px solid var(--color-border);
  color: var(--color-text-3);
}
.markdown-body :deep(details) {
  margin: 10px 0; padding: 8px 12px; border: 1px solid var(--color-border);
  border-radius: 7px; background: var(--color-card);
}
.markdown-body :deep(summary) {
  cursor: pointer; color: var(--color-text); font-weight: 650;
}
.markdown-body :deep(details[open] summary) { margin-bottom: 8px; }
.markdown-body :deep(.markdown-alert) {
  --alert-color: var(--color-accent);
  margin: 10px 0; padding: 2px 14px; border-left: 3px solid var(--alert-color);
  color: var(--color-text-2); background: color-mix(in srgb, var(--alert-color) 7%, transparent);
}
.markdown-body :deep(.markdown-alert-title) { color: var(--alert-color); font-weight: 650; }
.markdown-body :deep(.markdown-alert-tip) { --alert-color: var(--color-kind-issue); }
.markdown-body :deep(.markdown-alert-important) { --alert-color: var(--color-kind-pr); }
.markdown-body :deep(.markdown-alert-warning),
.markdown-body :deep(.markdown-alert-caution) { --alert-color: var(--color-severity-warning); }
.markdown-body :deep(hr) { margin: 16px 0; border: 0; border-top: 1px solid var(--color-border); }
.markdown-body :deep(code) {
  padding: 1.5px 6px; border-radius: 5px; background: var(--color-card);
  font-family: var(--font-mono); font-size: 0.86em;
}
.markdown-body :deep(pre) {
  margin: 10px 0; padding: 12px 14px; overflow-x: auto; border-radius: 7px;
  background: var(--color-card); line-height: 1.5;
}
.markdown-body :deep(pre code) { padding: 0; background: transparent; font-size: 0.86em; }
.markdown-body :deep(table) { margin: 10px 0; border-collapse: collapse; display: block; overflow-x: auto; font-size: 0.95em; }
.markdown-body :deep(th), .markdown-body :deep(td) { padding: 5px 11px; border: 1px solid var(--color-border); text-align: left; }
.markdown-body :deep(th) { background: var(--color-card); font-weight: 650; }
.markdown-body :deep(img) { max-width: 100%; }
</style>
