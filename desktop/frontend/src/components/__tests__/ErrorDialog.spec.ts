import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ErrorDialog from '../ErrorDialog.vue'
import type { ErrorDetails } from '../../composables/useErrorDialog'

const mocks = vi.hoisted(() => ({
  Preview: vi.fn(),
  Submit: vi.fn(),
  SetText: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/reportservice', () => ({
  Preview: mocks.Preview,
  Submit: mocks.Submit,
}))
vi.mock('@wailsio/runtime', () => ({
  Clipboard: { SetText: mocks.SetText },
}))

const DETAIL = 'flow "inbox": node "notify-me": action "ping" is not defined in actions.yml'

function details(overrides: Partial<ErrorDetails> = {}): ErrorDetails {
  return {
    title: 'Deploy failed',
    summary: 'Inbox was not written. The version already on disk keeps running.',
    detail: DETAIL,
    context: { flow: 'inbox' },
    ...overrides,
  }
}

function preview(available = true) {
  return { available, hasSettings: true, flowCount: 2, hasActions: true, accountCount: 1, hasLogs: true, logBytes: 4096 }
}

async function mountDialog(error: ErrorDetails = details()) {
  const wrapper = mount(ErrorDialog, { props: { error } })
  await flushPromises()
  return wrapper
}

function el(testid: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(`[data-testid="${testid}"]`)
}

beforeEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
  mocks.Preview.mockResolvedValue(preview())
  mocks.Submit.mockResolvedValue({ id: 'rpt_abc123' })
  mocks.SetText.mockResolvedValue(undefined)
})

describe('ErrorDialog', () => {
  it('shows the error text in full rather than a truncated summary', async () => {
    await mountDialog()

    expect(el('error-dialog')?.getAttribute('role')).toBe('alertdialog')
    expect(el('error-dialog-summary')?.textContent).toContain('keeps running')
    expect(el('error-dialog-detail')?.textContent).toBe(DETAIL)
    expect(el('error-dialog-context')?.textContent).toContain('inbox')
  })

  it('stays open when the backdrop is clicked, so the failure cannot be dismissed by a stray click', async () => {
    const wrapper = await mountDialog()

    el('error-dialog-backdrop')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()

    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('copies the title, context and detail as one block', async () => {
    await mountDialog()

    el('error-dialog-copy')?.click()
    await flushPromises()

    expect(mocks.SetText).toHaveBeenCalledWith(
      `Deploy failed\n\nInbox was not written. The version already on disk keeps running.\n\nflow: inbox\n\n${DETAIL}`,
    )
    expect(el('error-dialog-copy')?.textContent).toContain('Copied')
  })

  it('files a diagnostic report in one click and keeps the reference id', async () => {
    await mountDialog()

    el('error-dialog-report')?.click()
    await flushPromises()

    expect(mocks.Submit).toHaveBeenCalledWith({
      description: expect.stringContaining(DETAIL),
      contact: '',
      includeBasics: true,
      includeSettings: true,
      includeFlows: true,
      includeActions: true,
    })
    expect(el('error-dialog-report-id')?.textContent).toBe('rpt_abc123')
    expect(el('error-dialog-report')).toBeNull()
  })

  it('surfaces a failed report without losing the error it was raised for', async () => {
    mocks.Submit.mockRejectedValue(new Error('report endpoint unreachable'))
    await mountDialog()

    el('error-dialog-report')?.click()
    await flushPromises()

    expect(el('error-dialog-report-error')?.textContent).toContain('unreachable')
    expect(el('error-dialog-detail')?.textContent).toBe(DETAIL)
  })

  it('explains and disables reporting in a build with no report endpoint', async () => {
    mocks.Preview.mockResolvedValue(preview(false))
    await mountDialog()

    expect(el('error-dialog-report-unavailable')).not.toBeNull()
    expect(el('error-dialog-report')).toHaveProperty('disabled', true)
  })

  it('says so when the failure carried no text', async () => {
    await mountDialog(details({ detail: '   ', summary: undefined, context: undefined }))

    expect(el('error-dialog-detail')?.textContent).toBe('No further detail was reported.')
  })
})
