import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import SkillsSettingsView from '../SkillsSettingsView.vue'

const mocks = vi.hoisted(() => ({
  Catalog: vi.fn(),
  InstallTarget: vi.fn(),
  UninstallTarget: vi.fn(),
  Sync: vi.fn(),
  SetTargetDir: vi.fn(),
  SetAutoUpdate: vi.fn(),
  SetText: vi.fn(),
}))

vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/skillsservice', () => ({
  Catalog: mocks.Catalog,
  InstallTarget: mocks.InstallTarget,
  UninstallTarget: mocks.UninstallTarget,
  Sync: mocks.Sync,
  SetTargetDir: mocks.SetTargetDir,
  SetAutoUpdate: mocks.SetAutoUpdate,
}))

vi.mock('@wailsio/runtime', () => ({
  Clipboard: { SetText: mocks.SetText },
}))

function skill(id: string) {
  return {
    id,
    name: `hive-${id}`,
    title: `${id} title`,
    description: `${id} description`,
    target: `/config/${id}.yaml`,
    text: `${id} BODY`,
  }
}

function catalog(overrides: Record<string, unknown> = {}) {
  return {
    autoUpdate: true,
    targets: [
      // claude: nothing installed (off). codex: fully installed (on).
      { id: 'claude', label: 'Claude Code', dir: '~/.claude/skills', default: true, installed: 0, needsSync: false },
      { id: 'codex', label: 'OpenAI Codex', dir: '/custom/codex', default: false, installed: 2, needsSync: false },
    ],
    skills: [skill('flows'), skill('actions')],
    ...overrides,
  }
}

function targetResult(overrides: Record<string, unknown> = {}) {
  return { catalog: catalog(), count: 2, kept: 0, ...overrides }
}

function syncResult(overrides: Record<string, unknown> = {}) {
  return { catalog: catalog(), installed: 0, updated: 0, restored: 0, skipped: 0, ...overrides }
}

beforeEach(() => {
  for (const m of Object.values(mocks)) m.mockReset()
  mocks.Catalog.mockResolvedValue(catalog())
  mocks.InstallTarget.mockResolvedValue(targetResult())
  mocks.UninstallTarget.mockResolvedValue(targetResult())
  mocks.Sync.mockResolvedValue(syncResult())
  mocks.SetTargetDir.mockResolvedValue(catalog())
  mocks.SetAutoUpdate.mockResolvedValue(catalog())
  mocks.SetText.mockResolvedValue(undefined)
})

async function mountView() {
  const wrapper = mount(SkillsSettingsView)
  await flushPromises()
  return wrapper
}

describe('SkillsSettingsView', () => {
  it('lists every skill as an informational row with description and surface', async () => {
    const wrapper = await mountView()
    for (const id of ['flows', 'actions']) {
      const row = wrapper.get(`[data-testid="skill-${id}"]`)
      expect(row.text()).toContain(`${id} title`)
      expect(row.text()).toContain(`${id} description`)
      expect(row.text()).toContain(`/config/${id}.yaml`)
      // No per-agent install cells anymore.
      expect(wrapper.find(`[data-testid="skill-${id}-install-claude"]`).exists()).toBe(false)
    }
    // Each agent has a single install toggle.
    expect(wrapper.find('[data-testid="skill-target-claude-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="skill-target-codex-toggle"]').exists()).toBe(true)
  })

  // The skills catalog is this registry-driven prompt list — its Go
  // counterpart is Settings ▸ Skills, described elsewhere as "LLM prompts".
  // Asserting a specific entry here is what makes "a new prompt needs no
  // frontend change" a checked claim rather than an assumption: the
  // agent-workspaces prompt's target is the workspace root this install
  // resolved, not a placeholder.
  it('includes the agent workspaces prompt with its target workspace root', async () => {
    const workspaceRoot = '/home/u/.config/hive/desktop/workspaces'
    mocks.Catalog.mockResolvedValue(catalog({
      skills: [skill('flows'), skill('actions'), {
        id: 'agent-workspaces',
        name: 'hive-agent-workspaces',
        title: 'Agent workspaces',
        description: 'Author or edit an agent workspace.',
        target: workspaceRoot,
        text: 'agent-workspaces BODY',
      }],
    }))
    const wrapper = await mountView()
    const row = wrapper.get('[data-testid="skill-agent-workspaces"]')
    expect(row.text()).toContain('Agent workspaces')
    expect(row.text()).toContain(workspaceRoot)
  })

  it('installs every skill to an agent when its toggle is turned on', async () => {
    const wrapper = await mountView()
    // claude has nothing installed → toggle is off; turning it on installs all.
    await wrapper.get('[data-testid="skill-target-claude-toggle"]').trigger('click')
    await flushPromises()
    expect(mocks.InstallTarget).toHaveBeenCalledWith(expect.anything(), 'claude')
    expect(mocks.UninstallTarget).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="skill-status"]').text()).toContain('Installed 2 skills to Claude Code')
  })

  it('removes every skill from an agent when its toggle is turned off', async () => {
    mocks.UninstallTarget.mockResolvedValue(targetResult({ count: 1, kept: 1 }))
    const wrapper = await mountView()
    // codex is fully installed → toggle is on; turning it off removes all.
    await wrapper.get('[data-testid="skill-target-codex-toggle"]').trigger('click')
    await flushPromises()
    expect(mocks.UninstallTarget).toHaveBeenCalledWith(expect.anything(), 'codex')
    expect(wrapper.get('[data-testid="skill-status"]').text()).toContain('Removed 1 skill from OpenAI Codex')
    expect(wrapper.get('[data-testid="skill-status"]').text()).toContain('kept 1 you edited')
  })

  it('reports what a sync did', async () => {
    mocks.Sync.mockResolvedValue(syncResult({ installed: 3, updated: 1 }))
    const wrapper = await mountView()
    await wrapper.get('[data-testid="skill-sync"]').trigger('click')
    await flushPromises()
    expect(mocks.Sync).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="skill-status"]').text()).toContain('installed 3')
    expect(wrapper.get('[data-testid="skill-status"]').text()).toContain('updated 1')
  })

  it('reports "up to date" when a sync changes nothing', async () => {
    const wrapper = await mountView()
    await wrapper.get('[data-testid="skill-sync"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="skill-status"]').text()).toContain('up to date')
  })

  it('hints when an installed agent needs a sync', async () => {
    mocks.Catalog.mockResolvedValue(catalog({
      targets: [
        { id: 'claude', label: 'Claude Code', dir: '~/.claude/skills', default: true, installed: 0, needsSync: false },
        { id: 'codex', label: 'OpenAI Codex', dir: '/custom/codex', default: false, installed: 1, needsSync: true },
      ],
    }))
    const wrapper = await mountView()
    expect(wrapper.find('[data-testid="skill-target-codex-needssync"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="skill-target-claude-needssync"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="skill-needssync-count"]').text()).toContain('1 AGENT')
  })

  it('edits and resets a target install directory', async () => {
    const wrapper = await mountView()
    const input = wrapper.get('[data-testid="skill-target-codex-dir"]')
    await input.setValue('/new/codex/dir')
    await input.trigger('change')
    await flushPromises()
    expect(mocks.SetTargetDir).toHaveBeenCalledWith(expect.anything(), 'codex', '/new/codex/dir')

    // The default target offers no reset; the overridden one does.
    expect(wrapper.find('[data-testid="skill-target-claude-reset"]').exists()).toBe(false)
    await wrapper.get('[data-testid="skill-target-codex-reset"]').trigger('click')
    await flushPromises()
    expect(mocks.SetTargetDir).toHaveBeenLastCalledWith(expect.anything(), 'codex', '')
  })

  it('toggles auto-sync through the service', async () => {
    const wrapper = await mountView()
    await wrapper.get('[data-testid="skill-autoupdate"]').trigger('click')
    await flushPromises()
    expect(mocks.SetAutoUpdate).toHaveBeenCalledWith(expect.anything(), false)
  })

  it('previews and copies a skill body from the row expander', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('[data-testid="skill-flows-text"]').exists()).toBe(false)
    await wrapper.get('[data-testid="skill-flows-preview"]').trigger('click')
    expect(wrapper.get('[data-testid="skill-flows-text"]').text()).toBe('flows BODY')

    await wrapper.get('[data-testid="skill-flows-copy"]').trigger('click')
    await flushPromises()
    expect(mocks.SetText).toHaveBeenCalledWith('flows BODY')
    expect(wrapper.get('[data-testid="skill-flows-copy-label"]').text()).toBe('Copied')
  })

  it('reports a load failure rather than rendering an empty page', async () => {
    mocks.Catalog.mockRejectedValue(new Error('service unavailable'))
    const wrapper = await mountView()
    expect(wrapper.get('[data-testid="skill-settings-error"]').text()).toContain('service unavailable')
  })
})
