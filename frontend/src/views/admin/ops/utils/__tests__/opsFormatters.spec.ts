import { describe, expect, it } from 'vitest'

import { formatMemorySizeMB, isRecoveredOpsError } from '../opsFormatters'

describe('formatMemorySizeMB', () => {
  it('keeps capacities below one GiB in MB', () => {
    expect(formatMemorySizeMB(61)).toBe('61 MB')
    expect(formatMemorySizeMB(1023)).toBe('1023 MB')
  })

  it('converts capacities at or above one GiB to GB', () => {
    expect(formatMemorySizeMB(1024)).toBe('1 GB')
    expect(formatMemorySizeMB(1536)).toBe('1.5 GB')
    expect(formatMemorySizeMB(32768)).toBe('32 GB')
  })

  it('handles zero and invalid values without producing a misleading unit', () => {
    expect(formatMemorySizeMB(0)).toBe('0 MB')
    expect(formatMemorySizeMB(-1)).toBe('-')
    expect(formatMemorySizeMB(Number.NaN)).toBe('-')
    expect(formatMemorySizeMB(Number.POSITIVE_INFINITY)).toBe('-')
    expect(formatMemorySizeMB(null)).toBe('-')
    expect(formatMemorySizeMB(undefined)).toBe('-')
  })
})


describe('isRecoveredOpsError', () => {
  it('requires a successful request outcome and confirmed recovery', () => {
    expect(isRecoveredOpsError({ request_status_code: 200, message: 'Recovered upstream error 503: overloaded' })).toBe(true)
    expect(isRecoveredOpsError({ request_status_code: 101, message: 'Recovered upstream error 502: EOF' })).toBe(true)
    expect(isRecoveredOpsError({ request_status_code: 200, message: 'Recovered account authentication failure' })).toBe(true)
    expect(isRecoveredOpsError({ request_status_code: 101, message: 'Upstream WebSocket recovery not confirmed' })).toBe(false)
    expect(isRecoveredOpsError({ request_status_code: 502, message: 'Recovered upstream error 503' })).toBe(false)
    expect(isRecoveredOpsError({ message: 'Recovered upstream error 503' })).toBe(false)
  })
})
