import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chooseOption } from '../../test-utils/select'

const mocks = vi.hoisted(() => ({
  ListTasks: vi.fn(),
  ReadTaskDetail: vi.fn(),
  SetTaskStatus: vi.fn(),
  DeleteTask: vi.fn(),
  PruneTasks: vi.fn(),
  TaskRepoKeys: vi.fn(),
  // useTasks.ts calls useWindowFocus() at module scope, which fires this the
  // instant the test file's imports are evaluated — before any beforeEach has
  // a chance to configure it. clearAllMocks() (unlike resetAllMocks()) keeps
  // this default resolved value across tests, so it only needs setting once.
  Focused: vi.fn().mockResolvedValue(true),
  On: vi.fn(() => () => {}),
  SetText: vi.fn(),
  OpenURL: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/tasksservice', () => ({
  ListTasks: mocks.ListTasks,
  TaskDetail: mocks.ReadTaskDetail,
  SetTaskStatus: mocks.SetTaskStatus,
  DeleteTask: mocks.DeleteTask,
  PruneTasks: mocks.PruneTasks,
  TaskRepoKeys: mocks.TaskRepoKeys,
}))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/windowservice', () => ({
  Focused: mocks.Focused,
}))
vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.On },
  Clipboard: { SetText: mocks.SetText },
  Browser: { OpenURL: mocks.OpenURL },
}))

import TasksView from '../TasksView.vue'
import { resetTasksForTests } from '../../composables/useTasks'

interface TaskOverrides {
  status: string
  type: string
  parentId: string
  blocked: boolean
  sessionId: string
  repoKey: string
}

function task(id: string, overrides: Partial<TaskOverrides> = {}) {
  return {
    id,
    repoKey: overrides.repoKey ?? 'acme/site',
    epicId: '',
    parentId: overrides.parentId ?? '',
    sessionId: overrides.sessionId ?? '',
    title: `Task ${id}`,
    type: overrides.type ?? 'task',
    status: overrides.status ?? 'open',
    blocked: overrides.blocked ?? false,
    depth: 0,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

function detailFrom(item: ReturnType<typeof task>, patch: Partial<{ desc: string; blockers: unknown[]; comments: unknown[] }> = {}) {
  return { ...item, desc: patch.desc ?? '', blockers: patch.blockers ?? [], comments: patch.comments ?? [] }
}

async function selectRow(wrapper: ReturnType<typeof mount>, id: string): Promise<void> {
  await wrapper.get(`[data-testid="task-tree-row"][data-id="${id}"]`).trigger('click')
  await flushPromises()
}

describe('TasksView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetTasksForTests()
    mocks.On.mockReturnValue(() => {})
    mocks.Focused.mockResolvedValue(true)
    mocks.ListTasks.mockResolvedValue([])
    mocks.TaskRepoKeys.mockResolvedValue([])
    mocks.ReadTaskDetail.mockResolvedValue(detailFrom(task('t1')))
    mocks.SetTaskStatus.mockResolvedValue(undefined)
    mocks.DeleteTask.mockResolvedValue(undefined)
    mocks.SetText.mockResolvedValue(undefined)
  })

  afterEach(() => {
    resetTasksForTests()
  })

  it('renders the tree from mocked items, indenting a child under its epic', async () => {
    mocks.ListTasks.mockResolvedValue([task('e1', { type: 'epic' }), task('t1', { parentId: 'e1' })])
    const wrapper = mount(TasksView)
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="task-tree-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('Task e1')
    expect(rows[1].text()).toContain('Task t1')
    expect(rows[0].attributes('data-id')).toBe('e1')
    expect(rows[1].attributes('data-id')).toBe('t1')
  })

  it('shows the no-data hint when the repo has no tasks at all', async () => {
    const wrapper = mount(TasksView)
    await flushPromises()

    expect(wrapper.find('[data-testid="tasks-empty"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('hive hc create')
  })

  it('shows a distinct empty state when items exist but none match the active filter', async () => {
    mocks.ListTasks.mockResolvedValue([task('t1', { status: 'done' })])
    const wrapper = mount(TasksView)
    await flushPromises()

    // Default filter is "open"; a done-only item set matches nothing under it.
    expect(wrapper.find('[data-testid="tasks-empty-filter"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="tasks-empty"]').exists()).toBe(false)
  })

  it('persists the active filter under its storage key', async () => {
    const wrapper = mount(TasksView)
    await flushPromises()

    await wrapper.get('[data-testid="tasks-filter-all"]').trigger('click')

    expect(localStorage.getItem('hive.tasks.filter')).toBe('all')
    expect(wrapper.get('[data-testid="tasks-filter-all"]').attributes('aria-pressed')).toBe('true')
  })

  it('re-reads the list after a status change that needs no confirm', async () => {
    mocks.ListTasks.mockResolvedValue([task('t1')])
    mocks.ReadTaskDetail.mockResolvedValue(detailFrom(task('t1')))
    const wrapper = mount(TasksView)
    await flushPromises()
    await selectRow(wrapper, 't1')

    mocks.ListTasks.mockClear()
    await chooseOption(wrapper, 'task-status-select', 'in_progress')
    await flushPromises()

    expect(mocks.SetTaskStatus).toHaveBeenCalledWith('t1', 'in_progress')
    expect(mocks.ListTasks).toHaveBeenCalledTimes(1)
  })

  it('confirms cancelling a task before applying it', async () => {
    mocks.ListTasks.mockResolvedValue([task('t1')])
    mocks.ReadTaskDetail.mockResolvedValue(detailFrom(task('t1')))
    const wrapper = mount(TasksView)
    await flushPromises()
    await selectRow(wrapper, 't1')

    await chooseOption(wrapper, 'task-status-select', 'cancelled')
    await flushPromises()

    expect(mocks.SetTaskStatus).not.toHaveBeenCalled()
    const confirm = document.querySelector('[data-testid="task-cancel-confirm"]')
    expect(confirm).not.toBeNull()

    document.querySelector<HTMLButtonElement>('[data-testid="task-cancel-confirm-confirm"]')!.click()
    await flushPromises()
    expect(mocks.SetTaskStatus).toHaveBeenCalledWith('t1', 'cancelled')
  })

  it('confirms an epic status cascade, naming the open descendant count', async () => {
    const epic = task('e1', { type: 'epic' })
    mocks.ListTasks.mockResolvedValue([epic, task('c1', { parentId: 'e1', status: 'open' }), task('c2', { parentId: 'e1', status: 'in_progress' }), task('c3', { parentId: 'e1', status: 'done' })])
    mocks.ReadTaskDetail.mockResolvedValue(detailFrom(epic))
    const wrapper = mount(TasksView)
    await flushPromises()
    await selectRow(wrapper, 'e1')

    await chooseOption(wrapper, 'task-status-select', 'done')
    await flushPromises()

    expect(mocks.SetTaskStatus).not.toHaveBeenCalled()
    const confirm = document.querySelector('[data-testid="task-cascade-confirm"]')
    // Two open/in_progress children count toward the cascade; the done one does not.
    expect(confirm?.textContent).toContain('also closes 2 open tasks')

    document.querySelector<HTMLButtonElement>('[data-testid="task-cascade-confirm-confirm"]')!.click()
    await flushPromises()
    expect(mocks.SetTaskStatus).toHaveBeenCalledWith('e1', 'done')
  })

  it('warns that delete removes the whole subtree, then re-reads after confirming', async () => {
    mocks.ListTasks.mockResolvedValue([task('t1')])
    mocks.ReadTaskDetail.mockResolvedValue(detailFrom(task('t1')))
    const wrapper = mount(TasksView)
    await flushPromises()
    await selectRow(wrapper, 't1')

    await wrapper.get('[data-testid="task-delete"]').trigger('click')
    const confirm = document.querySelector('[data-testid="task-delete-confirm"]')
    expect(confirm?.textContent).toContain('everything nested under it')
    expect(confirm?.textContent).toContain('comments included')

    mocks.ListTasks.mockClear()
    mocks.ListTasks.mockResolvedValue([])
    document.querySelector<HTMLButtonElement>('[data-testid="task-delete-confirm-confirm"]')!.click()
    await flushPromises()

    expect(mocks.DeleteTask).toHaveBeenCalledWith('t1')
    expect(mocks.ListTasks).toHaveBeenCalledTimes(1)
  })

  it('shows the prune dry-run count in the confirm copy before pruning', async () => {
    mocks.PruneTasks.mockResolvedValueOnce(5)
    const wrapper = mount(TasksView)
    await flushPromises()

    await wrapper.get('[data-testid="tasks-prune"]').trigger('click')
    await flushPromises()

    expect(mocks.PruneTasks).toHaveBeenCalledWith(30, '', true)
    const confirm = document.querySelector('[data-testid="tasks-prune-confirm"]')
    expect(confirm?.textContent).toContain('5 tasks')
    expect(confirm?.textContent).toContain('nested under a pruned root')

    mocks.PruneTasks.mockResolvedValueOnce(0)
    document.querySelector<HTMLButtonElement>('[data-testid="tasks-prune-confirm-confirm"]')!.click()
    await flushPromises()
    expect(mocks.PruneTasks).toHaveBeenLastCalledWith(30, '', false)
  })

  it('surfaces a dry-run failure in a banner, since it never opens the confirm dialog', async () => {
    mocks.PruneTasks.mockRejectedValueOnce(Object.assign(new Error('boom'), { cause: { kind: 'internal', message: 'hc store is locked' } }))
    const wrapper = mount(TasksView)
    await flushPromises()

    await wrapper.get('[data-testid="tasks-prune"]').trigger('click')
    await flushPromises()

    expect(document.querySelector('[data-testid="tasks-prune-confirm"]')).toBeNull()
    expect(wrapper.get('[data-testid="tasks-prune-error"]').text()).toContain('hc store is locked')
  })

  it('shows an explanatory empty state (not the error dialog) when tasks are unavailable', async () => {
    mocks.ListTasks.mockRejectedValue(Object.assign(new Error('boom'), { cause: { kind: 'unavailable', message: 'hc store is not running' } }))
    const wrapper = mount(TasksView)
    await flushPromises()

    expect(wrapper.find('[data-testid="tasks-unavailable"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="error-dialog"]').exists()).toBe(false)
  })

  it('copies the task id via the native clipboard', async () => {
    mocks.ListTasks.mockResolvedValue([task('t1')])
    mocks.ReadTaskDetail.mockResolvedValue(detailFrom(task('t1')))
    const wrapper = mount(TasksView)
    await flushPromises()
    await selectRow(wrapper, 't1')

    await wrapper.get('[data-testid="task-detail-copy-id"]').trigger('click')

    expect(mocks.SetText).toHaveBeenCalledWith('t1')
  })

  it('renders a vanished blocker by its bare id, and a checkpoint comment with its badge and stripped prefix', async () => {
    mocks.ListTasks.mockResolvedValue([task('t1')])
    mocks.ReadTaskDetail.mockResolvedValue(detailFrom(task('t1'), {
      blockers: [{ id: 'b1', title: 'Ship the API', status: 'open' }, { id: 'b2', title: '', status: '' }],
      comments: [{ id: 'c1', message: 'CHECKPOINT: landed the migration', createdAt: '2026-01-01T00:00:00Z' }],
    }))
    const wrapper = mount(TasksView)
    await flushPromises()
    await selectRow(wrapper, 't1')

    const blockers = wrapper.findAll('[data-testid="task-blocker-chip"]')
    expect(blockers[0].text()).toContain('Ship the API')
    expect(blockers[1].text()).toContain('b2')

    expect(wrapper.find('[data-testid="task-comment-checkpoint"]').exists()).toBe(true)
    const comment = wrapper.get('[data-testid="task-comment"]')
    expect(comment.text()).toContain('landed the migration')
    expect(comment.text()).not.toContain('CHECKPOINT:')
  })
})
