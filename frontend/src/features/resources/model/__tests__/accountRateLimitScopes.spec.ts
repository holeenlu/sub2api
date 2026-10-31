import { Buffer } from 'node:buffer'
import { describe, expect, it } from 'vitest'
import { isBPSCooldownScope, readActiveAccountRateLimits, readBPSCooldownScope } from '../accountRateLimitScopes'

const now = Date.parse('2026-10-27T00:00:00Z')
const future = '2026-10-28T00:00:00Z'

describe('account cooldown metadata boundary', () => {
  it.each([null, undefined, false, 42, 'text', []])('ignores non-record input %j', input => {
    expect(readActiveAccountRateLimits(input, now)).toEqual([])
  })
  it('keeps native and BPS scopes intact without interpreting private encoded keys', () => {
    const input = Object.freeze({
      'gpt-5': Object.freeze({ rate_limit_reset_at: future }),
      'openai:excel_bps:429:model:Z3B0LTU': Object.freeze({ rate_limit_reset_at: future })
    })
    expect(readActiveAccountRateLimits(input, now)).toEqual([
      { scope: 'gpt-5', resetAt: future },
      { scope: 'openai:excel_bps:429:model:Z3B0LTU', resetAt: future }
    ])
    expect(input['gpt-5']).toEqual({ rate_limit_reset_at: future })
  })
  it('does not treat absent, null, malformed or expired deadlines as active', () => {
    expect(readActiveAccountRateLimits({
      absent: {}, empty: null, array: [], text: 'invalid',
      nullDeadline: { rate_limit_reset_at: null },
      numberDeadline: { rate_limit_reset_at: 1793059200000 },
      malformed: { rate_limit_reset_at: 'invalid' },
      expired: { rate_limit_reset_at: '2026-10-26T23:59:59Z' },
      boundary: { rate_limit_reset_at: '2026-10-27T00:00:00Z' }
    }, now)).toEqual([])
  })
  it.each(['gpt-5', 'AICredits', 'openai:excel_bps', 'my-openai:excel_bps:429'])('does not reclassify public scope %s', scope => {
    expect(isBPSCooldownScope(scope)).toBe(false)
  })
  it('recognizes the reserved namespace without decoding or inventing a target', () => {
    expect(isBPSCooldownScope('openai:excel_bps:429')).toBe(true)
    expect(isBPSCooldownScope('openai:excel_bps:unrecognized')).toBe(true)
  })
})

describe('BPS scope display mapping', () => {
  const prefix = 'openai:excel_bps:'
  const encoded = (value: string) => Buffer.from(value, 'utf8').toString('base64url')
  it('labels the account BPS scope without implying Native or Cloud restrictions', () => {
    expect(readBPSCooldownScope(prefix + '429')).toEqual({ kind: 'account' })
    expect(readBPSCooldownScope('gpt-5')).toBeNull()
  })
  it('keeps model, endpoint and their intersection distinct', () => {
    const model = 'gpt-5', endpoint = '/basispoints/api/responses'
    expect(readBPSCooldownScope(prefix + '429:model:' + encoded(model))).toEqual({ kind: 'model', model })
    expect(readBPSCooldownScope(prefix + '429:endpoint:' + encoded(endpoint))).toEqual({ kind: 'endpoint', endpoint })
    expect(readBPSCooldownScope(prefix + '429:model:' + encoded(model) + ':endpoint:' + encoded(endpoint))).toEqual({ kind: 'model_endpoint', model, endpoint })
    expect(readBPSCooldownScope(prefix + 'model_permission:' + encoded(model))).toEqual({ kind: 'model_permission', model })
  })
  it('preserves a valid Unicode model label', () => {
    const model = '模型-测试'
    expect(readBPSCooldownScope(prefix + '429:model:' + encoded(model))).toEqual({ kind: 'model', model })
  })
  it.each([
    'future-scope', '429:model:', '429:model:A', '429:model:_w',
    '429:endpoint:' + encoded('https://example.test/private?token=x'),
    '429:endpoint:' + encoded('/responses?token=x'),
    '429:model:' + encoded('model' + String.fromCharCode(10) + 'spoof'),
    '429:model:' + encoded(' leading'), '429:model:' + encoded('x'.repeat(1025)),
    'model_permission:', '429:endpoint:' + encoded('/responses') + ':model:' + encoded('gpt-5')
  ])('does not invent a public target for malformed or unknown key %s', suffix => {
    expect(readBPSCooldownScope(prefix + suffix)).toEqual({ kind: 'unknown' })
  })
})
