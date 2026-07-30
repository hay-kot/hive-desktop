import { ref, type Ref } from 'vue'
import type { TerminalClient, WindowState } from '../lib/terminalClient'
import type { TerminalSessionRow } from './useTerminalSessions'

// The passive per-session window listings behind "Always show windows", keyed
// by slug. A module singleton for the same reason as useTerminalSessions: the
// cached tree renders immediately on re-entry, and refresh() revalidates. It
// also stands in for a session whose attach is still in flight, so switching
// sessions does not collapse and rebuild its subtree and tab strip.
const listings = ref<Record<string, WindowState[]>>({})

// Replaces the whole map so sessions that no longer exist fall out; a write
// landing after the option was toggled off is harmless because rendering is
// gated on the option, not on this cache.
async function refresh(client: TerminalClient, rows: TerminalSessionRow[]): Promise<void> {
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
}
