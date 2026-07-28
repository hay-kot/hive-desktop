import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { InboxItem } from '../../types/feed'

const mocks = vi.hoisted(() => ({ SessionLaunchOptions: vi.fn(), CreateSession: vi.fn(), NewSessionDraft: vi.fn() }))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/sessionservice', () => ({ SessionLaunchOptions: mocks.SessionLaunchOptions, CreateSession: mocks.CreateSession }))
vi.mock('../../../bindings/github.com/hay-kot/hive-desktop/internal/adapter/wailsui/pipelineservice', () => ({ NewSessionDraft: mocks.NewSessionDraft }))

import { useNewSession } from '../useNewSession'
import { resetToastsForTests, useToasts } from '../useToasts'

const options = { repositories: [], defaultRepository: 'https://github.com/hay-kot/hive-desktop.git', agents: ['claude'], defaultAgent: 'claude' }
const item = { id: 7 } as InboxItem

beforeEach(() => {
  vi.clearAllMocks()
  resetToastsForTests()
  useNewSession().cancel()
  mocks.SessionLaunchOptions.mockResolvedValue(options)
})

describe('useNewSession', () => {
  it('opens blank with the default repository', async () => {
    const s = useNewSession()
    await s.openBlank()
    expect(s.open.value).toBe(true)
    expect(s.initial.value).toEqual({ repository: options.defaultRepository, name: '', prompt: '' })
  })

  it('prefills from an item draft', async () => {
    mocks.NewSessionDraft.mockResolvedValue({ repository: 'acme/site', name: 'fix-crash', prompt: 'Fix the crash' })
    const s = useNewSession()
    await s.openFromItem(item)
    expect(mocks.NewSessionDraft).toHaveBeenCalledWith(7)
    expect(s.open.value).toBe(true)
    expect(s.initial.value).toEqual({ repository: 'acme/site', name: 'fix-crash', prompt: 'Fix the crash' })
  })

  it('creates the session and closes on submit', async () => {
    mocks.CreateSession.mockResolvedValue({ id: 's1', name: 'fix-crash' })
    const s = useNewSession()
    await s.openBlank()
    await s.submit({ repository: 'acme/site', name: 'fix-crash', prompt: 'go', agent: 'claude' })
    expect(mocks.CreateSession).toHaveBeenCalledWith({ repository: 'acme/site', name: 'fix-crash', prompt: 'go', agent: 'claude' })
    expect(s.open.value).toBe(false)
    expect(useToasts().toasts.value.at(-1)?.message).toContain('fix-crash')
  })

  it('surfaces a create error without closing', async () => {
    mocks.CreateSession.mockRejectedValue(new Error('a session named "fix-crash" already exists'))
    const s = useNewSession()
    await s.openBlank()
    await s.submit({ repository: 'acme/site', name: 'fix-crash', prompt: '' })
    expect(s.open.value).toBe(true)
    expect(s.error.value).toContain('already exists')
  })
})
