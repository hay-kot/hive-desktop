<script setup lang="ts">
// Obsidian-style keybindings editor: every bindable command from the catalog,
// grouped and filterable, each with its current bindings as removable chips, a
// recorder to add a new combo, and a reset-to-default. Recording captures the
// next keystroke on the window in the capture phase and suppresses it from the
// global dispatcher (belt: kb.recording; suspenders: stopPropagation).
import { computed, onMounted, onUnmounted, ref } from 'vue'
import IconPlus from '~icons/lucide/plus'
import IconRotateCcw from '~icons/lucide/rotate-ccw'
import IconSearch from '~icons/lucide/search'
import IconTriangleAlert from '~icons/lucide/triangle-alert'
import IconX from '~icons/lucide/x'
import SettingsHeading from './settings/SettingsHeading.vue'
import SettingsPage from './settings/SettingsPage.vue'
import EmptyState from './settings/EmptyState.vue'
import { commands } from '../keybindings/catalog'
import { comboFromEvent, useKeybindings } from '../composables/useKeybindings'
import { requestedEditorFilter, useKeymapRows, type KeymapRow } from '../keybindings/keymapRows'

const kb = useKeybindings()
const filter = ref('')
const capturingId = ref<string | null>(null)

const catalogById = computed(() => new Map(commands.value.map((command) => [command.id, command])))
const titleFor = (id: string) => catalogById.value.get(id)?.title ?? id

// The ? scope's requested-filter handshake: a route landing here from a Keys
// row carries the command to land on. Applied once and cleared so a later,
// unrelated visit to this view starts unfiltered.
function applyRequestedFilter(): void {
  if (requestedEditorFilter.value === null) return
  filter.value = requestedEditorFilter.value
  requestedEditorFilter.value = null
}
onMounted(applyRequestedFilter)

const rows = useKeymapRows()

interface Group { group: string; rows: KeymapRow[] }

const groups = computed<Group[]>(() => {
  const query = filter.value.trim().toLowerCase()
  const byGroup = new Map<string, KeymapRow[]>()
  for (const row of rows.value) {
    if (query) {
      const keywords = catalogById.value.get(row.id)?.keywords ?? []
      const haystack = [row.title, row.group, ...keywords, ...row.formatted].join(' ').toLowerCase()
      if (!haystack.includes(query)) continue
    }
    if (!byGroup.has(row.group)) byGroup.set(row.group, [])
    byGroup.get(row.group)!.push(row)
  }
  return [...byGroup.entries()].map(([group, rows]) => ({ group, rows }))
})

const empty = computed(() => groups.value.length === 0)

function conflictTitles(id: string, combo: string): string[] {
  return kb.conflicts(combo, id).map(titleFor)
}

// ── Combo recording ───────────────────────────────────────────────────────────

function startCapture(id: string): void {
  if (capturingId.value) endCapture()
  capturingId.value = id
  kb.recording.value = true
  window.addEventListener('keydown', onCaptureKeydown, true) // capture phase
}

function endCapture(): void {
  if (!capturingId.value) return
  capturingId.value = null
  kb.recording.value = false
  window.removeEventListener('keydown', onCaptureKeydown, true)
}

function onCaptureKeydown(e: KeyboardEvent): void {
  const id = capturingId.value
  if (!id) return
  e.preventDefault()
  e.stopPropagation() // never reaches the global dispatcher or SettingsView's Escape
  if (e.key === 'Escape') {
    endCapture()
    return
  }
  const combo = comboFromEvent(e)
  if (!combo) return // lone modifier held — keep waiting for the full combo
  kb.addBinding(id, combo)
  endCapture()
}

function removeCombo(id: string, combo: string): void {
  kb.removeBinding(id, combo)
}

function reset(id: string): void {
  if (capturingId.value === id) endCapture()
  kb.resetToDefault(id)
}

onUnmounted(endCapture)
</script>

<template>
  <SettingsPage testid="settings-keybindings">
    <SettingsHeading
      title="Keyboard shortcuts"
      description="Rebind commands to your own keys. Bindings apply across the app; feed navigation keys work while the feed is open."
    >
      <template #actions>
        <label class="flex w-[220px] items-center gap-2 rounded-[7px] border border-card bg-app px-3 py-1.5 focus-within:border-accent">
          <IconSearch class="size-[14px] shrink-0 text-text-3" />
          <input
            v-model="filter"
            type="text"
            class="min-w-0 flex-1 border-none bg-transparent text-[12.5px] text-text outline-none placeholder:text-text-4"
            placeholder="Filter shortcuts…"
            data-testid="keybinding-filter"
          >
        </label>
      </template>
    </SettingsHeading>

    <EmptyState v-if="empty" boxed data-testid="keybinding-empty">
      No shortcuts match "{{ filter.trim() }}".
    </EmptyState>

    <div v-for="group in groups" :key="group.group" class="flex flex-col gap-2">
      <SettingsHeading level="group" :title="group.group" />
      <div class="overflow-hidden rounded-lg border border-border bg-raised">
        <div
          v-for="(row, index) in group.rows"
          :key="row.id"
          class="flex items-center gap-3 px-4 py-2.5"
          :class="index > 0 ? 'border-t border-row' : ''"
          data-testid="keybinding-row"
          :data-command-id="row.id"
        >
          <div class="min-w-0 flex-1 text-[13px] text-text">{{ row.title }}</div>

          <div class="flex flex-wrap items-center justify-end gap-2">
            <span
              v-for="(combo, i) in row.combos"
              :key="combo"
              class="combo"
              :class="conflictTitles(row.id, combo).length ? 'combo-conflict' : ''"
              :title="conflictTitles(row.id, combo).length ? `Also bound to ${conflictTitles(row.id, combo).join(', ')}` : undefined"
              data-testid="keybinding-combo"
            >
              <IconTriangleAlert v-if="conflictTitles(row.id, combo).length" class="size-3 shrink-0 text-accent" />
              <kbd class="keycap">{{ row.formatted[i] }}</kbd>
              <button
                type="button"
                class="combo-remove"
                aria-label="Remove shortcut"
                data-testid="keybinding-remove"
                @click="removeCombo(row.id, combo)"
              ><IconX class="size-3" /></button>
            </span>

            <span v-if="!row.combos.length && capturingId !== row.id" class="text-[11px] text-text-4">Blank</span>

            <span v-if="capturingId === row.id" class="capture-chip" data-testid="keybinding-capture">
              Press a key…&nbsp;<span class="text-text-4">Esc to cancel</span>
            </span>

            <button
              v-else
              type="button"
              class="icon-btn"
              aria-label="Add shortcut"
              data-testid="keybinding-add"
              @click="startCapture(row.id)"
            ><IconPlus class="size-3.5" /></button>

            <button
              v-if="row.overridden"
              type="button"
              class="icon-btn"
              aria-label="Reset to default"
              title="Reset to default"
              data-testid="keybinding-reset"
              @click="reset(row.id)"
            ><IconRotateCcw class="size-3.5" /></button>
          </div>
        </div>
      </div>
    </div>
  </SettingsPage>
</template>

<style scoped>
.combo { display: inline-flex; align-items: center; gap: 3px; }
/* A readable key cap: bright, mono, with a subtle physical-key bottom edge. */
.keycap {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 26px;
  height: 25px;
  padding: 0 9px;
  border: 1px solid var(--color-strong);
  border-bottom-width: 2px;
  border-radius: 6px;
  background: var(--color-chip);
  font-family: var(--font-mono);
  font-size: 12.5px;
  font-weight: 500;
  line-height: 1;
  color: var(--color-text);
}
.combo-conflict .keycap { border-color: var(--color-accent); color: var(--color-accent); }
/* The remove affordance is demoted so the key reads first; it lifts on hover. */
.combo-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  cursor: pointer;
  border-radius: 5px;
  color: var(--color-text-4);
  opacity: 0.4;
  transition: opacity 0.12s, color 0.12s, background 0.12s;
}
.combo:hover .combo-remove { opacity: 1; }
.combo-remove:hover { color: var(--color-text); background: var(--color-hover); }
.capture-chip {
  display: inline-flex;
  align-items: center;
  height: 25px;
  padding: 0 10px;
  border: 1px dashed var(--color-accent);
  border-radius: 6px;
  background: var(--color-chip);
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-text);
}
.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 25px;
  height: 25px;
  cursor: pointer;
  border: 1px solid var(--color-strong);
  border-radius: 7px;
  color: var(--color-text-2);
}
.icon-btn:hover { color: var(--color-text); border-color: var(--color-accent); }
</style>
