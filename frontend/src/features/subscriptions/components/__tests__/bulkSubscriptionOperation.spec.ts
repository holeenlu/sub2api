import { beforeEach, describe, expect, it } from 'vitest'
import { completeBulkSubscriptionOperation, prepareBulkSubscriptionOperation as prepare } from '../bulkSubscriptionOperation'

let adminId = 1000
const prepareBulkSubscriptionOperation = (request: Parameters<typeof prepare>[0]) => prepare(request, String(adminId))

beforeEach(() => {
  localStorage.setItem('auth_user', JSON.stringify({ id: ++adminId }))
  sessionStorage.clear()
})

describe('bulk subscription operation identity', () => {
  it('does not let a late K1 retry discard the recovery key of a new K2 intent', () => {
    const request = { action: 'extend' as const, subscription_ids: [1], days: 7 }
    const first = prepareBulkSubscriptionOperation(request)
    const lateRetry = prepareBulkSubscriptionOperation(request)
    completeBulkSubscriptionOperation(first)
    const next = prepareBulkSubscriptionOperation(request)
    expect(next.key).not.toBe(first.key)

    completeBulkSubscriptionOperation(lateRetry)
    expect(sessionStorage.getItem(next.storageKey!)).toBe(next.key)
    const resumed = prepareBulkSubscriptionOperation(request)
    expect(resumed.key).toBe(next.key)
    expect(resumed.outcomeUncertain).toBe(true)
  })

  it('preserves a newer stored identity independently of the in-memory entry', () => {
    const request = { action: 'revoke' as const, subscription_ids: [1] }
    const old = prepareBulkSubscriptionOperation(request)
    sessionStorage.setItem(old.storageKey!, 'newer-operation-key')
    completeBulkSubscriptionOperation(old)
    expect(sessionStorage.getItem(old.storageKey!)).toBe('newer-operation-key')
    expect(prepareBulkSubscriptionOperation(request).key).toBe('newer-operation-key')
  })

  it('canonicalizes duplicate and reordered IDs for storage and the sent request', () => {
    const first = prepareBulkSubscriptionOperation({ action: 'extend', subscription_ids: [3, 1, 3], days: 7 })
    const retry = prepareBulkSubscriptionOperation({ subscription_ids: [1, 3], days: 7, action: 'extend' })
    expect(retry.request).toEqual({ action: 'extend', subscription_ids: [1, 3], days: 7 })
    expect(retry.key).toBe(first.key)
    expect(retry.outcomeUncertain).toBe(true)
  })

  it('isolates different parameters, actions, targets, and administrators', () => {
    const request = { action: 'extend' as const, subscription_ids: [1], days: 7 }
    const first = prepareBulkSubscriptionOperation(request)
    const differentDays = prepareBulkSubscriptionOperation({ ...request, days: 14 })
    const differentAction = prepareBulkSubscriptionOperation({ action: 'revoke', subscription_ids: [1] })
    const differentTargets = prepareBulkSubscriptionOperation({ ...request, subscription_ids: [2] })
    localStorage.setItem('auth_user', JSON.stringify({ id: ++adminId }))
    const differentAdmin = prepareBulkSubscriptionOperation(request)
    expect(new Set([first, differentDays, differentAction, differentTargets, differentAdmin].map(operation => operation.key)).size).toBe(5)
  })

  it('reuses a stored pending key and clears both storage and memory after completion', () => {
    const request = { action: 'reset_quota' as const, subscription_ids: [1], daily: true, weekly: false, monthly: false }
    const scope = `sub2api:admin:subscription-bulk:${adminId}:${JSON.stringify({ subscription_ids: [1], action: 'reset_quota', daily: true, weekly: false, monthly: false })}`
    sessionStorage.setItem(scope, 'saved-operation-key')
    const resumed = prepareBulkSubscriptionOperation(request)
    expect(resumed.key).toBe('saved-operation-key')
    completeBulkSubscriptionOperation(resumed)
    expect(sessionStorage.getItem(scope)).toBeNull()
    expect(prepareBulkSubscriptionOperation(request).key).not.toBe(resumed.key)
  })
})
