import type { UsageLog } from '../api/types'
import { formatCostFixed } from '@/shared/lib/format'
import { formatCacheTokens } from '@/shared/lib/formatters'
export function knownUsageNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}
export function knownUsageCost(value: unknown, status?: UsageLog['billing_status']): value is number {
  return knownUsageNumber(value) && (!status || !['unknown', 'pending', 'pending_pricing', 'unmetered'].includes(status))
}
export function formatUsageCost(value: unknown, status?: UsageLog['billing_status'], digits = 6): string {
  if (!knownUsageCost(value, status)) return '—'
  return (status === 'lower_bound' ? '≥ ' : '') + '$' + formatCostFixed(value, digits)
}
export function accountUsageCost(row: { total_cost?: number | null; account_stats_cost?: number | null; account_rate_multiplier?: number | null }): number | null {
  const base = row.account_stats_cost ?? row.total_cost
  if (!knownUsageNumber(base)) return null
  const result = base * (row.account_rate_multiplier ?? 1)
  return Number.isFinite(result) ? result : null
}

export function formatUsageTokens(value: unknown, status?: UsageLog['billing_status'], compact = false): string {
  if (!knownUsageNumber(value)) return '—'
  // Only the explicit backend marker identifies partial usage; settlement readiness does not.
  const formatted = compact ? formatCacheTokens(value) : value.toLocaleString()
  return (status === 'lower_bound' ? '≥ ' : '') + formatted
}
export function formatUsageTokenTotal(row: Pick<UsageLog, 'input_tokens' | 'output_tokens' | 'cache_creation_tokens' | 'cache_read_tokens' | 'billing_status'> | null): string {
  if (!row) return '—'
  const values = [row.input_tokens, row.output_tokens, row.cache_creation_tokens, row.cache_read_tokens]
  return values.every(knownUsageNumber) ? formatUsageTokens(values.reduce((sum, value) => sum + value, 0), row.billing_status) : '—'
}
