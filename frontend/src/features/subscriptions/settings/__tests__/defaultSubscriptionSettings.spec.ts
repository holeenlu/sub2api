import { describe, expect, it } from 'vitest'
import { normalizeDefaultSubscriptionSettings, prepareDefaultSubscriptionSettings } from '../defaultSubscriptionSettings'
import { prepareSubscriptionSettings } from '../preparePatch'

describe('default subscription settings', () => {
  it('retains filtering, integer conversion and the existing validity range', () => {
    const input = [{ group_id: 2.9, validity_days: 0.5 }, { group_id: 4, validity_days: 50000 }, { group_id: 0, validity_days: 30 }, { group_id: 7, validity_days: 0 }]
    expect(normalizeDefaultSubscriptionSettings(input)).toEqual([{ group_id: 2, validity_days: 1 }, { group_id: 4, validity_days: 36500 }])
    expect(input[0]).toEqual({ group_id: 2.9, validity_days: 0.5 })
    expect(normalizeDefaultSubscriptionSettings(null)).toEqual([])
    expect(normalizeDefaultSubscriptionSettings(undefined)).toEqual([])
  })

  it('reports duplicates after normalization using the actual field name', () => {
    expect(() => prepareDefaultSubscriptionSettings([{ group_id: 2.4, validity_days: 30 }, { group_id: 2.9, validity_days: 60 }], 'auth_source_default_email_subscriptions')).toThrow(expect.objectContaining({ field: 'auth_source_default_email_subscriptions', messageKey: 'admin.settings.defaults.defaultSubscriptionsDuplicate', params: { groupId: 2 } }))
  })

  it('distinguishes omitted subscriptions from an explicit empty collection', () => {
    expect(prepareSubscriptionSettings({})).toEqual({})
    expect(prepareSubscriptionSettings({ default_subscriptions: [] })).toEqual({ default_subscriptions: [] })
  })
})
