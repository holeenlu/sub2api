import { describe, expect, it } from 'vitest'
import { currencySymbol, formatPaymentAmount } from '../../model/currency'

describe('formatPaymentAmount', () => {
  it('keeps unknown amounts distinct from an explicit zero', () => {
    expect(formatPaymentAmount(NaN, 'USD')).toBe('—')
    expect(formatPaymentAmount(Infinity, 'USD')).toBe('—')
    expect(formatPaymentAmount(0, 'USD')).toContain('0.00')
  })
  it('uses the currency default fraction digits', () => {
    expect(formatPaymentAmount(100, 'JPY', 'en-US')).not.toContain('.00')
    expect(formatPaymentAmount(100, 'KRW', 'en-US')).not.toContain('.00')
    expect(formatPaymentAmount(100, 'HKD', 'en-US')).toContain('.00')
  })
})

describe('currencySymbol', () => {
  it('maps common payment currencies and falls back safely', () => {
    expect(currencySymbol('USD')).toBe('$')
    expect(currencySymbol('cny')).toBe('¥')
    expect(currencySymbol('EUR')).toBe('€')
    expect(currencySymbol('')).toBe('¥')
    expect(currencySymbol('XYZ')).toBe('XYZ')
  })
})
