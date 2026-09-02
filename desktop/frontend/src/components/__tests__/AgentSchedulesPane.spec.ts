import { createMemoryHistory } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentSchedulesPane from '../AgentSchedulesPane.vue'
import { resetAgentWorkspacesForTests } from '../../composables/useAgentWorkspaces'
import { resetAgentSchedulesForTests } from '../../composables/useAgentSchedules'
import { createAppRouter } from '../../router'
import type { AgentSchedule, AgentScheduleRun } from '../../lib/agentWorkspacesClient'

const HOUR_MS = 60 * 60 * 1000

const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  On: vi.fn(),
  schedules: vi.fn(),
  scheduleRuns: vi.fn(),
  saveSchedule: vi.fn(),
  deleteSchedule: vi.fn(),
  runSchedule: vi.fn(),
  previewSchedule: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice', () => ({
  Available: mocks.Available,
  Endpoint: vi.fn().mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' }),
}))
vi.mock('@wailsio/runtime', () => ({ Events: { On: mocks.On } }))
vi.mock('../../lib/agentWorkspacesClient', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/agentWorkspacesClient')>()),
  createAgentWorkspacesClient: () => ({
    schedules: mocks.schedules,
    scheduleRuns: mocks.scheduleRuns,
    saveSchedule: mocks.saveSchedule,
    deleteSchedule: mocks.deleteSchedule,
    runSchedule: mocks.runSchedule,
    previewSchedule: mocks.previewSchedule,
  }),
}))

function schedule(overrides: Partial<AgentSchedule> = {}): AgentSchedule {
  return {
    workspace: 'web-app',
    id: 'weekly-summary',
    name: 'Weekly summary',
    cron: '0 9 * * 5',
    prompt: 'Summarize the week.',
    disabled: false,
    onMissed: 'run',
    // A little past the whole hour: relativeAge floors, so a fixture sitting
    // exactly 2h out reads as "in 1h" by the time the row renders.
    nextRunAt: Date.now() + 2 * HOUR_MS + 30_000,
    lastRun: null,
    ...overrides,
  }
}

function run(overrides: Partial<AgentScheduleRun> = {}): AgentScheduleRun {
  return {
    id: 1,
    workspace: 'web-app',
    scheduleId: 'weekly-summary',
    scheduleName: 'Weekly summary',
    scheduledFor: Date.now(),
    startedAt: Date.now(),
    reason: 'due',
    status: 'launched',
    missed: 0,
    sessionId: null,
    prompt: 'Summarize the week.',
    error: '',
    ...overrides,
  }
}

function el<T extends HTMLElement>(testid: string): T {
  return document.querySelector<T>(`[data-testid="${testid}"]`)!
}

async function mountPane(rows: AgentSchedule[] = [schedule()], history: AgentScheduleRun[] = []) {
  mocks.schedules.mockResolvedValue(rows)
  mocks.scheduleRuns.mockResolvedValue(history)
  const router = createAppRouter(createMemoryHistory())
  await router.push('/workspaces/web-app')
  await router.isReady()
  const wrapper = mount(AgentSchedulesPane, {
    global: { plugins: [router] },
    props: { workspace: 'web-app', workspaceName: 'Web App' },
    attachTo: document.body,
  })
  await flushPromises()
  return { wrapper, router }
}

describe('AgentSchedulesPane', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    document.body.innerHTML = ''
    resetAgentWorkspacesForTests()
    resetAgentSchedulesForTests()
    mocks.Available.mockResolvedValue({ available: true, reason: '' })
    mocks.On.mockReturnValue(() => {})
    mocks.saveSchedule.mockResolvedValue(schedule())
    mocks.deleteSchedule.mockResolvedValue(undefined)
    mocks.runSchedule.mockResolvedValue(run({ id: 2, reason: 'manual' }))
    mocks.previewSchedule.mockResolvedValue({ next: [], prompt: '', cronError: '', promptError: '' })
  })

  it('states each schedule\'s cron, its next run and how the last one went', async () => {
    const { wrapper } = await mountPane([schedule({
      lastRun: run({ status: 'failed', reason: 'catch_up', error: 'the agent CLI is not on PATH' }),
    })])

    const row = wrapper.get('[data-testid="agent-schedule-row-weekly-summary"]')
    expect(row.text()).toContain('Weekly summary')
    expect(row.text()).toContain('0 9 * * 5')
    expect(wrapper.get('[data-testid="agent-schedule-next-weekly-summary"]').text()).toBe('in 2h')
    expect(wrapper.get('[data-testid="agent-schedule-last-weekly-summary"]').text()).toBe('failed · catch-up')
  })

  it('says what a schedule is when the workspace has none', async () => {
    const { wrapper } = await mountPane([])

    expect(wrapper.get('[data-testid="agent-schedules-empty"]').text()).toContain('starts a chat in this workspace')
    expect(wrapper.find('[data-testid="agent-schedule-row-weekly-summary"]').exists()).toBe(false)
  })

  it('runs a schedule now and re-reads the workspace afterwards', async () => {
    const { wrapper } = await mountPane()
    const readsBefore = mocks.schedules.mock.calls.length

    await wrapper.get('[data-testid="agent-schedule-run-weekly-summary"]').trigger('click')
    await flushPromises()

    expect(mocks.runSchedule).toHaveBeenCalledWith('web-app', 'weekly-summary')
    expect(mocks.schedules.mock.calls.length).toBe(readsBefore + 1)
  })

  // The switch writes the whole entry back with the flag flipped, so pausing a
  // schedule is an ordinary manifest save.
  it('pauses a schedule through the enable switch', async () => {
    const { wrapper } = await mountPane()

    await wrapper.get('[data-testid="agent-schedule-toggle-weekly-summary"]').trigger('click')
    await flushPromises()

    expect(mocks.saveSchedule).toHaveBeenCalledWith(expect.objectContaining({ id: 'weekly-summary', disabled: true }))
  })

  it('deletes a schedule only after the confirmation is answered', async () => {
    const { wrapper } = await mountPane()

    await wrapper.get('[data-testid="agent-schedule-delete-weekly-summary"]').trigger('click')
    await flushPromises()
    expect(mocks.deleteSchedule).not.toHaveBeenCalled()

    el<HTMLButtonElement>('agent-schedules-delete-confirmation-confirm').click()
    await flushPromises()

    expect(mocks.deleteSchedule).toHaveBeenCalledWith('web-app', 'weekly-summary')
  })

  // The pane asks for the chat rather than writing ?chat: a scheduled chat is
  // usually finished by the time anyone opens it, and only the row-click path
  // relaunches a dead session.
  it('lists a run\'s reason, missed count and error, and asks for its chat', async () => {
    const { wrapper } = await mountPane([schedule()], [
      run({ id: 3, reason: 'catch_up', missed: 2, sessionId: 7 }),
      run({ id: 4, status: 'skipped', error: 'the previous run\'s chat is still running' }),
    ])

    expect(wrapper.get('[data-testid="agent-schedule-run-missed-3"]').text()).toBe('2 missed')
    expect(wrapper.get('[data-testid="agent-schedule-run-row-3"]').text()).toContain('launched · catch-up')
    expect(wrapper.get('[data-testid="agent-schedule-run-error-4"]').text()).toContain('still running')
    expect(wrapper.find('[data-testid="agent-schedule-open-chat-4"]').exists()).toBe(false)

    await wrapper.get('[data-testid="agent-schedule-open-chat-3"]').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('open-chat')).toEqual([[7]])
  })

  it('reports a failed read without dropping the rows it could not refresh', async () => {
    const { wrapper } = await mountPane()

    mocks.schedules.mockRejectedValueOnce(new Error('the control plane is down'))
    // Drive the reload the way the Go side does, through the wake-up event.
    mocks.On.mock.calls[0][1]()
    await flushPromises()

    expect(wrapper.get('[data-testid="agent-schedules-error"]').text()).toBe('the control plane is down')
    expect(wrapper.find('[data-testid="agent-schedule-row-weekly-summary"]').exists()).toBe(true)
  })
})
