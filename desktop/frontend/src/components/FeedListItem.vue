<script setup lang="ts">
import { computed, ref } from 'vue'
import ItemActionMenu from './ItemActionMenu.vue'
import SourceMark from './SourceMark.vue'
import { relativeAge } from '../lib/age'
import { defaultWebhookSourceIcon, feedIconComponent } from '../lib/feedIcons'
import { bodySnippet, feedSource, githubPayload, typeLabel } from '../lib/feedPresentation'
import IconArchive from '~icons/lucide/archive'
import IconEllipsis from '~icons/lucide/ellipsis'
import IconExternalLink from '~icons/lucide/external-link'
import IconEye from '~icons/lucide/eye'
import type { InboxItem } from '../types/feed'

const props = defineProps<{ item: InboxItem; archived?: boolean; trash?: boolean; selected: boolean; sourceIcons?: Record<string, string> }>()
const emit = defineEmits<{
  select: []
  'set-unread': [unread: boolean]
  'toggle-archive': []
  'toggle-ignored': []
  'open-browser': []
  'copy-link': []
  'copy-contents': []
  'run-action': [actionId: string]
}>()
const source = computed(() => feedSource(props.item))
// Webhook items render their source node's configured feed icon (falling
// back to the webhook glyph); GitHub keeps its brand mark inside SourceMark.
const sourceIcon = computed(() => source.value.key === 'webhook' ? feedIconComponent(props.sourceIcons?.[props.item.sourceScope] || defaultWebhookSourceIcon) : undefined)
const github = computed(() => githubPayload(props.item))
const type = computed(() => typeLabel(github.value.kind))
const snippet = computed(() => bodySnippet(github.value.body))
const typePillClass = computed(() => github.value.kind === 'PR' ? 'type-pill-pr' : github.value.kind === 'Issue' ? 'type-pill-issue' : 'type-pill-neutral')

// The row's "…" menu (also opened by right-click). Anchored under the kebab;
// flipped upward when the row sits too close to the bottom of the window for
// the menu to fit below.
const root = ref<HTMLElement | null>(null)
const menuToggle = ref<HTMLElement | null>(null)
const menuOpen = ref(false)
const menuFlip = ref(false)

function openMenu(): void {
  const rect = root.value?.getBoundingClientRect()
  menuFlip.value = rect != null && window.innerHeight - rect.bottom < 320 && rect.top > 320
  menuOpen.value = true
}
function toggleMenu(): void {
  if (menuOpen.value) menuOpen.value = false
  else openMenu()
}
</script>

<template>
  <!-- Not a <button>: the hover-action pill nests real buttons, which is
       invalid inside one. The div keeps the row focusable and Enter/Space
       select like the button did (`.self` so pill keystrokes don't select). -->
  <div ref="root" class="feed-item" :class="{ selected, 'menu-open': menuOpen }" role="button" tabindex="0" :data-id="item.externalId" :data-inbox-id="item.id" data-testid="feed-item" @click="emit('select')" @keydown.enter.self.prevent="emit('select')" @keydown.space.self.prevent="emit('select')" @contextmenu.prevent="openMenu()">
    <div class="relative flex items-start gap-3">
      <span class="source-badge" :data-source="source.key" data-testid="source-badge"><SourceMark :source="source" :icon="sourceIcon" class="size-4" /></span>
      <div class="min-w-0 flex-1">
        <div class="flex items-baseline gap-2.5"><div class="min-w-0 flex-1 truncate text-left text-[13.5px] leading-[1.35]" :class="item.unread ? 'font-semibold text-text' : 'font-normal text-text-2'">{{ item.title }}</div><div class="meta-right flex shrink-0 items-center gap-2"><span v-if="item.unread" data-testid="unread-dot" class="unread-dot" /><span class="font-mono text-[11px] text-text-4">{{ relativeAge(item.lastEventAt) }}</span></div></div>
        <div class="mt-[5px] flex min-w-0 items-center gap-2"><span v-if="archived && item.archivedReason" class="type-pill type-pill-neutral" data-testid="archive-reason">{{ item.archivedReason }}</span><span v-if="trash && item.ignoredAt != null" class="type-pill type-pill-neutral" data-testid="ignored-pill">ignored</span><span class="type-pill" :class="typePillClass" data-testid="type-pill" :data-kind="github.kind">{{ type }}</span><span class="min-w-0 truncate font-mono text-[11px] text-text-3">{{ source.label }}<template v-if="github.repo"> · {{ github.repo }}</template><template v-if="github.num"> #{{ github.num }}</template></span></div>
        <div v-if="github.author || snippet" class="mt-[5px] truncate text-left text-[12px] leading-[1.4] text-text-3" data-testid="item-snippet"><span v-if="github.author" class="text-text-2">{{ github.author }}</span><template v-if="github.author && snippet"> — </template>{{ snippet }}</div>
      </div>
    </div>
    <!-- Hover pill: swaps in over the unread dot + timestamp (which fade out)
         so triage never covers the title. Clicks stay inside the pill. -->
    <div class="hover-actions" data-testid="row-hover-actions" @click.stop>
      <button v-if="trash" class="hover-action" type="button" title="Stop ignoring" aria-label="Stop ignoring" data-testid="row-restore" @click="emit('toggle-ignored')"><IconEye class="size-[15px]" /></button>
      <button v-else class="hover-action" type="button" :title="item.archivedAt ? 'Move to inbox' : 'Archive'" :aria-label="item.archivedAt ? 'Move to inbox' : 'Archive'" data-testid="row-archive" @click="emit('toggle-archive')"><IconArchive class="size-[15px]" /></button>
      <button v-if="item.url" class="hover-action" type="button" title="Open in browser" aria-label="Open in browser" data-testid="row-open" @click="emit('open-browser')"><IconExternalLink class="size-[15px]" /></button>
      <div class="relative">
        <button ref="menuToggle" class="hover-action" type="button" title="More actions" aria-label="More actions" aria-haspopup="menu" :aria-expanded="menuOpen" data-testid="row-menu-toggle" @click="toggleMenu()"><IconEllipsis class="size-[15px]" /></button>
        <ItemActionMenu
          v-if="menuOpen"
          :item="item"
          :flip="menuFlip"
          :ignore="[menuToggle]"
          testid="row-menu"
          @close="menuOpen = false"
          @set-unread="(value) => emit('set-unread', value)"
          @toggle-archive="emit('toggle-archive')"
          @toggle-ignored="emit('toggle-ignored')"
          @open-browser="emit('open-browser')"
          @copy-link="emit('copy-link')"
          @copy-contents="emit('copy-contents')"
          @run-action="(actionId) => emit('run-action', actionId)"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.feed-item { position: relative; width: 100%; padding: 13px 16px 13px 18px; border-bottom: 1px solid var(--color-row); cursor: pointer; text-align: left; }
.feed-item:hover, .feed-item.menu-open { background: var(--color-row-hover); }.feed-item:focus-visible { outline: 2px solid var(--color-accent); outline-offset: -2px; }.feed-item.selected::after { content: ''; position: absolute; inset: 0; background: var(--color-selection); pointer-events: none; }.unread-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--color-accent); }.source-badge { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 30px; height: 30px; margin-top: 1px; border-radius: 8px; background: var(--color-chip); border: 1px solid var(--color-strong); color: var(--color-text); }.type-pill { display: inline-flex; flex: none; align-items: center; border-radius: 4px; padding: 2px 7px; font-family: var(--font-mono); font-size: 10px; font-weight: 600; letter-spacing: .02em; }.type-pill-pr { background: var(--color-kind-pr-tint); color: var(--color-kind-pr); }.type-pill-issue { background: var(--color-kind-issue-tint); color: var(--color-kind-issue); }.type-pill-neutral { background: var(--color-chip); color: var(--color-text-2); }
.meta-right { transition: opacity .1s ease; }
.hover-actions { position: absolute; top: 8px; right: 12px; z-index: 10; display: flex; gap: 2px; padding: 3px; border: 1px solid var(--color-strong); border-radius: 8px; background: var(--color-pane); box-shadow: 0 6px 18px -8px rgb(0 0 0 / .4); opacity: 0; pointer-events: none; transition: opacity .1s ease; }
.feed-item:hover .hover-actions, .feed-item:focus-within .hover-actions, .feed-item.menu-open .hover-actions { opacity: 1; pointer-events: auto; }
.feed-item:hover .meta-right, .feed-item:focus-within .meta-right, .feed-item.menu-open .meta-right { opacity: 0; pointer-events: none; }
.hover-action { display: inline-flex; align-items: center; justify-content: center; width: 26px; height: 26px; border-radius: 6px; color: var(--color-text-2); cursor: pointer; }
.hover-action:hover, .hover-action[aria-expanded="true"] { background: var(--color-hover); color: var(--color-text); }
.hover-action:focus-visible { outline: 2px solid var(--color-accent); outline-offset: -2px; }
@media (prefers-reduced-motion: reduce) { .hover-actions, .meta-right { transition: none; } }
</style>
