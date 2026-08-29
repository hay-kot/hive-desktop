import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref, type EffectScope } from 'vue'
import {
  filterAndScore,
  scoreCommand,
  sortCommands,
  useCommandPalette,
  useCommands,
  useShellEscape,
  type Command,
} from '../useCommands'

function command(overrides: Partial<Command> & Pick<Command, 'id' | 'title'>): Command {
  return {
    group: 'Profiles',
    run: vi.fn(),
    ...overrides,
  }
}

describe('useCommands', () => {
  let scope: EffectScope

  beforeEach(() => {
    scope = effectScope()
  })

  afterEach(() => {
    scope.stop()
    const palette = useCommandPalette()
    palette.open.value = false
    palette.query.value = ''
    palette.scope.value = 'all'
  })

  it('scores title, keyword, and group matches', () => {
    const cmd = command({
      id: 'open-repo',
      title: 'Open repository',
      group: 'Navigation',
      keywords: ['browser'],
    })

    expect(scoreCommand('', cmd)).toBe(0)
    expect(scoreCommand('open', cmd)).toBe(3)
    expect(scoreCommand('repo', cmd)).toBe(2)
    expect(scoreCommand('browser', cmd)).toBe(1)
    expect(scoreCommand('navigation', cmd)).toBe(1)
    expect(scoreCommand('missing', cmd)).toBe(-1)
  })

  it('sorts commands by group placement, keeping registration order within a group', () => {
    const sorted = sortCommands([
      command({ id: 'profile-work', title: 'Work profile', group: 'Profiles' }),
      command({ id: 'feed-desktop', title: 'Desktop feed', group: 'Feeds' }),
      command({ id: 'profile-personal', title: 'Personal profile', group: 'Profiles' }),
      command({ id: 'session-attach', title: 'Attach session', group: 'Sessions', order: -1 }),
    ])

    expect(sorted.map((cmd) => cmd.id)).toEqual([
      'session-attach',
      'feed-desktop',
      'profile-work',
      'profile-personal',
    ])
  })

  it('ranks a stronger match above an earlier group', () => {
    const results = filterAndScore('profile', [
      command({ id: 'profile-prefix', title: 'Profile settings', group: 'Profiles' }),
      command({ id: 'profile-keyword', title: 'Desktop feed', group: 'Profiles', keywords: ['profile'] }),
      command({ id: 'feed-keyword', title: 'Review inbox', group: 'Feeds', keywords: ['profile'] }),
    ])

    expect(results.map((cmd) => cmd.id)).toEqual(['profile-prefix', 'feed-keyword', 'profile-keyword'])
  })

  it('breaks score ties by group placement', () => {
    const results = filterAndScore('profile', [
      command({ id: 'feed-keyword', title: 'Review inbox', group: 'Feeds', keywords: ['profile'] }),
      command({ id: 'session-keyword', title: 'Attach session', group: 'Sessions', order: -1, keywords: ['profile'] }),
    ])

    expect(results.map((cmd) => cmd.id)).toEqual(['session-keyword', 'feed-keyword'])
  })

  it('registers commands and removes them when the effect scope is disposed', () => {
    const palette = useCommandPalette()

    scope.run(() => useCommands([command({ id: 'alpha', title: 'Alpha command' })]))

    expect(palette.results.value.map((cmd) => cmd.id)).toContain('alpha')

    scope.stop()

    expect(palette.results.value.map((cmd) => cmd.id)).not.toContain('alpha')
  })

  it('updates results from a reactive command source', () => {
    const enabled = ref(false)
    const palette = useCommandPalette()

    scope.run(() => {
      useCommands(() => [
        command({ id: 'always', title: 'Always available' }),
        ...(enabled.value ? [command({ id: 'dynamic', title: 'Dynamic command' })] : []),
      ])
    })

    expect(palette.results.value.map((cmd) => cmd.id)).toEqual(['always'])

    enabled.value = true

    expect(palette.results.value.map((cmd) => cmd.id)).toEqual(['always', 'dynamic'])
  })

  it('runs a palette command, closes the palette, and clears the query', async () => {
    const handler = vi.fn()
    const palette = useCommandPalette()

    scope.run(() => useCommands([command({ id: 'do-thing', title: 'Do thing', run: handler })]))
    palette.open.value = true
    palette.query.value = 'do'

    await palette.run(palette.results.value[0])

    expect(handler).toHaveBeenCalledTimes(1)
    expect(palette.open.value).toBe(false)
    expect(palette.query.value).toBe('')
  })

  it('routes !-queries to the shell escape instead of the fuzzy list', () => {
    const palette = useCommandPalette()

    scope.run(() => {
      useCommands([command({ id: 'decoy', title: '!important looking row' })])
      useShellEscape((line) => [command({ id: 'shell:run', title: `Run: ${line}` })])
    })

    palette.setQuery('!git status')

    expect(palette.scope.value).toBe('shell')
    expect(palette.results.value.map((cmd) => cmd.id)).toEqual(['shell:run'])
    expect(palette.results.value[0].title).toBe('Run: git status')
  })

  it('offers nothing for a bare ! and when every escape declines', () => {
    const palette = useCommandPalette()
    const lines: string[] = []

    scope.run(() => useShellEscape((line) => {
      lines.push(line)
      return []
    }))

    palette.setQuery('!')
    expect(palette.scope.value).toBe('shell')
    expect(palette.results.value).toEqual([])

    palette.setQuery('   ')
    expect(palette.results.value).toEqual([])
    expect(lines).toEqual([])

    palette.setQuery('ls')
    expect(palette.results.value).toEqual([])
    expect(lines).toEqual(['ls'])
  })

  it('releases the shell escape when its scope is disposed', () => {
    const palette = useCommandPalette()

    scope.run(() => useShellEscape((line) => [command({ id: 'shell:run', title: line })]))
    palette.setQuery('!ls')
    expect(palette.results.value).toHaveLength(1)

    scope.stop()

    expect(palette.results.value).toEqual([])
  })
})
