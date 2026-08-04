import { ref, type Ref } from 'vue'
import { ListSessions } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice'
import { Scratch } from '../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/terminalservice'
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

/**
 * One group of the sidebar tree, keyed by the session's remote — or the pinned
 * scratch section, which is `pinned` and belongs to no repository.
 */
export interface TerminalSessionGroup {
  key: string
  name: string
  sessions: TerminalSessionRow[]
  pinned?: boolean
}

// The pinned section's key. It draws no header of its own — its one row is the
// heading — so the name is only what the sidebar filter matches on.
const SCRATCH_GROUP_KEY = 'scratch'

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

/**
 * The tree's groups: the scratch terminal's own section first, then one per
 * repository. It is a group of one rather than a loose row so the tree stays
 * group → session → window everywhere, and it is prepended rather than sorted
 * in because pinned is the point — it must not move as repositories come and go.
 * The section draws no header: its row is the heading, and what is listed under
 * it are the tabs.
 */
export function terminalSessionGroups(
  rows: TerminalSessionRow[],
  scratch: TerminalSessionRow | null,
): TerminalSessionGroup[] {
  const repos = groupTerminalSessions(rows)
  if (!scratch) return repos
  return [{ key: SCRATCH_GROUP_KEY, name: scratch.name, sessions: [scratch], pinned: true }, ...repos]
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

// The scratch terminal as a row of the same shape, so everything keyed on a
// session — the pool, the window listings, the keyboard walk — reaches it
// without learning a second kind of row. It carries no repo and its slug is its
// id, which no hive session id can collide with. Declared by the core rather
// than assumed here, because the slug is what an attach addresses.
const scratch = ref<TerminalSessionRow | null>(null)

async function reload(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const [rows] = await Promise.all([ListSessions(), loadScratch()])
    sessions.value = rows ?? []
  } catch (e) {
    // Keep the last-good rows: a failed revalidation reports itself without
    // collapsing the tree it could not refresh.
    error.value = e instanceof Error && e.message ? e.message : 'Could not list sessions.'
  } finally {
    loading.value = false
    loaded.value = true
  }
}

// Read once: it is a constant for the run. A failure leaves the tree without its
// scratch section rather than without its sessions.
async function loadScratch(): Promise<void> {
  if (scratch.value) return
  try {
    const declared = await Scratch()
    if (!declared?.slug) return
    scratch.value = { id: declared.slug, name: declared.name, slug: declared.slug, repo: '', state: 'active' }
  } catch {
    // Terminal mode reports its own unavailability; a missing scratch row is
    // not worth failing the session list over.
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
  scratch: Ref<TerminalSessionRow | null>
  loading: Ref<boolean>
  loaded: Ref<boolean>
  error: Ref<string | null>
  reload: () => Promise<void>
} {
  return { sessions, scratch, loading, loaded, error, reload }
}

export function resetTerminalSessionsForTests(): void {
  sessions.value = []
  scratch.value = null
  loading.value = false
  loaded.value = false
  error.value = null
}
