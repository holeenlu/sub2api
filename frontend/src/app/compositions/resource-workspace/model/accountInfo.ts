/** Read only recorded credential expiry; never fall back to account or subscription expiry. */
export function credentialExpiryDate(value: unknown): Date | undefined {
  let milliseconds: number
  if (typeof value === 'number' || (typeof value === 'string' && /^\d+(?:\.\d+)?$/.test(value.trim()))) {
    const timestamp = Number(value)
    if (!Number.isFinite(timestamp) || timestamp <= 0) return undefined
    milliseconds = timestamp < 1e12 ? timestamp * 1000 : timestamp
  } else if (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/i.test(value.trim())) {
    milliseconds = Date.parse(value.trim())
  } else {
    return undefined
  }
  return Number.isFinite(milliseconds) && !Number.isNaN(new Date(milliseconds).getTime()) ? new Date(milliseconds) : undefined
}

export function recordedAccountString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

/** Translate only explicit payment channel values; subscription/plan tiers are not payment facts. */
export function paymentMethodLabel(billing: unknown, subscription: unknown, t: (key: string) => string): string | undefined {
  const value = recordedAccountString(billing)
  const labels: Record<string, string> = {
    stripe_subscription: 'stripe', apple_subscription: 'apple', google_subscription: 'google',
    subscription: 'subscription', prepaid: 'prepaid', usage_based: 'usageBased'
  }
  if (value) return labels[value] ? t('admin.accounts.identitySummary.payment.' + labels[value]) : value
  const type = recordedAccountString(subscription)
  return type && labels[type] ? t('admin.accounts.identitySummary.payment.' + labels[type]) : undefined
}
