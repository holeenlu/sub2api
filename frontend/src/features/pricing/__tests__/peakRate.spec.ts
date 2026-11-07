import { describe, expect, it } from 'vitest'
import { formatPeakRateWindow, hasPeakRate, type PeakRateFields } from '../model/peakRate'

const enabled: PeakRateFields = {
  peak_rate_enabled: true, peak_start: '22:00', peak_end: '06:00', peak_rate_multiplier: 2
}

describe('pricing peak windows', () => {
  it.each([undefined, null, {}, { ...enabled, peak_rate_enabled: false },
    { ...enabled, peak_start: '' }, { ...enabled, peak_end: '' },
    { ...enabled, peak_start: undefined }, { ...enabled, peak_end: undefined }
  ])('hides disabled or incomplete pricing fields: %j', fields => {
    expect(hasPeakRate(fields)).toBe(false)
    expect(formatPeakRateWindow(fields, '+08:00')).toBe('')
  })

  it.each([0, 0.5, 2])('preserves the effective multiplier %s', multiplier => {
    const fields = Object.freeze({ ...enabled, peak_rate_multiplier: multiplier })
    expect(hasPeakRate(fields)).toBe(true)
    expect(formatPeakRateWindow(fields, '+05:30')).toBe(`22:00-06:00 ×${multiplier} (UTC+05:30)`)
  })

  it('defaults only an absent multiplier to one', () => {
    expect(formatPeakRateWindow({ ...enabled, peak_rate_multiplier: undefined })).toBe('22:00-06:00 ×1')
  })

  it.each(['+08:00', '-03:30', '+00:00'])('labels the server offset %s without shifting the window', offset => {
    expect(formatPeakRateWindow(enabled, offset)).toBe(`22:00-06:00 ×2 (UTC${offset})`)
  })

  it.each([undefined, null, ''])('omits the timezone when old public settings have no offset: %j', offset => {
    expect(formatPeakRateWindow(enabled, offset)).toBe('22:00-06:00 ×2')
  })
})
