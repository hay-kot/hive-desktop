<script setup lang="ts">
// Detail pane for the selected task. Self-contained via useTasks() (the same
// module singleton TasksView reads) rather than props/emit — it owns
// everything about the selection: identity, the status control and its
// confirms (cancel; epic → done/cancelled cascade), rendered description and
// comments, blockers, and delete. TasksView owns the list/tree only.
import { computed, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import IconBookmarkCheck from '~icons/lucide/bookmark-check'
import IconCheck from '~icons/lucide/check'
import IconCopy from '~icons/lucide/copy'
import IconLink2 from '~icons/lucide/link-2'
import IconTrash2 from '~icons/lucide/trash-2'
import AppSelect, { type AppSelectOption } from './AppSelect.vue'
import BaseBadge from './BaseBadge.vue'
import ConfirmationDialog from './ConfirmationDialog.vue'
import PanelResizeHandle from './PanelResizeHandle.vue'
import { useClipboard } from '../composables/useClipboard'
import { useResizablePanel } from '../composables/useResizablePanel'
import { useTasks } from '../composables/useTasks'
import { errorText } from '../lib/appError'
import { relativeAge } from '../lib/age'
import { renderGithubMarkdown } from '../lib/githubMarkdown'
import { cascadeCount, checkpointBody, isCheckpoint, statusMeta } from '../lib/tasksPresentation'
import type { TaskComment } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'

const { detail, items, setStatus, remove } = useTasks()

const STATUS_OPTIONS: AppSelectOption[] = [
  { value: 'open', label: 'Open' },
  { value: 'in_progress', label: 'In Progress' },
  { value: 'done', label: 'Done' },
  { value: 'cancelled', label: 'Cancelled' },
]

const typeLabel = computed(() => (detail.value?.type === 'epic' ? 'Epic' : 'Task'))
const bodyHtml = computed(() => (detail.value?.desc ? renderGithubMarkdown(detail.value.desc) : ''))

function agoLabel(timestamp: number): string {
  const label = relativeAge(timestamp)
  return label === 'now' ? 'now' : `${label} ago`
}

function commentHtml(comment: TaskComment): string {
  return renderGithubMarkdown(checkpointBody(comment))
}

// Rendered bodies are untrusted markdown; links must open in the user's real
// browser rather than navigate the webview away from the app (matches
// DetailPane.vue's onBodyClick).
function onBodyClick(event: MouseEvent): void {
  const anchor = (event.target as HTMLElement).closest('a')
  if (!anchor) return
  event.preventDefault()
  const href = anchor.getAttribute('href') ?? ''
  if (/^(https?:|mailto:)/i.test(href)) void Browser.OpenURL(href)
}

// ── Copy ID ──────────────────────────────────────────────────────────────
const { copy: copyText, copied: idCopied } = useClipboard()
function copyId(): void {
  if (detail.value) void copyText(detail.value.id)
}

// ── Status control ──────────────────────────────────────────────────────
// Cancelling always confirms. An epic moving to done/cancelled with open
// descendants confirms with the cascade count instead — that copy already
// implies the cancellation, so it supersedes the plain cancel confirm.
const pendingStatus = ref<string | null>(null)
const cascadeConfirmCount = ref(0)
const cascadeConfirmOpen = ref(false)
const cancelConfirmOpen = ref(false)
const statusBusy = ref(false)
const statusError = ref<string | null>(null)

function requestStatusChange(next: string): void {
  const current = detail.value
  if (!current) return
  const cascade = current.type === 'epic' && (next === 'done' || next === 'cancelled')
    ? cascadeCount(items.value, current.id)
    : 0
  if (cascade > 0) {
    pendingStatus.value = next
    cascadeConfirmCount.value = cascade
    cascadeConfirmOpen.value = true
    return
  }
  if (next === 'cancelled') {
    pendingStatus.value = next
    cancelConfirmOpen.value = true
    return
  }
  void applyStatus(next)
}

async function applyStatus(status: string): Promise<void> {
  const id = detail.value?.id
  if (!id) return
  statusBusy.value = true
  statusError.value = null
  try {
    await setStatus(id, status)
    cascadeConfirmOpen.value = false
    cancelConfirmOpen.value = false
    pendingStatus.value = null
  } catch (err) {
    statusError.value = errorText(err, 'Could not update status.')
  } finally {
    statusBusy.value = false
  }
}

function confirmPendingStatus(): void {
  if (pendingStatus.value) void applyStatus(pendingStatus.value)
}

function cancelPendingStatus(): void {
  cascadeConfirmOpen.value = false
  cancelConfirmOpen.value = false
  pendingStatus.value = null
  statusError.value = null
}

const cascadeDescription = computed(() => {
  const label = statusMeta(pendingStatus.value ?? '').label.toLowerCase()
  const count = cascadeConfirmCount.value
  return `Marking this epic ${label} also closes ${count} open task${count === 1 ? '' : 's'} nested under it.`
})

// ── Delete ───────────────────────────────────────────────────────────────
const deleteConfirmOpen = ref(false)
const deleteBusy = ref(false)
const deleteError = ref<string | null>(null)

async function confirmDelete(): Promise<void> {
  const id = detail.value?.id
  if (!id) return
  deleteBusy.value = true
  deleteError.value = null
  try {
    await remove(id)
    deleteConfirmOpen.value = false
  } catch (err) {
    deleteError.value = errorText(err, 'Could not delete task.')
  } finally {
    deleteBusy.value = false
  }
}

// DetailPane.vue's precedent: docked right, handle on the left edge, width
// persisted separately from the tree pane it sits beside.
const { size: paneWidth, startResize, step } = useResizablePanel({
  storageKey: 'hive.panel.taskdetail',
  defaultSize: 560,
  min: 320,
  max: 1000,
  edge: 'left',
})
</script>

<template>
  <aside class="hive-scroll relative flex shrink-0 flex-col overflow-y-auto bg-pane" :style="{ width: paneWidth + 'px' }" data-testid="task-detail-pane">
    <PanelResizeHandle edge="left" name="taskdetail" :start="startResize" :step="step" />

    <template v-if="detail">
      <div class="border-b border-border px-5 pb-4 pt-[18px]">
        <div class="flex items-start justify-between gap-3">
          <h1 class="min-w-0 flex-1 text-[16.5px] font-semibold leading-[1.3] tracking-[-.01em]" data-testid="task-detail-title">{{ detail.title }}</h1>
          <button
            type="button"
            class="flex shrink-0 items-center gap-1.5 rounded border border-card px-2 py-1 font-mono text-[10.5px] text-text-3 hover:border-strong hover:text-text"
            data-testid="task-detail-copy-id"
            @click="copyId"
          >
            <span class="max-w-[110px] truncate">{{ detail.id }}</span>
            <component :is="idCopied ? IconCheck : IconCopy" class="size-3 shrink-0" />
          </button>
        </div>

        <div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-text-3" data-testid="task-detail-meta">
          <span>{{ detail.repoKey }}</span>
          <span>·</span>
          <span>{{ typeLabel }}</span>
          <span>·</span>
          <span>Created {{ agoLabel(Date.parse(detail.createdAt)) }}</span>
          <span>·</span>
          <span>Updated {{ agoLabel(Date.parse(detail.updatedAt)) }}</span>
        </div>

        <div class="mt-3.5 max-w-[220px]">
          <AppSelect
            :model-value="detail.status"
            :options="STATUS_OPTIONS"
            size="sm"
            aria-label="Status"
            testid="task-status-select"
            @update:model-value="requestStatusChange"
          />
          <p v-if="statusError && !cascadeConfirmOpen && !cancelConfirmOpen" class="mt-1.5 text-[11.5px] text-severity-error" data-testid="task-status-error">{{ statusError }}</p>
        </div>

        <div v-if="bodyHtml" class="markdown-body mt-3.5 text-[13.5px] leading-[1.65] text-text-2" data-testid="task-detail-body" @click="onBodyClick" v-html="bodyHtml" />
      </div>

      <div class="px-5 pb-5 pt-4">
        <section v-if="(detail.blockers ?? []).length" data-testid="task-blockers">
          <h2 class="mb-2 font-mono text-[10.5px] tracking-[.12em] text-text-3">BLOCKERS</h2>
          <div class="flex flex-wrap gap-1.5">
            <span
              v-for="blocker in detail.blockers ?? []"
              :key="blocker.id"
              class="inline-flex items-center gap-1.5 rounded-[5px] border border-card px-2 py-1 text-[11.5px]"
              :class="blocker.title ? 'text-text-2' : 'italic text-text-4'"
              data-testid="task-blocker-chip"
            ><IconLink2 class="size-3 shrink-0 text-text-4" />{{ blocker.title || blocker.id }}</span>
          </div>
        </section>

        <section v-if="(detail.comments ?? []).length" class="mt-5 border-t border-border pt-4" data-testid="task-comments">
          <h2 class="mb-3 font-mono text-[10.5px] tracking-[.12em] text-text-3">COMMENTS</h2>
          <div class="flex flex-col gap-3.5">
            <div v-for="comment in detail.comments ?? []" :key="comment.id" data-testid="task-comment">
              <div class="mb-1 flex items-center gap-2">
                <BaseBadge v-if="isCheckpoint(comment)" tone="accent" class="px-2 py-0.5 text-[10px] font-semibold" data-testid="task-comment-checkpoint">
                  <IconBookmarkCheck class="size-3" />CHECKPOINT
                </BaseBadge>
                <span class="font-mono text-[10.5px] text-text-4">{{ agoLabel(Date.parse(comment.createdAt)) }}</span>
              </div>
              <div class="markdown-body text-[13px] leading-[1.6] text-text-2" @click="onBodyClick" v-html="commentHtml(comment)" />
            </div>
          </div>
        </section>

        <div class="mt-6 border-t border-border pt-4">
          <button
            type="button"
            class="flex items-center gap-1.5 rounded-lg border border-severity-error/40 px-3 py-1.5 text-[12px] font-medium text-severity-error hover:bg-severity-error-tint"
            data-testid="task-delete"
            @click="deleteConfirmOpen = true"
          ><IconTrash2 class="size-3.5" />Delete</button>
        </div>
      </div>
    </template>
    <div v-else class="m-auto font-mono text-xs text-text-4" data-testid="task-detail-empty">Select a task to inspect</div>

    <ConfirmationDialog
      v-if="cascadeConfirmOpen"
      title="Close nested tasks?"
      :description="cascadeDescription"
      confirm-label="Confirm"
      :busy="statusBusy"
      :error="statusError"
      testid="task-cascade-confirm"
      @confirm="confirmPendingStatus"
      @cancel="cancelPendingStatus"
    />
    <ConfirmationDialog
      v-if="cancelConfirmOpen"
      title="Cancel this task?"
      description="This marks the task cancelled. It stays in the tree but drops out of the open filters."
      confirm-label="Cancel task"
      :busy="statusBusy"
      :error="statusError"
      testid="task-cancel-confirm"
      @confirm="confirmPendingStatus"
      @cancel="cancelPendingStatus"
    />
    <ConfirmationDialog
      v-if="deleteConfirmOpen"
      title="Delete task"
      description="Deletes this item and everything nested under it, comments included. This cannot be undone."
      confirm-label="Delete"
      :busy="deleteBusy"
      :error="deleteError"
      testid="task-delete-confirm"
      @confirm="confirmDelete"
      @cancel="deleteConfirmOpen = false"
    />
  </aside>
</template>

<style scoped>
/* Rendered task description / comment bodies (GitHub-flavored markdown).
   Matches DetailPane.vue's precedent so the two markdown surfaces read the
   same. */
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
.markdown-body :deep(summary) { cursor: pointer; color: var(--color-text); font-weight: 650; }
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
