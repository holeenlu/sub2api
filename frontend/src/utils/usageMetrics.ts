import type { UsageLog } from '@/types'

type LatencyRow = Pick<UsageLog, 'duration_ms' | 'first_token_ms' | 'latency_metrics'>

const validMilliseconds = (value: number | null | undefined): number | null =>
  value != null && Number.isFinite(value) && value >= 0 ? value : null

/** Missing stages in a measured request must never inherit the legacy first token. */
export const getUsageLatency = (row: LatencyRow) => {
  const metrics = row.latency_metrics
  const measured = metrics?.version === 1 ? metrics : null
  const measurement = metrics == null ? 'legacy' : measured ? 'unified_v1' : 'unsupported'
  return {
    measurement,
    legacy: metrics == null,
    firstEventMs: validMilliseconds(measured?.first_event_ms),
    firstContentMs: validMilliseconds(measured?.first_content_ms),
    firstTextMs: validMilliseconds(measured?.first_text_ms),
    firstTokenMs: validMilliseconds(metrics == null ? row.first_token_ms : measured?.first_content_ms),
    durationMs: validMilliseconds(metrics == null ? row.duration_ms : measured?.duration_ms),
  }
}

/** Stored input tokens exclude cache reads and creation on every platform. */
export const getCacheHitRate = (
  input: number | null | undefined,
  read: number | null | undefined,
  creation: number | null | undefined,
): number | null => {
  if (input == null || read == null || creation == null
    || [input, read, creation].some(value => !Number.isFinite(value) || value < 0)) return null
  const total = input + read + creation
  return total > 0 && Number.isFinite(total) ? read / total * 100 : null
}

export const getUsageCacheHitRate = (row: Pick<UsageLog,
  'input_tokens' | 'cache_read_tokens' | 'cache_creation_tokens' | 'billing_status'
>): number | null => row.billing_status === 'unmetered'
  ? null : getCacheHitRate(row.input_tokens, row.cache_read_tokens, row.cache_creation_tokens)

export const formatCacheHitRate = (rate: number | null): string =>
  rate == null ? '—' : `${rate.toFixed(1)}%`

// Both usage exports share the same order and keep legacy values in a separate column.
export const usageLatencyExportKeys = [
  'usage.latencyMeasurement', 'usage.firstEventMs', 'usage.firstContentMs',
  'usage.firstTextMs', 'usage.durationMs', 'usage.legacyFirstTokenMs',
] as const

export const usageLatencyExportValues = (row: LatencyRow): Array<string | number> => {
  const latency = getUsageLatency(row)
  return [
    latency.measurement, latency.firstEventMs ?? '',
    latency.firstContentMs ?? '', latency.firstTextMs ?? '', latency.durationMs ?? '',
    latency.legacy ? latency.firstTokenMs ?? '' : '',
  ]
}
