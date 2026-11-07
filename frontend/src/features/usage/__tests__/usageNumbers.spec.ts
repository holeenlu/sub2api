import { describe, expect, it, vi } from 'vitest'
import { knownUsageNumber, knownUsageCost, accountUsageCost, formatUsageCost, formatUsageTokens, formatUsageTokenTotal } from '../model/usageNumbers'
import { exportCost } from '../model/exportRows'
vi.mock('@/i18n', () => ({ i18n: { global: { t: (key: string) => key } }, getLocale: () => 'en-US' }))
describe('usage values preserve unknown, lower bounds and amount precision', () => {
  it.each([undefined, null, NaN, Infinity])('does not turn absent or invalid value %s into zero', value => {
    expect(knownUsageNumber(value)).toBe(false); expect(formatUsageCost(value)).toBe('—')
  })
  it.each(['unknown', 'pending', 'pending_pricing', 'unmetered'] as const)('does not label %s billing as free', status => {
    expect(knownUsageCost(0, status)).toBe(false); expect(formatUsageCost(0, status)).toBe('—'); expect(exportCost(0, status, 8)).toBe('')
  })
  it('retains recorded free/settled zeros and explicitly marks a confirmed lower bound', () => {
    expect(formatUsageCost(0, 'free', 8)).toBe('$0.00000000')
    expect(exportCost(0, 'settled', 8)).toBe('0.00000000')
    expect(formatUsageCost(1.23456789, 'lower_bound', 8)).toBe('≥ $1.23456789')
    expect(exportCost(1.23456789, 'lower_bound', 8)).toBe('1.23456789')
  })
  it('retains the existing account-cost basis and multiplier without inventing a missing base', () => {
    expect(accountUsageCost({ total_cost: 3, account_stats_cost: 2, account_rate_multiplier: 1.5 })).toBe(3)
    expect(accountUsageCost({ total_cost: 0 })).toBe(0)
    expect(accountUsageCost({})).toBeNull()
  })
})

describe('token formatting preserves the recorded usage confidence', () => {
  const tokens = { input_tokens: 20, output_tokens: 1000, cache_creation_tokens: 50, cache_read_tokens: 2000 }

  it('marks confirmed lower bounds for individual, compact and total tokens', () => {
    expect(formatUsageTokens(1000, 'lower_bound')).toBe('≥ 1,000')
    expect(formatUsageTokens(2000, 'lower_bound', true)).toBe('≥ 2.0K')
    expect(formatUsageTokenTotal({ ...tokens, billing_status: 'lower_bound' })).toBe('≥ 3,070')
  })

  it.each([undefined, null, NaN, Infinity])('keeps unknown token value %s unknown even for a lower bound', value => {
    expect(formatUsageTokens(value)).toBe('—')
    expect(formatUsageTokens(value, 'unknown')).toBe('—')
    expect(formatUsageTokens(value, 'lower_bound')).toBe('—')
    expect(formatUsageTokens(value, 'lower_bound', true)).toBe('—')
  })

  it.each(['input_tokens', 'output_tokens', 'cache_creation_tokens', 'cache_read_tokens'] as const)(
    'does not claim a total when %s is unknown', field => {
      expect(formatUsageTokenTotal({ ...tokens, [field]: NaN, billing_status: 'lower_bound' })).toBe('—')
    }
  )

  it('keeps known zero distinct from unknown and lower-bound zero', () => {
    const zero = { input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0 }
    expect(formatUsageTokens(0, 'settled')).toBe('0')
    expect(formatUsageTokens(0, 'free')).toBe('0')
    expect(formatUsageTokenTotal({ ...zero, billing_status: 'free' })).toBe('0')
    expect(formatUsageTokens(0, 'lower_bound')).toBe('≥ 0')
    expect(formatUsageTokenTotal({ ...zero, billing_status: 'lower_bound' })).toBe('≥ 0')
    expect(formatUsageTokenTotal(null)).toBe('—')
  })

  it.each([undefined, 'unknown', 'pending', 'pending_pricing', 'settled', 'free'] as const)(
    'does not infer token confidence from the %s settlement status', status => {
      expect(formatUsageTokens(1000, status)).toBe('1,000')
      expect(formatUsageTokens(2000, status, true)).toBe('2.0K')
      expect(formatUsageTokenTotal({ ...tokens, billing_status: status })).toBe('3,070')
    }
  )
})
