<script setup lang="ts">
import { ref } from 'vue'
import { onClickOutside } from '@vueuse/core'
import AppIcon from './AppIcon.vue'
import { useEscapeToClose } from '../composables/useEscapeToClose'
import type { MenuEntry } from '../types/menu'

// The shared dropdown menu. Owns the chrome (panel, entries, separators,
// group labels, shortcut hints) and dismissal (Escape, click-outside); the
// host owns the open flag and anchors the menu inside a `relative` wrapper.
// `ignore` should list the toggle button so its click doesn't close-then-reopen.
const props = defineProps<{
  entries: MenuEntry[]
  /** Open upward from the anchor — for hosts near the bottom of a scroll area. */
  flip?: boolean
  ignore?: (HTMLElement | null)[]
  testid?: string
}>()
const emit = defineEmits<{ select: [id: string]; close: [] }>()

const root = ref<HTMLElement | null>(null)
onClickOutside(root, () => emit('close'), { ignore: props.ignore ?? [] })
useEscapeToClose(() => emit('close'))
</script>

<template>
  <div ref="root" class="app-menu" :class="{ flip }" role="menu" :data-testid="testid">
    <template v-for="(entry, index) in entries" :key="index">
      <div v-if="entry.kind === 'separator'" class="app-menu-sep" />
      <div v-else-if="entry.kind === 'label'" class="app-menu-label">{{ entry.text }}</div>
      <button v-else class="app-menu-entry" role="menuitem" :data-testid="entry.testid" @click="emit('select', entry.id)">
        <component :is="entry.icon" v-if="entry.icon" class="size-3.5 shrink-0" :style="entry.iconColor ? { color: entry.iconColor } : undefined" />
        <AppIcon v-else-if="entry.iconName" :name="entry.iconName" class="size-3.5 shrink-0" :style="entry.iconColor ? { color: entry.iconColor } : undefined" />
        <span class="min-w-0 flex-1 truncate">{{ entry.label }}</span>
        <span v-if="entry.kbd" class="app-menu-kbd">{{ entry.kbd }}</span>
      </button>
    </template>
  </div>
</template>

<style scoped>
.app-menu { position: absolute; right: 0; top: calc(100% + 5px); z-index: 30; width: 230px; border: 1px solid var(--color-strong); border-radius: 8px; background: var(--color-pane); padding: 5px; box-shadow: 0 18px 45px -12px rgb(0 0 0 / .55); }
.app-menu.flip { top: auto; bottom: calc(100% + 5px); }
.app-menu-entry { display: flex; width: 100%; align-items: center; gap: 8px; cursor: pointer; border-radius: 6px; padding: 7px 9px; color: var(--color-text-2); font-size: 12px; text-align: left; }
.app-menu-entry:hover { background: var(--color-hover); color: var(--color-text); }
.app-menu-kbd { margin-left: auto; padding-left: 8px; font-family: var(--font-mono); font-size: 10.5px; color: var(--color-text-4); }
.app-menu-sep { height: 1px; margin: 5px 4px; background: var(--color-row); }
.app-menu-label { padding: 6px 9px 3px; font-family: var(--font-mono); font-size: 9.5px; font-weight: 600; letter-spacing: .1em; text-transform: uppercase; color: var(--color-text-4); }
</style>
