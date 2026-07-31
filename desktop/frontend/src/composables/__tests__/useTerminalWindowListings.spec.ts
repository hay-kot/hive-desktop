import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { resetTerminalWindowListingsForTests, useTerminalWindowListings } from '../useTerminalWindowListings'
import type { TerminalClient } from '../../lib/terminalClient'
import type { TerminalSessionRow } from '../useTerminalSessions'

function row(slug: string): TerminalSessionRow {
  return { id: slug, name: slug, slug, repo: 'owner/repo', state: 'active' }
}

// One deferred listing per call, so a sweep can be held open while more
// triggers arrive.
function heldClient() {
  const releases: (() => void)[] = []
  const listWindows = vi.fn((slug: string) => new Promise((resolve) => {
    releases.push(() => resolve({ windows: [{ windowId: `@${slug}`, name: slug, active: true, width: 80, height: 24 }] }))
  }))
  return {
    listWindows,
    releaseAll: () => {
      const pending = releases.splice(0)
      for (const release of pending) release()
    },
    client: { listWindows } as unknown as TerminalClient,
  }
}

describe('useTerminalWindowListings', () => {
  beforeEach(() => resetTerminalWindowListingsForTests())

  // A switch moves the route, the pool and the session list within a few
  // ticks. Sweeping per trigger cost tens of tmux spawns and queued the attach
  // behind all of them, so the burst has to collapse to one follow-up.
  it('collapses triggers arriving during a sweep into a single follow-up', async () => {
    const { listWindows, releaseAll, client } = heldClient()
    const { refresh } = useTerminalWindowListings()
    const rows = [row('one'), row('two')]

    void refresh(client, rows)
    expect(listWindows).toHaveBeenCalledTimes(2)

    // Three more triggers while the first sweep is still open.
    void refresh(client, rows)
    void refresh(client, rows)
    void refresh(client, rows)
    expect(listWindows).toHaveBeenCalledTimes(2)

    releaseAll()
    await flushPromises()

    // One follow-up sweep, not three.
    expect(listWindows).toHaveBeenCalledTimes(4)
  })

  it('publishes the sweep and lets a later one run', async () => {
    const { releaseAll, client, listWindows } = heldClient()
    const { listings, refresh } = useTerminalWindowListings()

    const first = refresh(client, [row('one')])
    releaseAll()
    await first

    expect(listings.value.one.map((window) => window.windowId)).toEqual(['@one'])

    const second = refresh(client, [row('two')])
    releaseAll()
    await second

    expect(listings.value).not.toHaveProperty('one')
    expect(listWindows).toHaveBeenCalledTimes(2)
  })
})
