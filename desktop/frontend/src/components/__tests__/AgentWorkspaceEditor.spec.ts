import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import AgentWorkspaceEditor from '../AgentWorkspaceEditor.vue'
import { resetAgentWorkspacesForTests, useAgentWorkspaces } from '../../composables/useAgentWorkspaces'
import type {
  AgentSchedule, AgentScheduleRun, AgentWorkspace, MCPCatalogueEntry, SkillPackage,
} from '../../lib/agentWorkspacesClient'

// The schedule half of the form talks to the control plane for three things
// (preview, Run now, and the run history), so those specs install a client. The
// rest run with none, the way the composable leaves it when the probe fails,
// which is what keeps a seeded mcpCatalogue/skillPackages from being read back
// over by an empty response.
const mocks = vi.hoisted(() => ({
  Available: vi.fn(),
  Endpoint: vi.fn(),
  getAgentsEndpoint: vi.fn(),
  client: null as unknown,
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/agentsservice', () => ({
  Available: mocks.Available,
  Endpoint: mocks.Endpoint,
}))
vi.mock('../../lib/agentWorkspacesClient', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/agentWorkspacesClient')>()),
  getAgentsEndpoint: mocks.getAgentsEndpoint,
  createAgentWorkspacesClient: () => mocks.client,
}))

const demo: AgentWorkspace = {
  dir: 'demo', name: 'Demo', agent: 'claude', autonomy: 'ask', mcps: [], skills: [], schedules: [], problem: '', notice: '',
}

const playwright: MCPCatalogueEntry = {
  id: 'playwright', title: 'Playwright', description: 'Browser automation', shipped: true,
  stability: 'stable', shadows: '', transport: 'stdio', command: 'npx -y @playwright/mcp@latest', problem: '',
}

const hivePackage: SkillPackage = {
  name: 'hive',
  title: 'Hive',
  description: 'Configure Hive Desktop itself.',
  members: [{ slug: 'hive-mcp', shipped: true }, { slug: 'hive-flows', shipped: true }],
}

const infraPackage: SkillPackage = {
  name: 'infra',
  title: 'infra',
  description: 'Terraform and Kubernetes.',
  members: [{ slug: 'terraform-plan', shipped: false }],
}

// The drawer teleports to the body.
function el<T extends HTMLElement>(testid: string): T | null {
  return document.querySelector<T>(`[data-testid="${testid}"]`)
}

function mountEditor(workspace: AgentWorkspace | null = demo) {
  return mount(AgentWorkspaceEditor, {
    props: { workspace, agents: ['claude', 'codex'] },
    attachTo: document.body,
  })
}

function schedule(overrides: Partial<AgentSchedule> = {}): AgentSchedule {
  return {
    workspace: 'demo', id: 'weekly-summary', name: 'Weekly summary', cron: '0 9 * * 5',
    prompt: 'Summarize the week.', disabled: false, onMissed: 'run',
    nextRunAt: null, lastRun: null, ...overrides,
  }
}

function run(overrides: Partial<AgentScheduleRun> = {}): AgentScheduleRun {
  return {
    id: 3, workspace: 'demo', scheduleId: 'weekly-summary', scheduleName: 'Weekly summary',
    scheduledFor: Date.now(), startedAt: Date.now(), reason: 'due', status: 'launched',
    missed: 0, sessionId: 9, prompt: 'Summarize the week.', error: '', ...overrides,
  }
}

function scheduleClient() {
  return {
    mcpCatalogue: vi.fn().mockResolvedValue([]),
    skillPackages: vi.fn().mockResolvedValue({ packages: [], skills: [], problem: '' }),
    scheduleRuns: vi.fn().mockResolvedValue([run()]),
    runSchedule: vi.fn().mockResolvedValue(run({ reason: 'manual' })),
    previewSchedule: vi.fn().mockResolvedValue({ next: [], prompt: '', cronError: '', promptError: '' }),
  }
}

function typeInto(testid: string, value: string): void {
  const field = el<HTMLInputElement | HTMLTextAreaElement>(testid)!
  field.value = value
  field.dispatchEvent(new Event('input', { bubbles: true }))
}

// AppSelect's popover teleports out of the drawer, so both halves are reached
// through the document rather than the wrapper.
async function chooseOption(wrapper: VueWrapper, testid: string, value: string): Promise<void> {
  el<HTMLButtonElement>(testid)!.click()
  await flushPromises()
  document.querySelector<HTMLElement>(`[data-testid="${testid}-option-${value}"]`)!
    .dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await wrapper.vm.$nextTick()
}

/** Long enough for the card's 300ms preview debounce to fire and land. */
function settlePreview(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 350))
}

function savedSchedules(wrapper: VueWrapper) {
  const saves = wrapper.emitted('save') as unknown[][] | undefined
  return (saves?.[0]?.[0] as { schedules: unknown[] } | undefined)?.schedules
}

beforeEach(() => {
  document.body.innerHTML = ''
  resetAgentWorkspacesForTests()
  mocks.client = null
  mocks.Available.mockResolvedValue({ available: true, reason: '' })
  mocks.getAgentsEndpoint.mockResolvedValue({ httpBaseURL: 'http://127.0.0.1:1', wsURL: 'ws://127.0.0.1:1/s', token: 'test' })
})

describe('AgentWorkspaceEditor', () => {
  it('keeps delete a quiet footer action that only emits after the inline confirm', async () => {
    const wrapper = mountEditor()
    expect(el('agent-workspace-editor-delete-confirm')).toBeNull()

    el<HTMLButtonElement>('agent-workspace-editor-delete')!.click()
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-delete-confirm')).not.toBeNull()
    expect(wrapper.emitted('delete')).toBeUndefined()

    el<HTMLButtonElement>('agent-workspace-editor-delete-confirm-confirm')!.click()
    expect(wrapper.emitted('delete')).toEqual([['demo']])
    wrapper.unmount()
  })

  it('cancelling the confirm strip returns to the form without emitting', async () => {
    const wrapper = mountEditor()
    el<HTMLButtonElement>('agent-workspace-editor-delete')!.click()
    await wrapper.vm.$nextTick()

    el<HTMLButtonElement>('agent-workspace-editor-delete-confirm-cancel')!.click()
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-delete-confirm')).toBeNull()
    expect(el('agent-workspace-editor-delete')).not.toBeNull()
    expect(wrapper.emitted('delete')).toBeUndefined()
    wrapper.unmount()
  })

  it('a pending confirm makes the form inert: no save on Enter, no close on Escape', async () => {
    const wrapper = mountEditor()
    el<HTMLButtonElement>('agent-workspace-editor-delete')!.click()
    await wrapper.vm.$nextTick()

    const name = el<HTMLInputElement>('agent-workspace-editor-name')!
    name.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(wrapper.emitted('save')).toBeUndefined()

    // Escape answers the strip (cancel), never the sheet.
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(el('agent-workspace-editor-delete-confirm')).toBeNull()
    wrapper.unmount()
  })

  it('the delete confirm names the folder it removes from disk', async () => {
    const { root } = useAgentWorkspaces()
    root.value = '/Users/me/workspaces'
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    el<HTMLButtonElement>('agent-workspace-editor-delete')!.click()
    await wrapper.vm.$nextTick()

    const confirm = el<HTMLElement>('agent-workspace-editor-delete-confirm')!
    expect(confirm.textContent).toContain('/Users/me/workspaces/demo')
    expect(confirm.textContent).toContain('deleted from disk')
    wrapper.unmount()
  })

  // A manifest the loader could not read reaches the form as empty lists, so a
  // save would reconcile mcps:/skills:/schedules: to nothing. The Go side
  // refuses the update; the form refuses it first and names the file to fix.
  it('refuses to save a workspace whose manifest could not be read', async () => {
    const wrapper = mountEditor({ ...demo, problem: 'agent-workspace.yaml: line 4: mapping values are not allowed' })
    await wrapper.vm.$nextTick()

    const notice = el('agent-workspace-editor-problem')!
    expect(notice.textContent).toContain('mapping values are not allowed')
    expect(notice.textContent).toContain('Fix agent-workspace.yaml')
    expect(el<HTMLButtonElement>('agent-workspace-editor-save')!.disabled).toBe(true)
    // Deleting a workspace whose file is broken has to stay possible.
    expect(el<HTMLButtonElement>('agent-workspace-editor-delete')!.disabled).toBe(false)
    wrapper.unmount()
  })

  it('shows no manifest notice for a readable workspace or a new one', async () => {
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-problem')).toBeNull()
    expect(el<HTMLButtonElement>('agent-workspace-editor-save')!.disabled).toBe(false)
    wrapper.unmount()

    const creating = mountEditor(null)
    await creating.vm.$nextTick()
    expect(el('agent-workspace-editor-problem')).toBeNull()
    creating.unmount()
  })

  it('creation offers no delete', () => {
    const wrapper = mountEditor(null)
    expect(el('agent-workspace-editor-delete')).toBeNull()
    expect(el('agent-workspace-editor-save')).not.toBeNull()
    wrapper.unmount()
  })

  it('the autonomy selector lays out every posture with the flags it launches', async () => {
    const { autonomyFlags } = useAgentWorkspaces()
    autonomyFlags.value = {
      claude: { ask: [], auto: ['--permission-mode', 'acceptEdits'], full: ['--dangerously-skip-permissions'] },
    }
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    const full = el<HTMLButtonElement>('agent-workspace-editor-autonomy-full')!
    expect(full.textContent).toContain('dangerously skip permissions')
    expect(full.textContent).toContain('--dangerously-skip-permissions')
    expect(full.getAttribute('aria-checked')).toBe('false')
    expect(el('agent-workspace-editor-autonomy-ask')!.getAttribute('aria-checked')).toBe('true')

    full.click()
    await wrapper.vm.$nextTick()
    expect(full.getAttribute('aria-checked')).toBe('true')

    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()
    expect(wrapper.emitted('save')).toEqual([[
      { dir: 'demo', name: 'Demo', agent: 'claude', autonomy: 'full', mcps: [], skills: [], schedules: [] },
    ]])
    wrapper.unmount()
  })

  it('a posture the launch table refuses for the agent is disabled', async () => {
    const { autonomyFlags } = useAgentWorkspaces()
    autonomyFlags.value = { claude: { ask: [] } }
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    const full = el<HTMLButtonElement>('agent-workspace-editor-autonomy-full')!
    expect(full.disabled).toBe(true)
    expect(full.textContent).toContain('not available for this agent')
    wrapper.unmount()
  })

  it('save carries the toggled mcps list', async () => {
    const { mcpCatalogue } = useAgentWorkspaces()
    mcpCatalogue.value = [playwright]
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    el<HTMLButtonElement>('agent-workspace-editor-mcp-playwright')!.click()
    await wrapper.vm.$nextTick()
    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()

    expect(wrapper.emitted('save')).toEqual([[
      { dir: 'demo', name: 'Demo', agent: 'claude', autonomy: 'ask', mcps: ['playwright'], skills: [], schedules: [] },
    ]])
    wrapper.unmount()
  })

  it('save carries the toggled package list', async () => {
    const { skillPackages } = useAgentWorkspaces()
    skillPackages.value = [hivePackage, infraPackage]
    const wrapper = mountEditor({ ...demo, skills: ['hive'] })
    await wrapper.vm.$nextTick()

    // A package is offered but off until this workspace switches it on — the
    // definitions are shared, the toggle is the workspace's own.
    el<HTMLButtonElement>('agent-workspace-editor-skill-infra')!.click()
    await wrapper.vm.$nextTick()
    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()

    expect(wrapper.emitted('save')).toEqual([[
      { dir: 'demo', name: 'Demo', agent: 'claude', autonomy: 'ask', mcps: [], skills: ['hive', 'infra'], schedules: [] },
    ]])
    wrapper.unmount()
  })

  it('a package expands to the skills its patterns select', async () => {
    const { skillPackages } = useAgentWorkspaces()
    skillPackages.value = [hivePackage]
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    const members = el<HTMLButtonElement>('agent-workspace-editor-skill-members-hive')!
    expect(members.textContent).toContain('2 skills')
    expect(members.getAttribute('aria-expanded')).toBe('false')

    const section = el<HTMLElement>('agent-workspace-editor-skills')!
    expect(section.textContent).not.toContain('hive-mcp')

    members.click()
    await wrapper.vm.$nextTick()
    expect(section.textContent).toContain('hive-mcp')
    expect(section.textContent).toContain('hive-flows')
    wrapper.unmount()
  })

  it('an enabled package skills.yml no longer defines still rows, marked missing', async () => {
    const { skillPackages } = useAgentWorkspaces()
    skillPackages.value = [hivePackage]
    const wrapper = mountEditor({ ...demo, skills: ['deleted-package'] })
    await wrapper.vm.$nextTick()

    expect(el('agent-workspace-editor-skill-deleted-package')).not.toBeNull()
    expect(el<HTMLElement>('agent-workspace-editor-skills')!.textContent).toContain('not defined in skills.yml')
    wrapper.unmount()
  })

  it('names the fix when an enabled name is a skill rather than a package', async () => {
    const { skillPackages, skillNames } = useAgentWorkspaces()
    skillPackages.value = [hivePackage]
    skillNames.value = [
      { slug: 'hive-mcp', shipped: true, selectedBy: ['hive'] },
      { slug: 'hive-flows', shipped: true, selectedBy: ['hive'] },
    ]
    const wrapper = mountEditor({ ...demo, skills: ['hive-mcp'] })
    await wrapper.vm.$nextTick()

    const text = el<HTMLElement>('agent-workspace-editor-skills')!.textContent!
    expect(text).toContain('a skill, not a package')
    expect(text).toContain('"hive" package selects it')
    expect(text).not.toContain('not defined in skills.yml')
    wrapper.unmount()
  })

  it('a skill no package selects says to define one rather than pointing nowhere', async () => {
    const { skillPackages, skillNames } = useAgentWorkspaces()
    skillPackages.value = [hivePackage]
    skillNames.value = [{ slug: 'orphan', shipped: false, selectedBy: [] }]
    const wrapper = mountEditor({ ...demo, skills: ['orphan'] })
    await wrapper.vm.$nextTick()

    expect(el<HTMLElement>('agent-workspace-editor-skills')!.textContent).toContain('no package selects it yet')
    wrapper.unmount()
  })

  it('a package whose patterns match nothing says so', async () => {
    const { skillPackages } = useAgentWorkspaces()
    skillPackages.value = [{ name: 'typoed', title: 'typoed', description: '', members: [] }]
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    expect(el<HTMLElement>('agent-workspace-editor-skills')!.textContent).toContain('matches no skill')
    wrapper.unmount()
  })

  it('a declared id the catalogue no longer resolves still rows, marked missing', async () => {
    const { mcpCatalogue } = useAgentWorkspaces()
    mcpCatalogue.value = [playwright]
    const wrapper = mountEditor({ ...demo, mcps: ['ghost'] })
    await wrapper.vm.$nextTick()

    const toggle = el<HTMLButtonElement>('agent-workspace-editor-mcp-ghost')
    expect(toggle).not.toBeNull()
    expect(toggle!.getAttribute('aria-checked')).toBe('true')
    expect(el('agent-workspace-editor-mcp-remove-ghost')).toBeNull()
    wrapper.unmount()
  })

  it('format json pretty-prints the paste box, and reports invalid input', async () => {
    const wrapper = mountEditor()
    el<HTMLButtonElement>('agent-workspace-editor-mcp-import')!.click()
    await wrapper.vm.$nextTick()

    const textarea = el<HTMLTextAreaElement>('agent-workspace-editor-mcp-import-text')!
    textarea.value = '{"a":{"command":"npx"}}'
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()

    el<HTMLButtonElement>('agent-workspace-editor-mcp-import-format')!.click()
    await wrapper.vm.$nextTick()
    expect(textarea.value).toBe('{\n  "a": {\n    "command": "npx"\n  }\n}')
    expect(el('agent-workspace-editor-mcp-error')).toBeNull()

    textarea.value = 'not json'
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()
    el<HTMLButtonElement>('agent-workspace-editor-mcp-import-format')!.click()
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-mcp-error')).not.toBeNull()
    wrapper.unmount()
  })

  // ── Schedules ─────────────────────────────────────────────────────────────
  // A schedule is manifest state, so the form holds the whole list and Save
  // sends it; the cards read their cron back as a sentence rather than showing
  // one.
  it('states each of the workspace manifest schedules as a card', async () => {
    const wrapper = mountEditor({
      ...demo,
      schedules: [schedule({ lastRun: run({ status: 'failed', reason: 'catch_up', error: 'the agent exited' }) })],
    })
    await wrapper.vm.$nextTick()

    expect(el('agent-workspace-editor-schedule-0-summary')!.textContent).toBe('Every Friday at 09:00')
    expect(el('agent-workspace-editor-schedule-0-next')!.textContent).toBe('not scheduled')
    expect(el('agent-workspace-editor-schedule-0-last-run')!.textContent).toContain('failed · catch-up')
    expect(el('agent-workspace-editor-schedule-0')!.textContent).toContain('the agent exited')
    wrapper.unmount()
  })

  it('adds a card, compiles its shape to cron, and saves the whole list', async () => {
    const wrapper = mountEditor()
    el<HTMLButtonElement>('agent-workspace-editor-schedule-add')!.click()
    await wrapper.vm.$nextTick()

    typeInto('agent-workspace-editor-schedule-0-name', 'Weekly summary')
    await wrapper.vm.$nextTick()
    typeInto('agent-workspace-editor-schedule-0-prompt', 'Summarize the week.')
    await wrapper.vm.$nextTick()

    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()
    expect(savedSchedules(wrapper)).toEqual([{
      id: 'weekly-summary', name: 'Weekly summary', cron: '0 9 * * 5',
      prompt: 'Summarize the week.', disabled: false, onMissed: 'run',
    }])
    wrapper.unmount()
  })

  // The id keys the run history and the scheduler's cursor, so a saved card
  // keeps it however the name changes; the flags a card is not showing ride
  // the save untouched too.
  it('keeps a saved id and its unedited flags across a rename', async () => {
    const wrapper = mountEditor({
      ...demo,
      schedules: [schedule({ disabled: true, onMissed: 'skip' })],
    })
    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-edit')!.click()
    await wrapper.vm.$nextTick()

    typeInto('agent-workspace-editor-schedule-0-name', 'Monday digest')
    await wrapper.vm.$nextTick()
    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()

    expect(savedSchedules(wrapper)).toEqual([{
      id: 'weekly-summary', name: 'Monday digest', cron: '0 9 * * 5',
      prompt: 'Summarize the week.', disabled: true, onMissed: 'skip',
    }])
    wrapper.unmount()
  })

  // A hand-authored entry may name nothing at all. The card is still editable
  // and still saves, and the save must not write `name: <id>` back into the
  // manifest on the user's behalf.
  it('keeps a saved schedule nameless, showing its id instead', async () => {
    const wrapper = mountEditor({ ...demo, schedules: [schedule({ name: '' })] })
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-schedule-0')!.textContent).toContain('weekly-summary')
    expect(el('agent-workspace-editor-schedule-0-problem')).toBeNull()

    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-edit')!.click()
    await wrapper.vm.$nextTick()
    expect(el<HTMLInputElement>('agent-workspace-editor-schedule-0-name')!.placeholder).toBe('weekly-summary')

    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()
    expect(savedSchedules(wrapper)).toEqual([expect.objectContaining({ id: 'weekly-summary', name: '' })])
    wrapper.unmount()
  })

  it('recompiles the cron as the weekly day chips are picked', async () => {
    const wrapper = mountEditor({ ...demo, schedules: [schedule()] })
    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-edit')!.click()
    await wrapper.vm.$nextTick()

    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-day-1')!.click()
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-schedule-0-summary')!.textContent).toBe('Mon, Fri at 09:00')

    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-day-5')!.click()
    await wrapper.vm.$nextTick()
    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()

    expect(savedSchedules(wrapper)).toEqual([expect.objectContaining({ cron: '0 9 * * 1' })])
    wrapper.unmount()
  })

  // Custom is where an expression no shape can state stays editable. Switching
  // to it carries the compiled cron over, so the box opens on what the picker
  // was already saying.
  it('shows the raw cron under Custom and saves what is typed there', async () => {
    const wrapper = mountEditor({ ...demo, schedules: [schedule()] })
    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-edit')!.click()
    await wrapper.vm.$nextTick()

    await chooseOption(wrapper, 'agent-workspace-editor-schedule-0-repeat', 'custom')
    expect(el<HTMLInputElement>('agent-workspace-editor-schedule-0-cron')!.value).toBe('0 9 * * 5')

    typeInto('agent-workspace-editor-schedule-0-cron', '0 */2 * * *')
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-schedule-0-summary')!.textContent).toBe('Custom: 0 */2 * * *')

    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()
    expect(savedSchedules(wrapper)).toEqual([expect.objectContaining({ cron: '0 */2 * * *' })])
    wrapper.unmount()
  })

  // The Go side upserts by id, so two cards on one id would silently drop a
  // schedule. The name is what the id derives from, so the collision is the
  // user's to resolve before the manifest is written.
  it('blocks Save while two cards derive the same id', async () => {
    const wrapper = mountEditor({ ...demo, schedules: [schedule()] })
    el<HTMLButtonElement>('agent-workspace-editor-schedule-add')!.click()
    await wrapper.vm.$nextTick()

    typeInto('agent-workspace-editor-schedule-1-name', 'Weekly Summary')
    await wrapper.vm.$nextTick()
    typeInto('agent-workspace-editor-schedule-1-prompt', 'Again.')
    await wrapper.vm.$nextTick()

    expect(el('agent-workspace-editor-schedule-1-problem')!.textContent)
      .toContain('Another schedule already uses the id "weekly-summary"')
    expect(el<HTMLButtonElement>('agent-workspace-editor-save')!.disabled).toBe(true)

    typeInto('agent-workspace-editor-schedule-1-name', 'Nightly digest')
    await wrapper.vm.$nextTick()
    expect(el<HTMLButtonElement>('agent-workspace-editor-save')!.disabled).toBe(false)
    wrapper.unmount()
  })

  it('blocks Save on the cron and prompt errors the preview reports', async () => {
    const client = scheduleClient()
    client.previewSchedule.mockResolvedValue({ next: [], prompt: '', cronError: 'not a cron', promptError: '' })
    mocks.client = client
    const wrapper = mountEditor({ ...demo, schedules: [schedule()] })
    await flushPromises()

    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-edit')!.click()
    await settlePreview()
    await flushPromises()

    expect(client.previewSchedule).toHaveBeenCalledWith({ workspace: 'demo', cron: '0 9 * * 5', prompt: 'Summarize the week.' })
    expect(el<HTMLButtonElement>('agent-workspace-editor-save')!.disabled).toBe(true)
    wrapper.unmount()
  })

  // The run the Go side answers with is the schedule's newest, so the card
  // shows it as the last run without re-reading the workspace.
  it('runs a schedule now and shows the run it answered with as the last run', async () => {
    const client = scheduleClient()
    mocks.client = client
    const wrapper = mountEditor({ ...demo, schedules: [schedule()] })
    await flushPromises()
    expect(el('agent-workspace-editor-schedule-0-last-run')).toBeNull()

    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-run')!.click()
    await flushPromises()

    expect(client.runSchedule).toHaveBeenCalledWith('demo', 'weekly-summary')
    expect(el('agent-workspace-editor-schedule-0-last-run')!.textContent).toContain('launched · manual')
    wrapper.unmount()
  })

  // A card asks for its history only when it is opened, and the chat a run
  // launched is opened by the area, never by the drawer writing a route.
  it('loads the run history on demand and asks the area to open a run chat', async () => {
    const client = scheduleClient()
    mocks.client = client
    const wrapper = mountEditor({ ...demo, schedules: [schedule()] })
    await flushPromises()
    expect(client.scheduleRuns).not.toHaveBeenCalled()

    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-history')!.click()
    await flushPromises()
    expect(client.scheduleRuns).toHaveBeenCalledWith('demo', 'weekly-summary', 20)

    el<HTMLButtonElement>('agent-workspace-editor-schedule-open-chat-3')!.click()
    expect(wrapper.emitted('open-chat')).toEqual([[9]])
    wrapper.unmount()
  })

  it('removes a card only after its inline confirm is answered', async () => {
    const wrapper = mountEditor({ ...demo, schedules: [schedule()] })
    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-remove')!.click()
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-schedule-0')).not.toBeNull()

    el<HTMLButtonElement>('agent-workspace-editor-schedule-0-remove-confirm-yes')!.click()
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-schedule-0')).toBeNull()

    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()
    expect(savedSchedules(wrapper)).toEqual([])
    wrapper.unmount()
  })
})
