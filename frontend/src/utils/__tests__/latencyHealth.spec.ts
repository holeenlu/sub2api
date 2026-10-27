import { describe, expect, it } from 'vitest'

import { durationSeverity, firstTokenSeverity, formatUsageOutputRate } from '../latencyHealth'
import { formatCacheHitRate, getUsageCacheHitRate, getUsageLatency, usageLatencyExportValues } from '../usageMetrics'

describe('latencyHealth', () => {
  it('classifies first-token latency at 10s/30s/60s boundaries', () => {
    expect(firstTokenSeverity(0)).toBe('good')
    expect(firstTokenSeverity(9_999)).toBe('good')
    expect(firstTokenSeverity(10_000)).toBe('warn')
    expect(firstTokenSeverity(29_999)).toBe('warn')
    expect(firstTokenSeverity(30_000)).toBe('slow')
    expect(firstTokenSeverity(59_999)).toBe('slow')
    expect(firstTokenSeverity(60_000)).toBe('critical')
  })

  it('classifies total duration at 1min/3min/5min boundaries', () => {
    expect(durationSeverity(0)).toBe('good')
    expect(durationSeverity(59_999)).toBe('good')
    expect(durationSeverity(60_000)).toBe('warn')
    expect(durationSeverity(179_999)).toBe('warn')
    expect(durationSeverity(180_000)).toBe('slow')
    expect(durationSeverity(299_999)).toBe('slow')
    expect(durationSeverity(300_000)).toBe('critical')
  })
})

describe('formatUsageOutputRate', () => {
  const row = {
    output_tokens: 1000,
    duration_ms: 20_000,
    image_count: 0,
    image_output_tokens: 0,
    billing_mode: 'token',
  }

  it.each(['sync', 'stream', 'ws_v2', 'cyber', undefined] as const)(
    'uses total duration for %s requests', (request_type) => {
      expect(formatUsageOutputRate({ ...row, request_type })).toBe('50.0 tok/s')
    }
  )

  it('rounds to one decimal place', () => {
    expect(formatUsageOutputRate({ ...row, output_tokens: 101, duration_ms: 3000 })).toBe('33.7 tok/s')
  })

  it('uses upstream duration even when unified timing differs or has an unsupported version', () => {
    const measured = { ...row, latency_metrics: {
      version: 1, first_event_ms: 1000, first_content_ms: 2000, first_text_ms: 3000, duration_ms: 40_000,
    } }
    expect(formatUsageOutputRate(measured)).toBe('50.0 tok/s')
    expect(formatUsageOutputRate({ ...measured, duration_ms: null })).toBe('—')
    const unsupported = { ...measured, latency_metrics: { ...measured.latency_metrics, version: 2 } }
    expect(formatUsageOutputRate(unsupported)).toBe('50.0 tok/s')
  })

  it.each([null, 0, -1, NaN, Infinity])('does not invent a rate for duration %s', (duration_ms) => {
    expect(formatUsageOutputRate({ ...row, duration_ms })).toBe('—')
  })

  it.each([0, -1, NaN, Infinity])('does not invent a rate for output %s', (output_tokens) => {
    expect(formatUsageOutputRate({ ...row, output_tokens })).toBe('—')
  })

  it.each([
    { image_count: 1 },
    { image_output_tokens: 100 },
    { billing_mode: 'image' },
    { request_type: 'live' as const },
    { request_type: 'unknown' as const },
  ])('omits non-text or unknown request rates: %j', (fields) => {
    expect(formatUsageOutputRate({ ...row, ...fields })).toBe('—')
  })
})

describe('usage metric contracts', () => {
  it('uses the upstream first token independently of diagnostic stages in display and export', () => {
    const legacy = { duration_ms: 500, first_token_ms: 20 }
    expect(getUsageLatency(legacy)).toEqual({ measurement: 'legacy', legacy: true, firstEventMs: null, firstContentMs: null, firstTextMs: null, primaryLatencyMs: 20, durationMs: 500 })
    expect(usageLatencyExportValues(legacy)).toEqual(['legacy', '', '', '', 500, 20])
    const measured = { ...legacy, latency_metrics: {
      version: 1, first_event_ms: 10, first_content_ms: null, first_text_ms: null, duration_ms: 900,
    } }
    expect(getUsageLatency(measured)).toEqual({ measurement: 'unified_v1', legacy: false, firstEventMs: 10, firstContentMs: null, firstTextMs: null, primaryLatencyMs: 20, durationMs: 900 })
    expect(usageLatencyExportValues(measured)).toEqual(['unified_v1', 10, '', '', 900, 20])
    expect(getUsageLatency({ ...measured, first_token_ms: 0 }).primaryLatencyMs).toBe(0)
    expect(getUsageLatency({ ...measured, first_token_ms: null }).primaryLatencyMs).toBeNull()
    expect(getUsageLatency({ ...measured, latency_metrics: { ...measured.latency_metrics, first_event_ms: null, first_content_ms: 200 } }).primaryLatencyMs).toBe(20)
    const unsupported = { ...measured, latency_metrics: { ...measured.latency_metrics, version: 2 } }
    expect(getUsageLatency(unsupported)).toEqual({ measurement: 'unsupported', legacy: false, firstEventMs: null, firstContentMs: null, firstTextMs: null, primaryLatencyMs: 20, durationMs: null })
    expect(usageLatencyExportValues(unsupported)).toEqual(['unsupported', '', '', '', '', 20])
  })

  it('uses normalized input buckets without counting creation TTL breakdowns twice', () => {
    const row = { input_tokens: 200, cache_read_tokens: 500, cache_creation_tokens: 300, cache_creation_5m_tokens: 100, cache_creation_1h_tokens: 200 }
    expect(formatCacheHitRate(getUsageCacheHitRate(row))).toBe('50.0%')
    expect(formatCacheHitRate(getUsageCacheHitRate({ ...row, cache_read_tokens: 0 }))).toBe('0.0%')
    expect(formatCacheHitRate(getUsageCacheHitRate({ ...row, billing_status: 'unmetered' }))).toBe('—')
    expect(getUsageCacheHitRate({ input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0 })).toBeNull()
    expect(getUsageCacheHitRate({ ...row, input_tokens: -1 })).toBeNull()
  })
})
