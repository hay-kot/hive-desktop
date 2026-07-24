<script setup lang="ts">
// The app's only single-select control. A native select element renders its
// open list with the OS chrome (a light popup that ignores the app theme), so
// this reimplements the listbox: trigger button + popover, full keyboard
// handling, optional search filter and per-option icons.
//
// The popover teleports to <body> and is positioned from the trigger's
// bounding rect. Every call site sits inside something that clips — the
// actions drawer and the node editor both scroll, the create-session dialog is
// a modal — so an `absolute` popover inside a `relative` root would be cut off
// by the nearest `overflow` ancestor. Fixed positioning off body escapes all
// of them; the cost is repositioning on scroll/resize while open.
import { onClickOutside } from '@vueuse/core'
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import type { Component } from 'vue'
import IconCheck from '~icons/lucide/check'
import IconChevronDown from '~icons/lucide/chevron-down'
import IconSearch from '~icons/lucide/search'

export interface AppSelectOption {
  value: string
  label: string
  icon?: Component
  disabled?: boolean
}

/** `md` matches TextField's metrics (it sits beside one in every form); `sm` is for toolbars and sidebars. */
export type AppSelectSize = 'sm' | 'md'

const props = withDefaults(defineProps<{
  modelValue: string
  options: AppSelectOption[]
  /** Shown when modelValue matches no option. An option with value '' is a real choice, not a placeholder. */
  placeholder?: string
  searchable?: boolean
  searchPlaceholder?: string
  disabled?: boolean
  size?: AppSelectSize
  testid?: string
  ariaLabel?: string
}>(), { size: 'md' })

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const listboxId = useId()

const root = ref<HTMLElement | null>(null)
const popover = ref<HTMLElement | null>(null)
const list = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const open = ref(false)
const query = ref('')
const active = ref(0)

const selected = computed(() => props.options.find((option) => option.value === props.modelValue) ?? null)
const visible = computed(() => {
  const q = props.searchable ? query.value.trim().toLowerCase() : ''
  if (!q) return props.options
  return props.options.filter((option) => option.label.toLowerCase().includes(q))
})

const trigger = computed(() => ({
  sm: 'gap-1.5 rounded-md px-2 py-1.5 text-[11px]',
  md: 'gap-2 rounded-lg px-3 py-2.5 text-[13.5px]',
}[props.size]))
const optionText = computed(() => (props.size === 'sm' ? 'text-[12px]' : 'text-[13px]'))

// Keep the active index in range as the filtered list shrinks/grows.
watch(visible, (options) => { if (active.value >= options.length) active.value = Math.max(0, options.length - 1) })

/** The list scrolls past its max height (the icon picker is 30+ rows), so keep the active row on screen. */
function revealActive(): void {
  if (!open.value) return
  void nextTick(() => (list.value?.children[active.value]?.firstElementChild as HTMLElement | undefined)?.scrollIntoView?.({ block: 'nearest' }))
}
watch(active, revealActive)

function firstEnabled(): number {
  const index = visible.value.findIndex((option) => !option.disabled)
  return index === -1 ? 0 : index
}

function openList(): void {
  if (props.disabled) return
  open.value = true
  query.value = ''
  const current = visible.value.findIndex((option) => option.value === props.modelValue)
  active.value = current === -1 ? firstEnabled() : current
  measure()
  revealActive()
  if (props.searchable) void nextTick(() => searchInput.value?.focus())
}

function close(): void { open.value = false }
function toggle(): void { open.value ? close() : openList() }

function choose(option: AppSelectOption): void {
  if (option.disabled) return
  if (option.value !== props.modelValue) emit('update:modelValue', option.value)
  close()
}

/** Walk to the next enabled option, wrapping; a fully disabled list leaves `active` alone. */
function step(delta: number): void {
  const options = visible.value
  if (!options.length) return
  let index = active.value
  for (let n = 0; n < options.length; n++) {
    index = (index + delta + options.length) % options.length
    if (!options[index].disabled) { active.value = index; return }
  }
}

/** Home/End land on the outermost enabled option, not on a disabled edge row. */
function jump(edge: 'start' | 'end'): void {
  const options = visible.value
  const delta = edge === 'start' ? 1 : -1
  for (let index = edge === 'start' ? 0 : options.length - 1; index >= 0 && index < options.length; index += delta) {
    if (!options[index].disabled) { active.value = index; return }
  }
}

/** Shared by the trigger (closed list) and the search box (open list). */
function onKeydown(event: KeyboardEvent): void {
  if (!open.value) {
    if (event.key === 'ArrowDown' || event.key === 'Enter' || event.key === ' ') { event.preventDefault(); openList() }
    return
  }
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close() }
  else if (event.key === 'ArrowDown') { event.preventDefault(); step(1) }
  else if (event.key === 'ArrowUp') { event.preventDefault(); step(-1) }
  else if (event.key === 'Home') { event.preventDefault(); jump('start') }
  else if (event.key === 'End') { event.preventDefault(); jump('end') }
  else if (event.key === 'Enter' || (event.key === ' ' && !props.searchable)) {
    // Space types a character in the search box, so only Enter commits there.
    event.preventDefault()
    const option = visible.value[active.value]
    if (option) choose(option)
  }
}

// --- Anchored positioning -------------------------------------------------
const GAP = 6
const EDGE = 8
const MAX_HEIGHT = 320
const MIN_HEIGHT = 140

const anchor = ref({ left: 0, width: 0, top: 0, bottom: 0, flip: false, maxHeight: MAX_HEIGHT })

function measure(): void {
  const el = root.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const viewport = window.innerHeight
  const below = viewport - rect.bottom - GAP - EDGE
  const above = rect.top - GAP - EDGE
  const flip = below < MIN_HEIGHT && above > below
  anchor.value = {
    left: rect.left,
    width: rect.width,
    top: rect.bottom + GAP,
    bottom: viewport - rect.top + GAP,
    flip,
    maxHeight: Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, flip ? above : below)),
  }
}

const popoverStyle = computed(() => ({
  left: `${anchor.value.left}px`,
  width: `${anchor.value.width}px`,
  maxHeight: `${anchor.value.maxHeight}px`,
  ...(anchor.value.flip ? { bottom: `${anchor.value.bottom}px` } : { top: `${anchor.value.top}px` }),
}))

function bindReposition(): void {
  // Capture phase: scrolling any ancestor moves the trigger, not just window.
  window.addEventListener('scroll', measure, true)
  window.addEventListener('resize', measure)
}
function unbindReposition(): void {
  window.removeEventListener('scroll', measure, true)
  window.removeEventListener('resize', measure)
}

watch(open, (isOpen) => (isOpen ? bindReposition() : unbindReposition()))
onBeforeUnmount(unbindReposition)

// The popover lives outside `root` once teleported, so it has to be ignored
// explicitly or clicking an option would count as a click outside.
onClickOutside(root, () => { if (open.value) close() }, { ignore: [popover] })
</script>

<template>
  <div ref="root" class="relative" @keydown="onKeydown">
    <button
      type="button"
      class="flex w-full items-center justify-between border bg-app text-left text-text outline-none disabled:cursor-not-allowed disabled:opacity-60"
      :class="[trigger, open ? 'border-accent' : 'border-strong']"
      :data-testid="testid"
      :aria-label="ariaLabel"
      :disabled="disabled"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="open ? listboxId : undefined"
      @click="toggle"
    >
      <component :is="selected.icon" v-if="selected?.icon" class="size-4 shrink-0 text-text-2" />
      <span class="min-w-0 flex-1 truncate" :class="selected ? '' : 'text-text-4'">{{ selected ? selected.label : (placeholder ?? '') }}</span>
      <IconChevronDown class="size-4 shrink-0 text-text-3 transition-transform" :class="open ? 'rotate-180' : ''" />
    </button>

    <Teleport to="body">
      <div
        v-if="open"
        ref="popover"
        class="fixed z-50 flex flex-col overflow-hidden rounded-lg border border-card bg-raised shadow-[0_16px_34px_-12px_rgba(0,0,0,.6)]"
        :style="popoverStyle"
        :data-testid="testid ? `${testid}-popover` : undefined"
      >
        <div v-if="searchable" class="flex shrink-0 items-center gap-2 border-b border-row px-2.5 py-2">
          <IconSearch class="size-3.5 shrink-0 text-text-4" />
          <input
            ref="searchInput"
            v-model="query"
            type="text"
            :placeholder="searchPlaceholder ?? 'Search…'"
            class="min-w-0 flex-1 bg-transparent text-[13px] text-text outline-none placeholder:text-text-4"
            :data-testid="testid ? `${testid}-search` : undefined"
            @keydown="onKeydown"
          >
        </div>
        <ul
          v-if="visible.length"
          :id="listboxId"
          ref="list"
          class="hive-scroll flex min-h-0 flex-col gap-0.5 overflow-y-auto p-[5px]"
          role="listbox"
          :aria-label="ariaLabel"
        >
          <li v-for="(option, index) in visible" :key="option.value" role="option" :aria-selected="option.value === modelValue">
            <button
              type="button"
              class="flex w-full items-center gap-2 rounded-md px-[9px] py-[7px] text-left disabled:cursor-not-allowed disabled:opacity-40"
              :class="[optionText, index === active && !option.disabled ? 'bg-hover text-text' : 'text-text-2', option.value === modelValue ? 'text-text' : '']"
              :data-testid="testid ? `${testid}-option-${option.value}` : undefined"
              :disabled="option.disabled"
              @click="choose(option)"
              @mousemove="active = index"
            >
              <IconCheck class="size-3.5 shrink-0" :class="option.value === modelValue ? 'text-accent' : 'opacity-0'" :stroke-width="3" />
              <component :is="option.icon" v-if="option.icon" class="size-4 shrink-0 text-text-2" />
              <span class="min-w-0 flex-1 truncate">{{ option.label }}</span>
            </button>
          </li>
        </ul>
        <div v-else class="px-3 py-4 text-center text-[12.5px] text-text-4" :data-testid="testid ? `${testid}-empty` : undefined">
          {{ searchable && query.trim() ? 'No matches' : 'No options' }}
        </div>
      </div>
    </Teleport>
  </div>
</template>
