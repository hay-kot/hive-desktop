import { ref } from 'vue'
import { CreateSession, SessionLaunchOptions } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'
import { NewSessionDraft } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/pipelineservice'
import type { SessionLaunchOptions as SessionLaunchOptionsView } from '../../bindings/github.com/hay-kot/hive-desktop/internal/app/dispatch/models'
import type { InboxItem } from '../types/feed'
import { useToasts } from './useToasts'

interface Draft { repository: string; name: string; prompt: string }

// Module-scoped so the global command, the per-item menu, and the mounted-once
// dialog all drive the same form (mirrors useReportDialog).
const open = ref(false)
const loading = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)
const options = ref<SessionLaunchOptionsView | null>(null)
const initial = ref<Draft>({ repository: '', name: '', prompt: '' })

// The item the open form was drafted from, so the session it creates is
// recorded against that item. 0 for a blank form. It is not a form field: the
// backend resolves the item's identity from this id and never takes it from
// the client, and the user cannot retarget a draft at a different item.
const itemID = ref(0)

// SessionLaunchOptions scans every configured workspace directory (a git call
// per repo), slow enough that a click blocked on it visibly lags. The last
// result opens the dialog instantly; the refresh lands behind it.
let cachedOptions: SessionLaunchOptionsView | null = null

async function fetchOptions(): Promise<SessionLaunchOptionsView> {
  const opts = await SessionLaunchOptions()
  cachedOptions = opts
  return opts
}

function resolveOptions(): Promise<SessionLaunchOptionsView> {
  if (!cachedOptions) return fetchOptions()
  void fetchOptions().then((opts) => { if (open.value) options.value = opts }).catch(() => {})
  return Promise.resolve(cachedOptions)
}

export function resetNewSessionForTests(): void {
  cachedOptions = null
  open.value = false
  loading.value = false
  busy.value = false
  error.value = null
  options.value = null
  itemID.value = 0
}

function message(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback
}

export function useNewSession() {
  const { showToast } = useToasts()

  function prefetch(): void {
    if (!cachedOptions) void fetchOptions().catch(() => {})
  }

  // `preferred` is the repository of whatever session is on screen. Starting a
  // second session on the repo you are already in is the common case, and the
  // backend default — the first configured workspace — is almost never it.
  async function openBlank(preferred = ''): Promise<void> {
    if (open.value || loading.value) return
    error.value = null
    loading.value = true
    try {
      const opts = await resolveOptions()
      options.value = opts
      initial.value = { repository: preferred || opts.defaultRepository || '', name: '', prompt: '' }
      itemID.value = 0
      open.value = true
    } catch (e) {
      showToast(message(e, 'Could not load session options.'), { severity: 'error' })
    } finally {
      loading.value = false
    }
  }

  async function openFromItem(item: InboxItem): Promise<void> {
    if (open.value || loading.value) return
    error.value = null
    loading.value = true
    try {
      const [opts, draft] = await Promise.all([resolveOptions(), NewSessionDraft(item.id)])
      options.value = opts
      initial.value = {
        repository: draft.repository || opts.defaultRepository || '',
        name: draft.name,
        prompt: draft.prompt,
      }
      itemID.value = item.id
      open.value = true
    } catch (e) {
      showToast(message(e, 'Could not prepare the session.'), { severity: 'error' })
    } finally {
      loading.value = false
    }
  }

  function cancel(): void {
    if (busy.value) return
    open.value = false
    options.value = null
    error.value = null
    itemID.value = 0
  }

  async function submit(input: { repository: string; name: string; prompt: string; agent?: string }): Promise<void> {
    if (busy.value) return
    busy.value = true
    error.value = null
    try {
      // Creation (including any clone) runs as a background job; its outcome
      // shows in the titlebar jobs chip. Only validation errors reject here.
      await CreateSession({ repository: input.repository, name: input.name, prompt: input.prompt, agent: input.agent ?? '', itemId: itemID.value })
      showToast(`Creating session ${input.name}…`, { severity: 'info' })
      open.value = false
      options.value = null
      itemID.value = 0
    } catch (e) {
      error.value = message(e, 'Could not start the session.')
    } finally {
      busy.value = false
    }
  }

  return { open, options, initial, busy, loading, error, prefetch, openBlank, openFromItem, cancel, submit }
}
