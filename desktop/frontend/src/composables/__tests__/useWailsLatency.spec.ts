import { describe, expect, it } from 'vitest'
import { percentile } from '../useWailsLatency'

describe('percentile', () => {
  it('takes the nearest rank, so p95 of 40 samples is the second worst', () => {
    const values = Array.from({ length: 40 }, (_, i) => i + 1)
    expect(percentile(values, 50)).toBe(20)
    expect(percentile(values, 95)).toBe(38)
    expect(percentile(values, 100)).toBe(40)
  })

  it('does not depend on the samples arriving sorted', () => {
    expect(percentile([9, 1, 5, 3], 50)).toBe(3)
  })

  it('has nothing to report for an empty run', () => {
    expect(percentile([], 50)).toBe(0)
  })
})
