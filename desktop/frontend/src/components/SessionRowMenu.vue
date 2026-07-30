<script setup lang="ts">
import { computed } from 'vue'
import AppMenu from './AppMenu.vue'
import IconInfo from '~icons/lucide/info'
import IconPencil from '~icons/lucide/pencil'
import IconRecycle from '~icons/lucide/recycle'
import IconTags from '~icons/lucide/tags'
import IconTrash from '~icons/lucide/trash-2'
import type { SessionSummary } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'
import type { MenuEntry } from '../types/menu'

// The one menu for a hive session, opened from the session row. It owns the
// session's own operations; anything a *host* contributes — a new tab per
// configured agent, a user-defined action — arrives as `extra` and lands in its
// own labelled section, so a second menu never has to be built beside this one.
const props = defineProps<{
  session: SessionSummary
  /** Host-contributed entries, appended under `extraLabel`. */
  extra?: MenuEntry[]
  extraLabel?: string
  flip?: boolean
  ignore?: (HTMLElement | null)[]
  testid?: string
}>()
const emit = defineEmits<{
  close: []
  detail: []
  rename: []
  group: []
  recycle: []
  delete: []
  extra: [id: string]
}>()

const entries = computed<MenuEntry[]>(() => {
  const list: MenuEntry[] = [
    { kind: 'action', id: 'detail', label: 'Session details…', icon: IconInfo, testid: 'session-menu-detail' },
    { kind: 'separator' },
    { kind: 'action', id: 'rename', label: 'Rename…', icon: IconPencil, testid: 'session-menu-rename' },
    {
      kind: 'action',
      id: 'group',
      label: props.session.group ? 'Change group…' : 'Set group…',
      icon: IconTags,
      testid: 'session-menu-group',
    },
    { kind: 'separator' },
  ]
  // Only an active session has a clone to reset; recycling a recycled one is
  // rejected by hive, so it is not offered.
  if (props.session.state === 'active') {
    list.push({ kind: 'action', id: 'recycle', label: 'Recycle…', icon: IconRecycle, testid: 'session-menu-recycle' })
  }
  list.push({ kind: 'action', id: 'delete', label: 'Delete…', icon: IconTrash, testid: 'session-menu-delete' })
  if (props.extra?.length) {
    list.push({ kind: 'separator' }, { kind: 'label', text: props.extraLabel ?? 'Actions' }, ...props.extra)
  }
  return list
})

const own = new Set(['detail', 'rename', 'group', 'recycle', 'delete'])

function onSelect(id: string): void {
  if (id === 'detail') emit('detail')
  else if (id === 'rename') emit('rename')
  else if (id === 'group') emit('group')
  else if (id === 'recycle') emit('recycle')
  else if (id === 'delete') emit('delete')
  if (!own.has(id)) emit('extra', id)
  emit('close')
}
</script>

<template>
  <AppMenu
    :entries="entries"
    :flip="flip"
    width="min(230px, 100%)"
    :ignore="ignore"
    :testid="testid ?? 'session-row-menu'"
    @select="onSelect"
    @close="emit('close')"
  />
</template>
