import { describe, expect, it } from 'vitest'
import { credentialExpiryDate } from '../accountInfo'

describe('recorded access credential expiry', () => {
  it.each([1793178000, '1793178000', 1793178000000, '2026-10-28T09:00:00Z', '2026-10-28T17:00:00+08:00'])(
    'normalizes existing timestamp formats without changing the instant: %s', value => {
      expect(credentialExpiryDate(value)?.toISOString()).toBe('2026-10-28T09:00:00.000Z')
    })
  it.each([undefined, null, '', ' ', 0, -1, Number.NaN, Number.POSITIVE_INFINITY, {}, [], true, 'not-a-date', '2026-10-28', '2026-10-28T09:00:00', '0xabc'])(
    'keeps absent, invalid or timezone-ambiguous expiry unknown: %s', value => {
      expect(credentialExpiryDate(value)).toBeUndefined()
    })
})
