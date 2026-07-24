import { describe, expect, it } from 'vitest'
import { buildTransformPrompt } from '../prompt'

describe('buildTransformPrompt', () => {
  it('embeds the contract, invariants, target shape, and the captured sample', () => {
    const prompt = buildTransformPrompt({ path: 'ci-alerts', sample: '{"event":"deploy","ok":true}' })

    // Self-contained: names the endpoint and the function-node contract.
    expect(prompt).toContain('"ci-alerts"')
    expect(prompt).toContain('function on_message(msg, node, state)')
    expect(prompt).toContain('msg.Payload')

    // The commit-protocol invariants a transform must not break.
    expect(prompt).toContain('never change `msg.Key`, `msg.Topic`')

    // The feed-item target shape the LLM maps into.
    expect(prompt).toContain('"repo": "owner/name"')
    expect(prompt).toContain('"url"')

    // The captured payload is the concrete input sample.
    expect(prompt).toContain('{"event":"deploy","ok":true}')
    expect(prompt).not.toContain('<paste a sample payload here>')

    // Output instruction keeps the reply drop-in for the on_message field.
    expect(prompt).toContain('ONLY the JavaScript statements')
  })

  it('falls back to a placeholder when nothing was captured', () => {
    const prompt = buildTransformPrompt({ path: 'ci' })
    expect(prompt).toContain('<paste a sample payload here>')
  })
})
