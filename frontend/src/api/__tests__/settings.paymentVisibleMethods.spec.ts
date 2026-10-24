import { describe, expect, it } from 'vitest'

import {
  getPaymentVisibleMethodSourceOptions,
  normalizePaymentVisibleMethodSource,
} from '@/api/admin/settings'

describe('admin settings payment visible method helpers', () => {
  it('normalizes aliases into canonical source keys per visible method', () => {
    expect(normalizePaymentVisibleMethodSource('alipay', 'official')).toBe('official_alipay')
    expect(normalizePaymentVisibleMethodSource('alipay', 'alipay_direct')).toBe('official_alipay')
    expect(normalizePaymentVisibleMethodSource('alipay', 'easypay')).toBe('easypay_alipay')

    expect(normalizePaymentVisibleMethodSource('wxpay', 'official')).toBe('official_wxpay')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'wechat')).toBe('official_wxpay')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'easypay')).toBe('easypay_wxpay')
  })

  it('rejects unknown or cross-method source values', () => {
    expect(normalizePaymentVisibleMethodSource('alipay', 'official_wxpay')).toBe('')
    expect(normalizePaymentVisibleMethodSource('wxpay', 'official_alipay')).toBe('')
    expect(normalizePaymentVisibleMethodSource('alipay', 'unknown')).toBe('')
    expect(normalizePaymentVisibleMethodSource('wxpay', null)).toBe('')
  })

  it('exposes method-scoped source options instead of arbitrary strings', () => {
    expect(getPaymentVisibleMethodSourceOptions('alipay')).toEqual([
      {
        value: '',
        labelKey: 'ui.notConfigured',
      },
      {
        value: 'official_alipay',
        labelKey: 'ui.officialAlipay',
      },
      {
        value: 'easypay_alipay',
        labelKey: 'ui.easypayAlipay',
      },
    ])

    expect(getPaymentVisibleMethodSourceOptions('wxpay')).toEqual([
      {
        value: '',
        labelKey: 'ui.notConfigured',
      },
      {
        value: 'official_wxpay',
        labelKey: 'ui.officialWechatPay',
      },
      {
        value: 'easypay_wxpay',
        labelKey: 'ui.easypayWechatPay',
      },
    ])
  })
})
