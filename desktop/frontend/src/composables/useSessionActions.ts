import { ref } from 'vue'
import {
  DeleteSession,
  PruneSessions,
  RecycleSession,
  RenameSession,
  SessionDetail,
  SessionRisk,
  SetSessionGroup,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'
import type {
  SessionDetail as SessionDetailView,
  SessionRisk as SessionRiskView,
  SessionSummary,
} from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'
import { appErrorMessage } from '../lib/appError'
import { useConfirmation } from './useConfirmation'
import { useToasts } from './useToasts'

/** A pending single-field edit — a rename or a group change — and how to apply it. */
export interface SessionEdit {
  title: string
  label: string
  hint: string
  value: string
  confirmLabel: string
  allowEmpty: boolean
  testid: string
  apply: (value: string) => Promise<void>
}

export interface SessionActionOptions {
  /**
   * Called after a change the session list has to re-read. A rename is one of
   * them, and re-reading is the whole mechanism: the renamed session comes back
   * with a new slug, which is what the host follows to re-attach.
   */
  onChanged?: () => void
}

function message(error: unknown, fallback: string): string {
  return appErrorMessage(error) || (error instanceof Error && error.message ? error.message : fallback)
}

/**
 * Names the work a destructive operation would discard. Hazard 2 of #144: a
 * generic "are you sure" hides the only thing worth confirming, which is what
 * this session in particular is holding.
 */
export function riskDescription(name: string, operation: 'delete' | 'recycle', risk: SessionRiskView): string {
  const consequence = operation === 'delete'
    ? `Deleting ${name} removes its directory and kills its terminal session.`
    : risk.recycleDeletes
      ? `Recycling ${name} deletes it — it is a git worktree, so there is no clone of its own to reset.`
      : `Recycling ${name} resets its clone to a clean checkout of the default branch.`

  let held = 'Git reports no uncommitted changes and no unpushed commits.'
  if (risk.uncommittedChanges && risk.unpushedCommits) held = 'It has uncommitted changes and unpushed commits, and both are lost.'
  else if (risk.uncommittedChanges) held = 'It has uncommitted changes, and they are lost.'
  else if (risk.unpushedCommits) held = 'It has unpushed commits, and they are lost.'

  return `${consequence} ${held}`
}

/**
 * The session row's operations: read, rename, group, recycle, delete, and the
 * list-wide prune. Destructive ones are gated on a risk pre-flight and run as
 * background jobs, so what they report is that the job started — `jobs:updated`
 * is what tells the list to re-read.
 */
export function useSessionActions(options: SessionActionOptions = {}) {
  const { showToast } = useToasts()
  const confirmation = useConfirmation()

  const detail = ref<SessionDetailView | null>(null)
  const detailLoading = ref(false)
  const edit = ref<SessionEdit | null>(null)
  const editBusy = ref(false)
  const editError = ref<string | null>(null)

  async function openDetail(session: SessionSummary): Promise<void> {
    detailLoading.value = true
    try {
      detail.value = await SessionDetail(session.id)
    } catch (e) {
      showToast(message(e, 'Could not read that session.'), { severity: 'error' })
    } finally {
      detailLoading.value = false
    }
  }

  function closeDetail(): void {
    detail.value = null
  }

  function requestRename(session: SessionSummary): void {
    edit.value = {
      title: 'Rename session',
      label: 'Session name',
      hint: 'Renaming also renames its terminal session, so an open terminal reconnects.',
      value: session.name,
      confirmLabel: 'Rename',
      allowEmpty: false,
      testid: 'session-rename',
      apply: async (name) => {
        await RenameSession(session.id, name)
        options.onChanged?.()
      },
    }
  }

  function requestGroup(session: SessionSummary): void {
    edit.value = {
      title: 'Set group',
      label: 'Group',
      hint: 'Leave empty to remove this session from its group.',
      value: session.group,
      confirmLabel: 'Save',
      allowEmpty: true,
      testid: 'session-group',
      apply: async (group) => {
        await SetSessionGroup(session.id, group)
        options.onChanged?.()
      },
    }
  }

  function cancelEdit(): void {
    if (editBusy.value) return
    edit.value = null
    editError.value = null
  }

  // A failed edit keeps the dialog open with its error — the value is almost
  // always nearly right (a name hive rejects, a slug already taken).
  async function submitEdit(value: string): Promise<void> {
    const pending = edit.value
    if (!pending || editBusy.value) return
    editBusy.value = true
    editError.value = null
    try {
      await pending.apply(value.trim())
      edit.value = null
    } catch (e) {
      editError.value = message(e, 'Could not save that.')
    } finally {
      editBusy.value = false
    }
  }

  async function requestDestructive(session: SessionSummary, operation: 'delete' | 'recycle'): Promise<void> {
    let risk: SessionRiskView
    try {
      risk = await SessionRisk(session.id)
    } catch (e) {
      // Without the pre-flight there is nothing specific to confirm against,
      // and confirming against nothing is what hazard 2 forbids.
      showToast(message(e, 'Could not check that session for unsaved work.'), { severity: 'error' })
      return
    }
    confirmation.request({
      title: operation === 'delete' ? 'Delete this session?' : 'Recycle this session?',
      description: riskDescription(session.name, operation, risk),
      confirmLabel: operation === 'delete' ? 'Delete' : 'Recycle',
      onConfirm: async () => {
        await (operation === 'delete' ? DeleteSession(session.id) : RecycleSession(session.id))
        showToast(operation === 'delete' ? `Deleting ${session.name}…` : `Recycling ${session.name}…`, { severity: 'info' })
      },
    })
  }

  function requestDelete(session: SessionSummary): Promise<void> {
    return requestDestructive(session, 'delete')
  }

  function requestRecycle(session: SessionSummary): Promise<void> {
    return requestDestructive(session, 'recycle')
  }

  /** count is the number of prunable (recycled or corrupted) sessions listed. */
  function requestPrune(count: number): void {
    confirmation.request({
      title: 'Prune sessions?',
      description: count === 1
        ? 'The one recycled or corrupted session is deleted, along with its directory.'
        : `All ${count} recycled and corrupted sessions are deleted, along with their directories.`,
      confirmLabel: 'Prune',
      onConfirm: async () => {
        await PruneSessions()
        showToast('Pruning sessions…', { severity: 'info' })
      },
    })
  }

  return {
    confirmation,
    detail,
    detailLoading,
    openDetail,
    closeDetail,
    edit,
    editBusy,
    editError,
    requestRename,
    requestGroup,
    cancelEdit,
    submitEdit,
    requestDelete,
    requestRecycle,
    requestPrune,
  }
}
