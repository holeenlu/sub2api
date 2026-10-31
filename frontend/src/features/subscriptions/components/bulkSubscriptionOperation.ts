import type { SubscriptionBulkActionRequest } from '@/features/subscriptions/api/admin'

export interface BulkSubscriptionOperation {
  request: SubscriptionBulkActionRequest
  key: string
  storageKey: string | null
  outcomeUncertain: boolean
}

const pendingKeys = new Map<string, string>()

function readStoredKey(storageKey: string): string | null {
  try {
    return globalThis.sessionStorage?.getItem(storageKey) ?? null
  } catch {
    return null
  }
}

function storeKey(storageKey: string, key: string | null) {
  try {
    if (key) globalThis.sessionStorage?.setItem(storageKey, key)
    else globalThis.sessionStorage?.removeItem(storageKey)
  } catch {
    // Keep same-session retries safe in memory when browser storage is unavailable.
  }
}

export function prepareBulkSubscriptionOperation(input: SubscriptionBulkActionRequest, scope: string): BulkSubscriptionOperation {
  // Send the same canonical payload used for storage, including ID order, so a
  // retry from a differently sorted table still matches the backend fingerprint.
  const request: SubscriptionBulkActionRequest = {
    subscription_ids: [...new Set(input.subscription_ids)].sort((a, b) => a - b),
    action: input.action
  }
  if (input.action === 'extend') request.days = input.days
  if (input.action === 'reset_quota') {
    request.daily = !!input.daily
    request.weekly = !!input.weekly
    request.monthly = !!input.monthly
  }
  const storageKey = scope ? `sub2api:admin:subscription-bulk:${scope}:${JSON.stringify(request)}` : null
  let key = storageKey ? pendingKeys.get(storageKey) ?? readStoredKey(storageKey) : null
  const outcomeUncertain = !!key
  if (!key) {
    const requestId = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
    key = `subscription-bulk-${scope ?? 'unknown'}-${requestId}`
  }
  if (storageKey) {
    pendingKeys.set(storageKey, key)
    storeKey(storageKey, key)
  }
  return { request, key, storageKey, outcomeUncertain }
}

export function completeBulkSubscriptionOperation(operation: BulkSubscriptionOperation) {
  if (!operation.storageKey) return
  // A retry of K1 may finish after K1 was confirmed and a new identical intent
  // started as K2. Each completion can retire only its own recovery identity.
  if (pendingKeys.get(operation.storageKey) === operation.key) pendingKeys.delete(operation.storageKey)
  if (readStoredKey(operation.storageKey) === operation.key) storeKey(operation.storageKey, null)
}
