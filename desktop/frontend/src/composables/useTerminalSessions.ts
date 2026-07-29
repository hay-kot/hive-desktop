import { ref, type Ref } from 'vue'
import { ListSessions } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'

/** One row of the terminal picker. Slug is the tmux target an attach uses. */
export interface TerminalSessionRow {
  id: string
  name: string
  slug: string
  repo: string
  state: string
}

export function useTerminalSessions(): {
  sessions: Ref<TerminalSessionRow[]>
  loading: Ref<boolean>
  error: Ref<string | null>
  reload: () => Promise<void>
} {
  const sessions = ref<TerminalSessionRow[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function reload(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      sessions.value = (await ListSessions()) ?? []
    } catch (e) {
      error.value = e instanceof Error && e.message ? e.message : 'Could not list sessions.'
      sessions.value = []
    } finally {
      loading.value = false
    }
  }

  return { sessions, loading, error, reload }
}
