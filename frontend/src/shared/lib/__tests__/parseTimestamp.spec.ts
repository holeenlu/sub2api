import { describe, expect, it } from 'vitest'
import { parseRfc3339Timestamp } from '../parseTimestamp'

describe('API RFC3339 timestamps', () => {
  it.each([
    '2026-02-29T00:00:00Z', '2026-02-30T00:00:00Z', '2024-04-31T00:00:00Z',
    '2026-10-27T24:00:00Z', '2026-10-27T12:60:00Z', '2026-10-27T12:00:60Z',
    '1900-02-29T00:00:00Z', '2026-00-01T00:00:00Z', '2026-13-01T00:00:00Z',
    '2026-10-00T00:00:00Z', '2026-10-27T12:00:00+24:00', '2026-10-27T12:00:00+08:60',
    '2026-10-27', '0', '', null, 0, undefined
  ])('rejects invalid or incomplete timestamp %j', value => {
    expect(parseRfc3339Timestamp(value)).toBeNull()
  })
  it.each([
    ['2024-02-29T12:34:56Z', '2024-02-29T12:34:56.000Z'],
    ['2000-02-29T00:00:00Z', '2000-02-29T00:00:00.000Z'],
    ['2026-10-27T08:00:00+08:00', '2026-10-27T00:00:00.000Z'],
    ['2026-10-27T00:00:00.123456789Z', '2026-10-27T00:00:00.123Z']
  ])('preserves the instant for %s', (value, expected) => {
    expect(parseRfc3339Timestamp(value)?.toISOString()).toBe(expected)
  })
})
