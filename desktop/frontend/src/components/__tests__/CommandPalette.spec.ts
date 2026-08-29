import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import CommandPalette from '../CommandPalette.vue'
import { useCommandPalette, useCommands, useShellEscape, type Command } from '../../composables/useCommands'

const runBackend = vi.fn()
const runDesktop = vi.fn()
const runPersonal = vi.fn()
const runShell = vi.fn()

// Empty query sorts by group placement, registration order within a group:
// Feeds → desktop, backend; Profiles → personal.
const commands: Command[] = [
  { id: 'profile-personal', title: 'Switch to personal', group: 'Profiles', run: runPersonal },
  { id: 'feed-desktop', title: 'Open desktop feed', group: 'Feeds', run: runDesktop },
  { id: 'feed-backend', title: 'Open backend feed', group: 'Feeds', run: runBackend },
]

function mountPalette() {
  return mount(
    {
      components: { CommandPalette },
      template: '<CommandPalette />',
      setup() {
        useCommands(commands)
        useShellEscape((line) => [{ id: 'shell:run', title: `Run: ${line}`, run: () => runShell(line) }])
        return {}
      },
    },
    {
      attachTo: document.body,
      global: { stubs: { teleport: true } },
    },
  )
}

async function openPalette() {
  useCommandPalette().toggle()
  await flushPromises()
}

describe('CommandPalette', () => {
  let wrapper: ReturnType<typeof mountPalette> | undefined

  beforeEach(() => {
    vi.clearAllMocks()
    wrapper = mountPalette()
  })

  afterEach(() => {
    // Unconditional cleanup: unmounting disposes the useCommands registration
    // even when an assertion failed mid-test, and the module-scoped palette
    // state is reset by hand.
    wrapper?.unmount()
    wrapper = undefined
    const palette = useCommandPalette()
    palette.open.value = false
    palette.query.value = ''
    palette.scope.value = 'all'
  })

  function panel() {
    return wrapper!.find('[data-testid="command-palette"]')
  }

  // Row text is read from .palette-title: rows also contain an icon chip, an
  // optional hint, and the ↵ badge on the selected row.
  function rowTitle(row: ReturnType<typeof panel>) {
    return row.find('.palette-title').text()
  }

  function selectedRows() {
    return wrapper!.findAll('.palette-row').filter((row) => row.classes().includes('palette-row-selected'))
  }

  it('focuses the input on open and lists commands grouped under headers', async () => {
    await openPalette()

    expect(document.activeElement).toBe(wrapper!.find('[data-testid="command-palette-input"]').element)

    const entries = wrapper!.findAll('.palette-results > *').map((node) => ({
      header: node.classes().includes('palette-group-header'),
      text: node.classes().includes('palette-group-header') ? node.text() : rowTitle(node),
    }))
    expect(entries).toEqual([
      { header: true, text: 'Feeds' },
      { header: false, text: 'Open desktop feed' },
      { header: false, text: 'Open backend feed' },
      { header: true, text: 'Profiles' },
      { header: false, text: 'Switch to personal' },
    ])
  })

  it('moves the selection with ArrowDown/ArrowUp and wraps at both ends', async () => {
    await openPalette()
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Open desktop feed'])

    await panel().trigger('keydown', { key: 'ArrowDown' })
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Open backend feed'])

    await panel().trigger('keydown', { key: 'ArrowDown' })
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Switch to personal'])

    await panel().trigger('keydown', { key: 'ArrowDown' })
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Open desktop feed'])

    await panel().trigger('keydown', { key: 'ArrowUp' })
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Switch to personal'])
  })

  it('runs the selected command on Enter, closes the palette, and clears the query', async () => {
    const palette = useCommandPalette()
    await openPalette()
    palette.query.value = 'open'
    await flushPromises()

    await panel().trigger('keydown', { key: 'ArrowDown' })
    await panel().trigger('keydown', { key: 'Enter' })

    expect(runBackend).toHaveBeenCalledTimes(1)
    expect(runDesktop).not.toHaveBeenCalled()
    expect(palette.open.value).toBe(false)
    expect(palette.query.value).toBe('')
  })

  it('shows the empty state for a query with no results and ignores Enter', async () => {
    const palette = useCommandPalette()
    await openPalette()

    await wrapper!.find('[data-testid="command-palette-input"]').setValue('zzz')

    expect(wrapper!.findAll('.palette-row')).toHaveLength(0)
    expect(wrapper!.find('.palette-empty').text()).toContain('No results for "zzz"')

    await panel().trigger('keydown', { key: 'Enter' })

    expect(palette.open.value).toBe(true)
    expect(runBackend).not.toHaveBeenCalled()
    expect(runDesktop).not.toHaveBeenCalled()
    expect(runPersonal).not.toHaveBeenCalled()
  })

  it('closes on backdrop click but not on clicks inside the panel', async () => {
    const palette = useCommandPalette()
    await openPalette()

    await panel().trigger('click')
    expect(palette.open.value).toBe(true)

    await wrapper!.find('.palette-backdrop').trigger('click')
    expect(palette.open.value).toBe(false)
  })

  // The Code view's rows are rebuilt whenever the state they read changes, and
  // session statuses poll — so a selection that reset on every rebuild would
  // walk back to the top on its own, under the user's arrow keys.
  it('keeps the selected row when the results are rebuilt around it', async () => {
    wrapper!.unmount()
    const churn = ref(0)
    wrapper = mount(
      {
        components: { CommandPalette },
        template: '<CommandPalette />',
        setup() {
          useCommands(() => [
            { id: 'a', title: 'Alpha', group: 'Group', run: vi.fn() },
            { id: 'b', title: `Bravo ${churn.value}`, group: 'Group', run: vi.fn() },
            { id: 'c', title: 'Charlie', group: 'Group', run: vi.fn() },
          ])
          return {}
        },
      },
      { attachTo: document.body, global: { stubs: { teleport: true } } },
    )
    await openPalette()

    await panel().trigger('keydown', { key: 'ArrowDown' })
    await panel().trigger('keydown', { key: 'ArrowDown' })
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Charlie'])

    churn.value++
    await flushPromises()
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Charlie'])
  })

  // A row that goes away takes the selection with it rather than stranding it.
  it('falls back to the first row when the selected command disappears', async () => {
    wrapper!.unmount()
    const present = ref(true)
    wrapper = mount(
      {
        components: { CommandPalette },
        template: '<CommandPalette />',
        setup() {
          useCommands(() => present.value
            ? [{ id: 'a', title: 'Alpha', run: vi.fn() }, { id: 'b', title: 'Bravo', run: vi.fn() }]
            : [{ id: 'a', title: 'Alpha', run: vi.fn() }])
          return {}
        },
      },
      { attachTo: document.body, global: { stubs: { teleport: true } } },
    )
    await openPalette()

    await panel().trigger('keydown', { key: 'ArrowDown' })
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Bravo'])

    present.value = false
    await flushPromises()
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Alpha'])
  })

  it('resets the selection to the first row when the query changes', async () => {
    await openPalette()

    await panel().trigger('keydown', { key: 'ArrowDown' })
    await panel().trigger('keydown', { key: 'ArrowDown' })
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Switch to personal'])

    await wrapper!.find('[data-testid="command-palette-input"]').setValue('open')

    expect(wrapper!.findAll('.palette-row').map((row) => rowTitle(row))).toEqual([
      'Open desktop feed',
      'Open backend feed',
    ])
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Open desktop feed'])
  })

  it('highlights the matched substring in result titles, case-insensitively', async () => {
    await openPalette()

    await wrapper!.find('[data-testid="command-palette-input"]').setValue('open')

    expect(wrapper!.findAll('.palette-title-match').map((node) => node.text())).toEqual(['Open', 'Open'])
    expect(selectedRows().map((row) => rowTitle(row))).toEqual(['Open desktop feed'])
  })

  it('replaces headers with a per-row scope prefix while filtering', async () => {
    await openPalette()

    await wrapper!.find('[data-testid="command-palette-input"]').setValue('open')

    expect(wrapper!.findAll('.palette-group-header')).toHaveLength(0)
    expect(wrapper!.findAll('[data-testid="command-palette-command-scope"]').map((node) => node.text()))
      .toEqual(['Feeds ›', 'Feeds ›'])
  })

  function tabs() {
    return wrapper!.findAll('[data-testid="command-palette-tab"]')
  }

  it('renders a tab per visible scope with All active by default', async () => {
    await openPalette()

    expect(tabs().map((tab) => tab.attributes('data-scope'))).toEqual(['all', 'goto', 'actions', 'shell'])
    expect(tabs().filter((tab) => tab.classes().includes('palette-tab-active')).map((tab) => tab.attributes('data-scope')))
      .toEqual(['all'])
  })

  it('clicking a tab switches scope and refocuses the input', async () => {
    const palette = useCommandPalette()
    await openPalette()

    const gotoTab = tabs().find((tab) => tab.attributes('data-scope') === 'goto')!
    await gotoTab.trigger('click')

    expect(palette.scope.value).toBe('goto')
    expect(gotoTab.classes()).toContain('palette-tab-active')
    expect(document.activeElement).toBe(wrapper!.find('[data-testid="command-palette-input"]').element)
  })

  it('cycles scope forward and backward with Tab and Shift+Tab', async () => {
    const palette = useCommandPalette()
    await openPalette()
    expect(palette.scope.value).toBe('all')

    await panel().trigger('keydown', { key: 'Tab' })
    expect(palette.scope.value).toBe('goto')

    await panel().trigger('keydown', { key: 'Tab' })
    expect(palette.scope.value).toBe('actions')

    await panel().trigger('keydown', { key: 'Tab', shiftKey: true })
    expect(palette.scope.value).toBe('goto')
  })

  it('pops to All on Backspace with an empty query', async () => {
    const palette = useCommandPalette()
    await openPalette()
    palette.scope.value = 'goto'
    await flushPromises()

    await panel().trigger('keydown', { key: 'Backspace' })

    expect(palette.scope.value).toBe('all')
  })

  it('deletes a character on Backspace with a non-empty query, without popping scope', async () => {
    const palette = useCommandPalette()
    await openPalette()
    palette.scope.value = 'goto'
    await wrapper!.find('[data-testid="command-palette-input"]').setValue('abc')

    const event = new KeyboardEvent('keydown', { key: 'Backspace', bubbles: true, cancelable: true })
    panel().element.dispatchEvent(event)
    await flushPromises()

    expect(palette.scope.value).toBe('goto')
    expect(event.defaultPrevented).toBe(false)
  })

  it('shows only the shell escape for a !-query and runs it on Enter', async () => {
    const palette = useCommandPalette()
    await openPalette()

    await wrapper!.find('[data-testid="command-palette-input"]').setValue('!make build')

    const rows = wrapper!.findAll('.palette-row')
    expect(rows).toHaveLength(1)
    expect(rowTitle(rows[0])).toBe('Run: make build')
    expect(wrapper!.findAll('.palette-group-header')).toHaveLength(0)

    await panel().trigger('keydown', { key: 'Enter' })

    expect(runShell).toHaveBeenCalledWith('make build')
    expect(runBackend).not.toHaveBeenCalled()
    expect(palette.open.value).toBe(false)
  })
})
