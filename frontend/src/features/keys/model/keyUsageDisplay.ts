import { formatCostFixed } from '@/shared/lib/format'

/** Missing/non-finite observations are unknown, while a recorded zero is known. */
export function isKnownKeyMetric(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}
export function formatKeyCost(value: unknown, fractionDigits = 4): string {
  return isKnownKeyMetric(value) ? '$' + formatCostFixed(value, fractionDigits) : '—'
}
