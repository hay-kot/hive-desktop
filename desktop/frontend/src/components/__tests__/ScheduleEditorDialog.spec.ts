import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ScheduleEditorDialog from '../ScheduleEditorDialog.vue'
import { useAgentSchedules } from '../../composables/useAgentSchedules'
import type { AgentSchedule } from '../../lib/agentWorkspacesClient'

/** Matches PREVIEW_DEBOUNCE_MS in the component. */
const DEBOUNCE_MS = 300

const mocks = vi.hoisted(() => ({ preview: vi.fn() }))
// The workspace's existing rows are what an id collision is checked against.
// The ref lives in the factory because vi.hoisted runs before the imports it
// would need.
vi.mock('../../composables/useAgentSchedules', async () => {
  const { ref } = await import('vue')
  const schedules = ref<AgentSchedule[]>([])
  return { useAgentSchedules: () => ({ preview: mocks.preview, schedules }) }
})

function existing(overrides: Partial<AgentSchedule> = {}): AgentSchedule {
  return {
    workspace: 'web-app',
    id: 'weekly-summary',
    name: 'Weekly summary',
    cron: '0 9 * * 5',
    prompt: 'Summarize the week.',
    disabled: false,
    onMissed: 'skip',
    nextRunAt: null,
    lastRun: null,
    ...overrides,
  }
}

function mountDialog(schedule: AgentSchedule | null = null) {
  return mount(ScheduleEditorDialog, {
    attachTo: document.body,
    props: { workspace: 'web-app', schedule, busy: false, error: '' },
    global: { stubs: { Teleport: true } },
  })
}

async function settlePreview(): Promise<void> {
  await vi.advanceTimersByTimeAsync(DEBOUNCE_MS)
  await flushPromises()
}

describe('ScheduleEditorDialog', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    document.body.innerHTML = ''
    useAgentSchedules().schedules.value = []
    mocks.preview.mockReset()
    mocks.preview.mockResolvedValue({ next: [1_700_000_000_000, 1_700_086_400_000, 1_700_172_800_000], prompt: 'rendered', cronError: '', promptError: '' })
  })

  afterEach(() => vi.useRealTimers())

  // The preset is derived from the cron rather than stored beside it, so a
  // hand-edited cron falls back to Custom with no second source of truth.
  it('fills the cron from a preset, and an edited cron reads as Custom', async () => {
    const wrapper = mountDialog()

    await wrapper.get('[data-testid="schedule-editor-preset"]').trigger('click')
    await wrapper.vm.$nextTick()
    document.querySelector<HTMLElement>('[data-testid="schedule-editor-preset-option-@hourly"]')!.click()
    await flushPromises()

    expect((wrapper.get('[data-testid="schedule-editor-cron"]').element as HTMLInputElement).value).toBe('@hourly')
    expect(wrapper.get('[data-testid="schedule-editor-preset"]').text()).toContain('Every hour')

    await wrapper.get('[data-testid="schedule-editor-cron"]').setValue('7 3 * * *')

    expect(wrapper.get('[data-testid="schedule-editor-preset"]').text()).toContain('Custom')
  })

  it('derives a new schedule\'s id from its name and keeps an existing one fixed', async () => {
    const fresh = mountDialog()
    await fresh.get('[data-testid="schedule-editor-name"]').setValue('  Weekly Product Summary!  ')
    expect(fresh.get('[data-testid="schedule-editor-id"]').text()).toBe('weekly-product-summary')

    const saved = mountDialog(existing())
    await saved.get('[data-testid="schedule-editor-name"]').setValue('Renamed entirely')
    expect(saved.get('[data-testid="schedule-editor-id"]').text()).toBe('weekly-summary')
  })

  // Cron and template validity are the Go side's answer, not this form's: it
  // asks, shows what came back beside the field, and refuses the save.
  it('shows the preview errors inline and blocks the save while either is set', async () => {
    mocks.preview.mockResolvedValue({ next: [], prompt: '', cronError: 'expected 5 fields, found 3', promptError: '' })
    const wrapper = mountDialog(existing())
    await settlePreview()

    expect(wrapper.get('[data-testid="schedule-editor-cron-error"]').text()).toBe('expected 5 fields, found 3')
    expect(wrapper.get('[data-testid="schedule-editor-save"]').attributes('disabled')).toBeDefined()

    mocks.preview.mockResolvedValue({ next: [], prompt: '', cronError: '', promptError: 'function "nope" not defined' })
    await wrapper.get('[data-testid="schedule-editor-prompt"]').setValue('{{ nope }}')
    await settlePreview()

    expect(wrapper.get('[data-testid="schedule-editor-prompt-error"]').text()).toBe('function "nope" not defined')
    expect(wrapper.get('[data-testid="schedule-editor-save"]').attributes('disabled')).toBeDefined()
  })

  it('renders the next occurrences the preview reports', async () => {
    const wrapper = mountDialog(existing())
    await settlePreview()

    expect(mocks.preview).toHaveBeenCalledWith({ workspace: 'web-app', cron: '0 9 * * 5', prompt: 'Summarize the week.' })
    expect(wrapper.findAll('[data-testid="schedule-editor-next"] li')).toHaveLength(3)
  })

  it('emits the whole manifest entry on save', async () => {
    const wrapper = mountDialog()
    await wrapper.get('[data-testid="schedule-editor-name"]').setValue('Weekly summary')
    await wrapper.get('[data-testid="schedule-editor-cron"]').setValue('0 9 * * 5')
    await wrapper.get('[data-testid="schedule-editor-prompt"]').setValue('Summarize the week.')
    await settlePreview()

    await wrapper.get('[data-testid="schedule-editor-save"]').trigger('click')

    expect(wrapper.emitted('save')).toEqual([[{
      workspace: 'web-app',
      id: 'weekly-summary',
      name: 'Weekly summary',
      cron: '0 9 * * 5',
      prompt: 'Summarize the week.',
      disabled: false,
      onMissed: 'run',
    }]])
  })

  // The Go side upserts by id, so a new schedule named into an existing id
  // would silently replace it. Editing that same schedule is untouched: its id
  // is fixed and the row it matches is itself.
  it('refuses a new schedule whose derived id is already taken', async () => {
    useAgentSchedules().schedules.value = [existing()]
    const wrapper = mountDialog()
    await wrapper.get('[data-testid="schedule-editor-name"]').setValue('Weekly summary')
    await wrapper.get('[data-testid="schedule-editor-cron"]').setValue('0 9 * * 5')
    await wrapper.get('[data-testid="schedule-editor-prompt"]').setValue('Summarize the week.')
    await settlePreview()

    expect(wrapper.get('[data-testid="schedule-editor-id-taken"]').text()).toBe('A schedule with this id already exists')
    expect(wrapper.get('[data-testid="schedule-editor-save"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="schedule-editor-name"]').setValue('Weekly summary two')

    expect(wrapper.find('[data-testid="schedule-editor-id-taken"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="schedule-editor-save"]').attributes('disabled')).toBeUndefined()

    const edit = mountDialog(existing())
    await settlePreview()
    expect(edit.find('[data-testid="schedule-editor-id-taken"]').exists()).toBe(false)
  })

  // A preview that never answered says nothing about the cron and prompt on
  // screen now, so its predecessor's verdict must not keep the button off.
  it('clears an earlier preview error when the next preview call fails', async () => {
    mocks.preview.mockResolvedValue({ next: [], prompt: '', cronError: 'expected 5 fields, found 3', promptError: '' })
    const wrapper = mountDialog(existing())
    await settlePreview()
    expect(wrapper.get('[data-testid="schedule-editor-save"]').attributes('disabled')).toBeDefined()

    mocks.preview.mockRejectedValue(new Error('the control plane is down'))
    await wrapper.get('[data-testid="schedule-editor-cron"]').setValue('0 9 * * 1')
    await settlePreview()

    expect(wrapper.find('[data-testid="schedule-editor-cron-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="schedule-editor-save"]').attributes('disabled')).toBeUndefined()
  })

  // An empty required field is refused before the Go side ever sees it: a
  // schedule with no prompt has nothing to launch.
  it('refuses to save while a required field is empty', async () => {
    const wrapper = mountDialog()
    await wrapper.get('[data-testid="schedule-editor-name"]').setValue('Weekly summary')
    await settlePreview()

    expect(wrapper.get('[data-testid="schedule-editor-save"]').attributes('disabled')).toBeDefined()
  })
})
