import { describe, expect, it } from 'vitest'
import { getUsageLatency, getCacheHitRate, formatCacheHitRate } from '../model/metrics'
import { formatUsageOutputRate } from '@/features/usage/model/latencyHealth'
import type { UsageLog } from '../api/types'
const textUsage = { output_tokens: 1000, duration_ms: 20000, image_count: 0, image_output_tokens: 0, billing_mode: 'token', request_type: 'stream' as const }
describe('usage metrics preserve raw duration and unknown facts', () => {
  it('keeps output TPS independent of TTFT and unified total duration', () => {
    const row = { ...textUsage, first_token_ms: 5000, latency_metrics: { version: 1 as const, first_event_ms: 2, first_content_ms: 8, first_text_ms: 12, duration_ms: 40000 } }
    expect(formatUsageOutputRate(row)).toBe('50.0 tok/s')
    expect(getUsageLatency(row).durationMs).toBe(40000)
    expect(getUsageLatency(row).primaryLatencyMs).toBe(5000)
  })
  it.each([null, 0, NaN, Infinity])('does not fabricate a rate for invalid duration %s', duration_ms => {
    expect(formatUsageOutputRate({ ...textUsage, duration_ms })).toBe('—')
  })
  it.each(['unknown', 'live'] as const)('does not label %s as a known text throughput', request_type => {
    expect(formatUsageOutputRate({ ...textUsage, request_type })).toBe('—')
  })
  it('preserves unknown latency and cache share rather than substituting zero', () => {
    const row: Pick<UsageLog, 'duration_ms' | 'first_token_ms' | 'latency_metrics'> = { duration_ms: null, first_token_ms: null }
    expect(getUsageLatency(row).durationMs).toBeNull(); expect(getUsageLatency(row).primaryLatencyMs).toBeNull()
    expect(getCacheHitRate(undefined, 1, 1)).toBeNull(); expect(formatCacheHitRate(null)).toBe('—')
    expect(getCacheHitRate(10, 0, 0)).toBe(0); expect(formatCacheHitRate(0)).toBe('0.0%')
  })
})
