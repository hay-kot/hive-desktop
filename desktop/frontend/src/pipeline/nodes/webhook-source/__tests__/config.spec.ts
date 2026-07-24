import { describe, expect, it } from 'vitest'
import { defaults, role, type, validate, type Config } from '../config'

describe('webhook-source config', () => {
  it('is a backend-run source node', () => {
    expect(type).toBe('webhook-source')
    expect(role).toBe('source')
    expect(defaults).toEqual({ path: '' })
  })
})

describe('webhook-source validate', () => {
  const ok = (config: Config) => expect(validate(config)).toEqual([])
  const bad = (config: Config, fragment: string) =>
    expect(validate(config).join('\n')).toContain(fragment)

  it('accepts slug paths, nested paths, and printable secrets', () => {
    ok({ path: 'ci-alerts' })
    ok({ path: 'ci/deploys/prod_1' })
    ok({ path: 'ci', secret: 's3cret-token_9' })
  })

  it('requires a path', () => {
    bad({ path: '' }, 'requires a path')
    bad({ path: '   ' }, 'requires a path')
  })

  it('rejects malformed paths', () => {
    bad({ path: 'CI' }, 'lowercase slug')
    bad({ path: '/ci' }, 'lowercase slug')
    bad({ path: 'ci/' }, 'lowercase slug')
    bad({ path: 'ci//x' }, 'lowercase slug')
    bad({ path: '-ci' }, 'lowercase slug')
    bad({ path: 'ci alerts' }, 'lowercase slug')
    bad({ path: 'a'.repeat(129) }, 'caps at 128')
  })

  it('rejects malformed secrets', () => {
    bad({ path: 'ci', secret: 's'.repeat(129) }, 'secret caps at 128')
    bad({ path: 'ci', secret: 'no spaces' }, 'printable ASCII')
  })
})
