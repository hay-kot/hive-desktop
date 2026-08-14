<script setup lang="ts">
// A hover tooltip that appears when you meant it to, rather than after the
// platform's own delay. `title` is ~1-2s in WebKit and there is no knob for
// it, which is too slow for a status row whose glyphs are only readable
// through their tooltip.
//
// Teleported and fixed, because every place a tooltip is worth having sits
// inside something that clips — the session status bar's own slot is
// `overflow-hidden` so a long branch name truncates — and an absolutely
// positioned bubble would be cut off by it. Same reasoning as
// useAnchoredPopover, but not that composable: it sizes a list to its anchor's
// width with a 320px cap, and a tooltip wants neither.
import { onBeforeUnmount, ref } from 'vue'

const props = withDefaults(defineProps<{
  /** The tooltip text. Empty renders the trigger alone, with no tooltip at all. */
  text: string
  /**
   * Hover dwell before it appears. Long enough to read as a deliberate point
   * rather than firing under a cursor on its way past, and still far short of
   * the platform's own ~1.5s.
   */
  delay?: number
}>(), { delay: 300 })

const GAP = 6
const EDGE = 8

const anchor = ref<HTMLElement | null>(null)
const open = ref(false)
const bubble = ref<HTMLElement | null>(null)
const style = ref<Record<string, string>>({})
let timer: ReturnType<typeof setTimeout> | undefined

function place(): void {
  const el = anchor.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  // Measured from the rendered bubble, so showing runs this twice: once to put
  // it somewhere, once it knows its own width.
  const width = bubble.value?.getBoundingClientRect().width ?? 0
  const height = bubble.value?.getBoundingClientRect().height ?? 0
  const below = window.innerHeight - rect.bottom - GAP - EDGE
  const flip = height > 0 && below < height
  style.value = {
    left: `${Math.max(EDGE, Math.min(rect.left + rect.width / 2 - width / 2, window.innerWidth - EDGE - width))}px`,
    ...(flip ? { bottom: `${window.innerHeight - rect.top + GAP}px` } : { top: `${rect.bottom + GAP}px` }),
  }
}

function show(): void {
  if (!props.text) return
  open.value = true
  place()
  // The first place() ran without a bubble to measure; this one has one.
  requestAnimationFrame(place)
  window.addEventListener('scroll', hide, true)
  window.addEventListener('resize', hide)
}

function schedule(): void {
  clearTimeout(timer)
  timer = setTimeout(show, props.delay)
}

// Keyboard focus skips the dwell: arriving by Tab is already deliberate, and
// there is no cursor passing through on its way somewhere else.
function showNow(): void {
  clearTimeout(timer)
  show()
}

function hide(): void {
  clearTimeout(timer)
  open.value = false
  window.removeEventListener('scroll', hide, true)
  window.removeEventListener('resize', hide)
}

onBeforeUnmount(hide)
</script>

<template>
  <span
    ref="anchor"
    class="inline-flex"
    @pointerenter="schedule"
    @pointerleave="hide"
    @focusin="showNow"
    @focusout="hide"
  >
    <slot />
    <Teleport to="body">
      <div
        v-if="open"
        ref="bubble"
        class="app-tooltip"
        :style="style"
        role="tooltip"
        data-testid="app-tooltip"
      >{{ text }}</div>
    </Teleport>
  </span>
</template>

<style scoped>
/* pointer-events: none so the bubble can never sit between the cursor and the
   thing it describes, which would flicker it on and off. */
.app-tooltip {
  position: fixed;
  z-index: 60;
  max-width: 280px;
  pointer-events: none;
  border: 1px solid var(--color-strong);
  border-radius: 6px;
  background: var(--color-pane);
  padding: 4px 7px;
  color: var(--color-text-2);
  font-size: 11px;
  line-height: 1.35;
  box-shadow: 0 10px 28px -10px rgb(0 0 0 / .55);
}
</style>
