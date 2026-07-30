import { ref, type Ref } from 'vue'
import { ListSessions } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'

/**
 * One row of the terminal sidebar. Slug is the tmux target an attach uses, and
 * only an `active` session has one — recycled and corrupted sessions are listed
 * so they can be read and deleted, not attached to.
 */
export interface TerminalSessionRow {
  id: string
  name: string
  slug: string
  repo: string
  state: string
}

/** One repo group of the sidebar tree, keyed by the session's remote. */
export interface TerminalSessionGroup {
  key: string
  name: string
  sessions: TerminalSessionRow[]
}

// Mirrors the TUI's GroupSessionsByRepo: one group per remote, a "(no remote)"
// group for sessions without one, groups and sessions alphabetical.
export function groupTerminalSessions(rows: TerminalSessionRow[]): TerminalSessionGroup[] {
  const groups = new Map<string, TerminalSessionGroup>()
  for (const row of rows) {
    let group = groups.get(row.repo)
    if (!group) {
      group = { key: row.repo, name: repoDisplayName(row.repo), sessions: [] }
      groups.set(row.repo, group)
    }
    group.sessions.push(row)
  }
  const sorted = [...groups.values()].sort((a, b) => a.name.localeCompare(b.name))
  for (const group of sorted) group.sessions.sort((a, b) => a.name.localeCompare(b.name))
  return sorted
}

// "git@github.com:owner/name.git" and "https://github.com/owner/name.git"
// both read as "owner/name"; a bare path keeps its last two segments.
function repoDisplayName(remote: string): string {
  if (!remote) return '(no remote)'
  let path = remote.replace(/\.git\/*$/, '').replace(/\/+$/, '')
  const protocol = path.indexOf('://')
  if (protocol !== -1) {
    path = path.slice(protocol + 3)
    const host = path.indexOf('/')
    if (host !== -1) path = path.slice(host + 1)
  } else {
    const colon = path.indexOf(':')
    if (colon !== -1 && path.slice(0, colon).includes('@')) path = path.slice(colon + 1)
  }
  const segments = path.split('/').filter(Boolean)
  if (!segments.length) return remote
  return segments.slice(-2).join('/')
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
