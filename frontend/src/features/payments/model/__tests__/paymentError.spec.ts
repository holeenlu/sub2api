import { describe, expect, it, vi } from 'vitest'
import { extractPaymentErrorMessage } from '../paymentError'

const labels: Record<string, string> = {
  'admin.settings.payment.field_certSerial': '证书序列号',
  'admin.settings.payment.field_apiV3Key': 'API v3 密钥',
}
function translate(key: string, params?: Record<string, unknown>): string {
  if (key === 'payment.errors.MISSING_FIELD') return '缺少：' + params?.key
  if (key === 'payment.errors.MISSING_FIELDS') return '缺少：' + params?.keys
  return labels[key] ?? key
}

describe('extractPaymentErrorMessage', () => {
  it('localizes the provider field without mutating server metadata', () => {
    const metadata = Object.freeze({ key: 'certSerial', provider: 'unchanged', limit: 0 })
    expect(extractPaymentErrorMessage({ code: 400, reason: 'MISSING_FIELD', metadata }, translate, 'fallback')).toBe('缺少：证书序列号')
    expect(metadata).toEqual({ key: 'certSerial', provider: 'unchanged', limit: 0 })
  })
  it('localizes slash-separated fields and preserves unknown field names', () => {
    expect(extractPaymentErrorMessage({ reason: 'MISSING_FIELDS', metadata: { keys: 'certSerial/apiV3Key/newField' } }, translate, 'fallback'))
      .toBe('缺少：证书序列号 / API v3 密钥 / newField')
  })
  it('respects translation existence checks and preserves non-string metadata', () => {
    const t = Object.assign(vi.fn((key: string, params?: Record<string, unknown>) => key === 'payment.errors.RAW' ? JSON.stringify(params) : 'translated'), { te: () => false })
    const metadata = { key: 'newField', keys: ['certSerial'], amount: 0, enabled: false, optional: null }
    expect(extractPaymentErrorMessage({ reason: 'RAW', metadata }, t, 'fallback')).toBe(JSON.stringify(metadata))
  })
  it.each([
    [{ reason: 'UNMAPPED', message: 'server detail', metadata: { key: 'certSerial' } }, 'server detail'],
    [{ response: { data: { detail: 'axios detail' } } }, 'axios detail'],
    [new Error('network detail'), 'network detail'],
    ['plain error', 'plain error'],
    [null, 'fallback'],
  ])('keeps generic message fallback for %s', (error, expected) => {
    expect(extractPaymentErrorMessage(error, translate, 'fallback')).toBe(expected)
  })
})
