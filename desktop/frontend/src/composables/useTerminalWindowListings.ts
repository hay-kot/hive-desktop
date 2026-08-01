import { ref, type Ref } from 'vue'
import type { TerminalClient, WindowState } from '../lib/terminalClient'
import type { TerminalSessionRow } from './useTerminalSessions'

// The passive per-session window listings behind "Always show windows", keyed
// by slug. A module singleton for the same reason as useTerminalSessions, and
// it also stands in for a session whose attach is still in flight, so switching
// sessions does not collapse and rebuild its subtree and tab strip.
const listings = ref<Record<string, WindowState[]>>({})

// A sweep costs one round trip per session, and an unattached session answers
// it by spawning tmux twice. The triggers arrive in bursts — a switch moves
// the route, the pool and the session list inside a few ticks — so sweeps are
// coalesced rather than run per trigger: overlapping calls collapse into a
// single follow-up carrying the newest rows. Uncoalesced, one switch cost tens
// of sweeps, and the attach it raced queued behind every one of them.
let running: Promise<void> | null = null
let next: { client: TerminalClient; rows: TerminalSessionRow[] } | null = null

function refresh(client: TerminalClient, rows: TerminalSessionRow[]): Promise<void> {
  if (running) {
    next = { client, rows }
    return running
  }
  running = drain(client, rows)
  return running
}

async function drain(client: TerminalClient, rows: TerminalSessionRow[]): Promise<void> {
  let current: { client: TerminalClient; rows: TerminalSessionRow[] } | null = { client, rows }
  try {
    while (current) {
      await sweep(current.client, current.rows)
      current = next
      next = null
    }
  } finally {
    running = null
    next = null
  }
}

// Replaces the whole map so sessions that no longer exist fall out; a write
// landing after the option was toggled off is harmless because rendering is
// gated on the option, not on this cache.
async function sweep(client: TerminalClient, rows: TerminalSessionRow[]): Promise<void> {
  const entries = await Promise.all(rows.map(async (row) => {
    try {
      const { windows } = await client.listWindows(row.slug)
      return [row.slug, windows] as const
    } catch {
      // One session's listing failing must not blank the others' rows.
      return [row.slug, [] as WindowState[]] as const
    }
  }))
  listings.value = Object.fromEntries(entries)
}

export function useTerminalWindowListings(): {
  listings: Ref<Record<string, WindowState[]>>
  refresh: (client: TerminalClient, rows: TerminalSessionRow[]) => Promise<void>
} {
  return { listings, refresh }
}

export function resetTerminalWindowListingsForTests(): void {
  listings.value = {}
  running = null
  next = null
}
