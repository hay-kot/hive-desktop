import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentWorkspaceEditor from '../AgentWorkspaceEditor.vue'
import { resetAgentWorkspacesForTests, useAgentWorkspaces } from '../../composables/useAgentWorkspaces'
import type { AgentWorkspace, MCPCatalogueEntry, SkillCatalogueEntry } from '../../lib/agentWorkspacesClient'

const demo: AgentWorkspace = {
  dir: 'demo', name: 'Demo', agent: 'claude', autonomy: 'ask', mcps: [], skills: [], problem: '', notice: '',
}

const playwright: MCPCatalogueEntry = {
  id: 'playwright', title: 'Playwright', description: 'Browser automation', shipped: true,
  stability: 'stable', shadows: '', transport: 'stdio', command: 'npx -y @playwright/mcp@latest', problem: '',
}

const hiveMCP: SkillCatalogueEntry = {
  slug: 'hive-mcp', title: 'Hive MCP', description: 'Drive this install.', shipped: true, shadows: '',
}

const releaseNotes: SkillCatalogueEntry = {
  slug: 'release-notes', title: 'release-notes', description: 'Draft release notes.', shipped: false, shadows: '',
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
      { dir: 'demo', name: 'Demo', agent: 'claude', autonomy: 'full', mcps: [], skills: [] },
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
      { dir: 'demo', name: 'Demo', agent: 'claude', autonomy: 'ask', mcps: ['playwright'], skills: [] },
    ]])
    wrapper.unmount()
  })

  it('save carries the toggled skills list', async () => {
    const { skillCatalogue } = useAgentWorkspaces()
    skillCatalogue.value = [hiveMCP, releaseNotes]
    const wrapper = mountEditor({ ...demo, skills: ['hive-mcp'] })
    await wrapper.vm.$nextTick()

    // The library skill is offered but off until this workspace switches it
    // on — the library is shared, the toggle is the workspace's own.
    el<HTMLButtonElement>('agent-workspace-editor-skill-release-notes')!.click()
    await wrapper.vm.$nextTick()
    el<HTMLButtonElement>('agent-workspace-editor-save')!.click()

    expect(wrapper.emitted('save')).toEqual([[
      { dir: 'demo', name: 'Demo', agent: 'claude', autonomy: 'ask', mcps: [], skills: ['hive-mcp', 'release-notes'] },
    ]])
    wrapper.unmount()
  })

  it('a declared skill slug the catalogue no longer resolves still rows, marked missing', async () => {
    const { skillCatalogue } = useAgentWorkspaces()
    skillCatalogue.value = [hiveMCP]
    const wrapper = mountEditor({ ...demo, skills: ['deleted-skill'] })
    await wrapper.vm.$nextTick()

    const row = el<HTMLElement>('agent-workspace-editor-skills')!
    expect(el('agent-workspace-editor-skill-deleted-skill')).not.toBeNull()
    expect(row.textContent).toContain('not in the catalogue')
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
