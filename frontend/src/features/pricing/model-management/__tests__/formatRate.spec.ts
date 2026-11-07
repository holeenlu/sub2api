import { describe, expect, it } from 'vitest'
import { formatManagedRate } from '../formatRate'
describe('managed rate presentation', () => {
  it('does not expose binary per-token conversion artifacts', () => {
    expect(formatManagedRate(0.09999999999999999, 'en')).toBe('$0.1')
    expect(formatManagedRate(0.19999999999999998, 'zh')).toBe('$0.2')
  })
  it('retains free and tiny known prices as distinct amounts', () => {
    expect(formatManagedRate(0, 'en')).toBe('$0')
    expect(formatManagedRate(1e-15, 'en')).toBe('$0.000000000000001')
  })
  it('preserves editable rates with meaningful fractional precision', () => {
    expect(formatManagedRate(12.3456789, 'en')).toBe('$12.3456789')
  })
})
