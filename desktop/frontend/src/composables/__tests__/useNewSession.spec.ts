import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { InboxItem } from '../../types/feed'

const mocks = vi.hoisted(() => ({ SessionLaunchOptions: vi.fn(), CreateSession: vi.fn(), NewSessionDraft: vi.fn() }))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({ SessionLaunchOptions: mocks.SessionLaunchOptions, CreateSession: mocks.CreateSession }))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/pipelineservice', () => ({ NewSessionDraft: mocks.NewSessionDraft }))

import { resetNewSessionForTests, useNewSession } from '../useNewSession'
import { resetToastsForTests, useToasts } from '../useToasts'

const options = { repositories: [], defaultRepository: 'https://github.com/hay-kot/hive-desktop.git', agents: ['claude'], defaultAgent: 'claude' }
const item = { id: 7 } as InboxItem

beforeEach(() => {
  vi.clearAllMocks()
  resetToastsForTests()
  resetNewSessionForTests()
  mocks.SessionLaunchOptions.mockResolvedValue(options)
})

describe('useNewSession', () => {
  it('opens blank with the default repository', async () => {
    const s = useNewSession()
    await s.openBlank()
    expect(s.open.value).toBe(true)
    expect(s.initial.value).toEqual({ repository: options.defaultRepository, name: '', prompt: '' })
  })

  it('prefers the repository of the session on screen', async () => {
    const s = useNewSession()
    await s.openBlank('https://github.com/acme/site.git')
    expect(s.initial.value).toEqual({ repository: 'https://github.com/acme/site.git', name: '', prompt: '' })
  })

  it('falls back to the default when the session on screen has no remote', async () => {
    const s = useNewSession()
    await s.openBlank('')
    expect(s.initial.value.repository).toBe(options.defaultRepository)
  })

  it('prefills from an item draft', async () => {
    mocks.NewSessionDraft.mockResolvedValue({ repository: 'acme/site', name: 'fix-crash', prompt: 'Fix the crash' })
    const s = useNewSession()
    await s.openFromItem(item)
    expect(mocks.NewSessionDraft).toHaveBeenCalledWith(7)
    expect(s.open.value).toBe(true)
    expect(s.initial.value).toEqual({ repository: 'acme/site', name: 'fix-crash', prompt: 'Fix the crash' })
  })

  it('reopens instantly from the cached options while the refresh is pending', async () => {
    const s = useNewSession()
    await s.openBlank()
    s.cancel()

    let release!: (opts: typeof options) => void
    mocks.SessionLaunchOptions.mockReturnValue(new Promise((resolve) => { release = resolve }))
    await s.openBlank()
    expect(s.open.value).toBe(true)
    expect(s.options.value).toEqual(options)

    release({ ...options, agents: ['claude', 'codex'] })
    await vi.waitFor(() => expect(s.options.value?.agents).toContain('codex'))
  })

  it('starts the session job and closes on submit', async () => {
    mocks.CreateSession.mockResolvedValue(7)
    const s = useNewSession()
    await s.openBlank()
    await s.submit({ repository: 'acme/site', name: 'fix-crash', prompt: 'go', agent: 'claude' })
    expect(mocks.CreateSession).toHaveBeenCalledWith({ repository: 'acme/site', name: 'fix-crash', prompt: 'go', agent: 'claude', itemId: 0 })
    expect(s.open.value).toBe(false)
    expect(useToasts().toasts.value.at(-1)?.message).toContain('fix-crash')
  })

  // The session a form drafted from an item creates is recorded against that
  // item, so the detail pane can link through to it later.
  it('submits the item a draft came from, and nothing for a blank form', async () => {
    mocks.NewSessionDraft.mockResolvedValue({ repository: 'acme/site', name: 'fix-crash', prompt: 'Fix the crash' })
    mocks.CreateSession.mockResolvedValue(7)
    const s = useNewSession()
    await s.openFromItem(item)
    await s.submit({ repository: 'acme/site', name: 'fix-crash', prompt: 'go' })
    expect(mocks.CreateSession).toHaveBeenCalledWith(expect.objectContaining({ itemId: 7 }))

    await s.openBlank()
    await s.submit({ repository: 'acme/site', name: 'other', prompt: 'go' })
    expect(mocks.CreateSession).toHaveBeenLastCalledWith(expect.objectContaining({ itemId: 0 }))
  })

  it('surfaces a validation error without closing', async () => {
    mocks.CreateSession.mockRejectedValue(new Error('session name is required'))
    const s = useNewSession()
    await s.openBlank()
    await s.submit({ repository: 'acme/site', name: '', prompt: '' })
    expect(s.open.value).toBe(true)
    expect(s.error.value).toContain('session name is required')
  })
})
