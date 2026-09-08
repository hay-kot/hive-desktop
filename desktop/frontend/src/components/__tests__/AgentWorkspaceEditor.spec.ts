import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentWorkspaceEditor from '../AgentWorkspaceEditor.vue'
import { resetAgentWorkspacesForTests, useAgentWorkspaces } from '../../composables/useAgentWorkspaces'
import type { AgentWorkspace, MCPCatalogueEntry, SkillPackage } from '../../lib/agentWorkspacesClient'

const demo: AgentWorkspace = {
  dir: 'demo', name: 'Demo', command: 'claude', danger: false, mcps: [], skills: [], problem: '', notice: '',
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
    props: { workspace },
    attachTo: document.body,
  })
}

async function chooseCommand(wrapper: { vm: { $nextTick: () => Promise<unknown> } }, value: string): Promise<void> {
  el<HTMLButtonElement>('agent-workspace-editor-command-preset')!.click()
  await wrapper.vm.$nextTick()
  el<HTMLButtonElement>(`agent-workspace-editor-command-preset-option-${value}`)!.click()
  await wrapper.vm.$nextTick()
}

beforeEach(() => {
  document.body.innerHTML = ''
  resetAgentWorkspacesForTests()
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

  it('creation offers no delete', () => {
    const wrapper = mountEditor(null)
    expect(el('agent-workspace-editor-delete')).toBeNull()
    expect(el('agent-workspace-editor-save')).not.toBeNull()
    wrapper.unmount()
  })

  it('picking a suggestion sets the command, with no template box in the way', async () => {
    const { presets } = useAgentWorkspaces()
    presets.value = [
      { id: 'claude-ask', agent: 'claude', label: 'Ask', command: 'claude --session-id x', danger: false, source: 'builtin' },
      { id: 'claude-full', agent: 'claude', label: 'Full', command: 'claude --dangerously-skip-permissions', danger: true, source: 'builtin' },
    ]
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    await chooseCommand(wrapper, 'claude-full')
    expect(el('agent-workspace-editor-command-input')).toBeNull()
    expect(el<HTMLElement>('agent-workspace-editor-command-preview')!.textContent)
      .toBe('claude --dangerously-skip-permissions')

    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()
    expect(wrapper.emitted('save')).toEqual([[
      { dir: 'demo', name: 'Demo', command: 'claude --dangerously-skip-permissions', mcps: [], skills: [] },
    ]])
    wrapper.unmount()
  })

  // A hive "fable" profile is a claude row, so the mark is the only thing that
  // says which CLI it runs.
  it('a suggestion carries its agent mark, sized to the row', async () => {
    const { presets } = useAgentWorkspaces()
    presets.value = [
      { id: 'hive-fable', agent: 'claude', label: 'fable', command: 'claude --model fable', danger: false, source: 'hive' },
    ]
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    el<HTMLButtonElement>('agent-workspace-editor-command-preset')!.click()
    await wrapper.vm.$nextTick()

    const row = el<HTMLButtonElement>('agent-workspace-editor-command-preset-option-hive-fable')!
    expect(row.textContent).toContain('fable')
    const mark = row.querySelector('svg')
    expect(mark).not.toBeNull()
    expect(mark!.getAttribute('class')).toContain('size-4')
    wrapper.unmount()
  })

  it('custom reveals the template box, seeded with the command already chosen', async () => {
    const { presets } = useAgentWorkspaces()
    presets.value = [
      { id: 'claude-ask', agent: 'claude', label: 'Ask', command: 'claude --session-id x', danger: false, source: 'builtin' },
    ]
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    await chooseCommand(wrapper, 'claude-ask')
    expect(el('agent-workspace-editor-command-fields')).toBeNull()

    await chooseCommand(wrapper, '__custom__')
    const input = el<HTMLTextAreaElement>('agent-workspace-editor-command-input')!
    expect(input.value).toBe('claude --session-id x')
    expect(el<HTMLElement>('agent-workspace-editor-command-fields')!.textContent).toContain('{{ .SessionID }}')

    input.value = 'claude --session-id x --model opus'
    input.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()
    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()
    expect(wrapper.emitted('save')).toEqual([[
      { dir: 'demo', name: 'Demo', command: 'claude --session-id x --model opus', mcps: [], skills: [] },
    ]])
    wrapper.unmount()
  })

  it('a hand-written command opens the editor on custom', async () => {
    const { presets } = useAgentWorkspaces()
    presets.value = [
      { id: 'claude-ask', agent: 'claude', label: 'Ask', command: 'claude --session-id x', danger: false, source: 'builtin' },
    ]
    const wrapper = mountEditor({ ...demo, command: 'pi --some-flag' })
    await wrapper.vm.$nextTick()

    expect(el<HTMLTextAreaElement>('agent-workspace-editor-command-input')!.value).toBe('pi --some-flag')
    wrapper.unmount()
  })

  // The posture enum used to label its own danger. A free-form command cannot,
  // so the warning is derived from the text — including one typed by hand that
  // no preset offered.
  it('warns about a permission bypass typed into the command', async () => {
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()
    expect(el('agent-workspace-editor-command-danger')).toBeNull()

    await chooseCommand(wrapper, '__custom__')
    const input = el<HTMLTextAreaElement>('agent-workspace-editor-command-input')!
    input.value = 'pi --yolo'
    input.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()

    expect(el('agent-workspace-editor-command-danger')).not.toBeNull()
    wrapper.unmount()
  })

  // The whole point of the schema change: an agent this build ships no preset
  // for is editable and saveable, not refused.
  it('saves a command for an agent with no preset', async () => {
    const { presets } = useAgentWorkspaces()
    presets.value = []
    const wrapper = mountEditor()
    await wrapper.vm.$nextTick()

    const input = el<HTMLTextAreaElement>('agent-workspace-editor-command-input')!
    input.value = 'pi --some-flag'
    input.dispatchEvent(new Event('input'))
    await wrapper.vm.$nextTick()

    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()
    expect(wrapper.emitted('save')).toEqual([[
      { dir: 'demo', name: 'Demo', command: 'pi --some-flag', mcps: [], skills: [] },
    ]])
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
      { dir: 'demo', name: 'Demo', command: 'claude', mcps: ['playwright'], skills: [] },
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
      { dir: 'demo', name: 'Demo', command: 'claude', mcps: [], skills: ['hive', 'infra'] },
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
})
