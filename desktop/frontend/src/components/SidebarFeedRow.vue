<script setup lang="ts">
// One feed entry in the sidebar's FEEDS section. Presentational: it renders a
// feed and emits select / mark-read, but drag-and-drop and grouping are the
// parent SideBar's concern (the parent wraps this row in a draggable drop zone).
import { computed, ref } from 'vue'
import AppMenu from './AppMenu.vue'
import IconEllipsis from '~icons/lucide/ellipsis'
import IconMailCheck from '~icons/lucide/mail-check'
import { feedIconComponent } from '../lib/feedIcons'
import { formatCombo, useKeybindings } from '../composables/useKeybindings'
import type { FeedSummary } from '../types/feed'
import type { MenuEntry } from '../types/menu'

const props = defineProps<{ feed: FeedSummary; selected: boolean }>()
const emit = defineEmits<{ select: []; 'mark-read': [] }>()

// The tree glyph resolves from the feed's configured icon key, falling back to
// the default when unset/unknown. The row's title is the feed's description
// (native tooltip on hover) when present, so LLM-generated feeds can explain
// their context; otherwise it falls back to the feed name.
const icon = computed(() => feedIconComponent(props.feed.icon))
const tooltip = computed(() => props.feed.description || props.feed.name)

// The row's "…" menu (also opened by right-click), same shape as an inbox
// row's. The shortcut hint is shown only on the selected row: the binding acts
// on the current selection, so on any other row it would name a key that does
// something else.
const { combosFor } = useKeybindings()
const root = ref<HTMLElement | null>(null)
const menuToggle = ref<HTMLElement | null>(null)
const menuOpen = ref(false)
const menuFlip = ref(false)
const entries = computed<MenuEntry[]>(() => {
  const combo = props.selected ? combosFor('feed.mark-all-read')[0] : undefined
  return [{ kind: 'action', id: 'mark-read', label: 'Mark all as read', icon: IconMailCheck, kbd: combo ? formatCombo(combo) : undefined, testid: 'sidebar-feed-mark-read' }]
})

// The sidebar is a scroll container, so an overflowing menu is clipped rather
// than allowed to hang outside it. Open upward when the row sits near the
// bottom of the window; the width caps at the row so a narrowed sidebar never
// clips the panel horizontally either.
function openMenu(): void {
  const rect = root.value?.getBoundingClientRect()
  menuFlip.value = rect != null && window.innerHeight - rect.bottom < 90 && rect.top > 90
  menuOpen.value = true
}
function toggleMenu(): void {
  if (menuOpen.value) menuOpen.value = false
  else openMenu()
}

function onSelect(id: string): void {
  if (id === 'mark-read') emit('mark-read')
  menuOpen.value = false
}
</script>

<template>
  <!-- Not a <button>: the menu toggle is a real button, which is invalid
       nested inside one. The div keeps the row focusable and Enter/Space
       select like the button did (`.self` so menu keystrokes don't select). -->
  <div
    ref="root"
    class="sidebar-entry"
    :class="{ 'sidebar-entry-selected': selected, 'menu-open': menuOpen }"
    role="button"
    tabindex="0"
    data-testid="sidebar-feed"
    :data-id="feed.id"
    :title="tooltip"
    @click="emit('select')"
    @keydown.enter.self.prevent="emit('select')"
    @keydown.space.self.prevent="emit('select')"
    @contextmenu.prevent="openMenu()"
  >
    <span class="nav-icon"><component :is="icon" class="size-3" /></span>
    <span class="min-w-0 flex-1 truncate text-left">{{ feed.name }}</span>
    <!-- The menu anchors to the row, not the kebab, so its right edge lands
         inside the sidebar. Clicks stay inside this wrapper so choosing an
         entry never also selects the row. -->
    <div class="flex shrink-0" @click.stop>
      <button
        ref="menuToggle"
        type="button"
        class="row-action"
        title="Feed actions"
        aria-label="Feed actions"
        aria-haspopup="menu"
        :aria-expanded="menuOpen"
        data-testid="sidebar-feed-menu-toggle"
        @click="toggleMenu()"
      ><IconEllipsis class="size-3" /></button>
      <AppMenu
        v-if="menuOpen"
        :entries="entries"
        :flip="menuFlip"
        width="min(230px, 100%)"
        :ignore="[menuToggle]"
        testid="sidebar-feed-menu"
        @close="menuOpen = false"
        @select="onSelect"
      />
    </div>
    <span class="font-mono text-[11px]" :class="feed.newCount ? 'text-accent' : 'text-text-3'">{{ feed.newCount || feed.count }}</span>
  </div>
</template>

<style scoped>
.sidebar-entry { position: relative; display: flex; align-items: center; gap: 9px; width: 100%; padding: 7px 8px; border-radius: 7px; color: var(--color-text-2); font-size: 13px; cursor: pointer; }
.sidebar-entry:hover, .sidebar-entry.menu-open { background: var(--color-chip); color: var(--color-text); }
.sidebar-entry:focus-visible { outline: 2px solid var(--color-accent); outline-offset: -2px; }
.sidebar-entry-selected { background: var(--color-hover); color: var(--color-accent); font-weight: 500; }
.sidebar-entry-selected .nav-icon { border-color: var(--color-accent-tint); color: var(--color-accent); }
.nav-icon { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 18px; height: 18px; border: 1px solid var(--color-strong); border-radius: 5px; background: var(--color-app); color: var(--color-text-2); }
/* Revealed by opacity, not display, so the kebab's column is always reserved:
   hovering a row never reflows the feed name or hides its unread count. Same
   affordance the folder header directly above these rows uses. */
.row-action { display: inline-flex; align-items: center; justify-content: center; width: 18px; height: 18px; border-radius: 5px; color: var(--color-text-4); cursor: pointer; opacity: 0; }
.row-action:hover, .row-action[aria-expanded="true"] { background: var(--color-app); color: var(--color-text); }
.sidebar-entry:hover .row-action, .row-action:focus-visible, .sidebar-entry.menu-open .row-action { opacity: 1; }
</style>
