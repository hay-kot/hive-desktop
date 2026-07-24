<script setup lang="ts">
// Edit dialog for a sidebar feed folder: rename, plus the only route to
// deleting it. Delete is deliberately behind this dialog rather than a hover
// button in the tree — the parent still confirms it before anything is written.
import { computed, nextTick, onMounted, ref } from 'vue'
import IconFolder from '~icons/lucide/folder'
import IconTrash2 from '~icons/lucide/trash-2'
import BaseButton from './BaseButton.vue'
import BaseModal from './BaseModal.vue'
import type { FeedFolder } from '../types/feed'

const props = defineProps<{ folder: FeedFolder }>()
const emit = defineEmits<{ close: []; save: [name: string]; delete: [] }>()

const name = ref(props.folder.name)
const inputRef = ref<HTMLInputElement | null>(null)

const deleteNote = computed(() => {
  const count = props.folder.feeds.length
  if (count === 0) return 'This folder is empty, so only the group itself goes away.'
  return `The ${count} ${count === 1 ? 'feed' : 'feeds'} inside move to the top level — no feed is removed.`
})

function submit(): void {
  const trimmed = name.value.trim()
  if (trimmed) emit('save', trimmed)
}

// A folder opened straight after creation still carries the placeholder name,
// so the field opens selected and typing replaces it.
onMounted(async () => {
  await nextTick()
  inputRef.value?.focus()
  inputRef.value?.select()
})
</script>

<template>
  <BaseModal title="Edit folder" :icon="IconFolder" testid="folder-edit-modal" @close="emit('close')">
    <div class="flex flex-col gap-4 px-5 py-4">
      <div>
        <label for="folder-edit-name" class="text-[12.5px] text-text-3">Folder name</label>
        <input
          id="folder-edit-name"
          ref="inputRef"
          v-model="name"
          type="text"
          class="mt-2 w-full rounded-lg border border-strong bg-app px-3.5 py-2.5 text-[13.5px] text-text outline-none placeholder:text-text-4 focus:border-accent"
          data-testid="folder-edit-name"
          @keydown.enter="submit"
        >
      </div>
      <div class="rounded-lg border border-severity-error/35 bg-raised p-4">
        <div class="text-[13.5px] font-semibold text-text">Delete folder</div>
        <p class="mt-1.5 text-xs leading-relaxed text-text-3">{{ deleteNote }}</p>
        <BaseButton
          variant="danger"
          size="sm"
          class="mt-3.5"
          data-testid="folder-edit-delete"
          @click="emit('delete')"
        ><template #icon><IconTrash2 class="size-3.5" /></template>Delete folder</BaseButton>
      </div>
    </div>
    <template #footer>
      <BaseButton
        class="flex-1"
        :disabled="!name.trim()"
        data-testid="folder-edit-save"
        @click="submit"
      >Save ↵</BaseButton>
      <BaseButton variant="secondary" data-testid="folder-edit-cancel" @click="emit('close')">Cancel</BaseButton>
    </template>
  </BaseModal>
</template>
