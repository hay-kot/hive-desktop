import { ref, type Ref } from 'vue'
import { ListSessions } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'
import { repoDisplayName } from '../lib/repositories'

/**
 * One row of the terminal sidebar. Slug is the tmux target an attach uses; a
 * session with no tmux session yet is offered a start rather than failing, so
 * no row is dead. Only an `active` session can be started or attached to —
 * recycled and corrupted ones are listed so they can be read and deleted.
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
      group = { key: row.repo, name: groupDisplayName(row.repo), sessions: [] }
      groups.set(row.repo, group)
    }
    group.sessions.push(row)
  }
  const sorted = [...groups.values()].sort((a, b) => a.name.localeCompare(b.name))
  for (const group of sorted) group.sessions.sort((a, b) => a.name.localeCompare(b.name))
  return sorted
}

function groupDisplayName(remote: string): string {
  return repoDisplayName(remote) || '(no remote)'
}

// Module singletons: the rows are hive's session set, not one view's, and the
// tree renders the last-known ones while reload() revalidates rather than
// emptying and popping back in.
const sessions = ref<TerminalSessionRow[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
// Whether a reload has ever finished. `loading` cannot answer that — it is
// false both before the first one starts and after it lands — and the tree
// needs the difference to tell an empty list from one it has not read yet.
const loaded = ref(false)

async function reload(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    sessions.value = (await ListSessions()) ?? []
  } catch (e) {
    // Keep the last-good rows: a failed revalidation reports itself without
    // collapsing the tree it could not refresh.
    error.value = e instanceof Error && e.message ? e.message : 'Could not list sessions.'
  } finally {
    loading.value = false
    loaded.value = true
  }
}

/**
 * The remote of the session at `slug`, or '' when there is no such session or
 * it has none. This is what makes "new session" default to the repository you
 * are already working in rather than to the first workspace on disk.
 */
export function sessionRepository(slug: string): string {
  if (!slug) return ''
  return sessions.value.find((row) => row.slug === slug)?.repo ?? ''
}

export function useTerminalSessions(): {
  sessions: Ref<TerminalSessionRow[]>
  loading: Ref<boolean>
  loaded: Ref<boolean>
  error: Ref<string | null>
  reload: () => Promise<void>
} {
  return { sessions, loading, loaded, error, reload }
}

export function resetTerminalSessionsForTests(): void {
  sessions.value = []
  loading.value = false
  loaded.value = false
  error.value = null
}
