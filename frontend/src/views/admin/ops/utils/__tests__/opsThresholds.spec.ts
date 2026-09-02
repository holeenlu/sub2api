import { describe, expect, it } from 'vitest'

import {
  DEFAULT_TTFT_P99_MS_MAX,
  getDiagnosisErrorRateLevel,
  getDiagnosisUpstreamErrorRateLevel,
  getSLAThresholdLevel,
  getTTFTThresholdLevel,
  getUpperBoundThresholdLevel
} from '../opsThresholds'

describe('getTTFTThresholdLevel', () => {
  it('uses the configured TTFT P99 threshold', () => {
    expect(getTTFTThresholdLevel(14577, 20000)).toBe('normal')
    expect(getTTFTThresholdLevel(16000, 20000)).toBe('warning')
    expect(getTTFTThresholdLevel(20000, 20000)).toBe('critical')
    expect(getTTFTThresholdLevel(22000, 20000)).toBe('critical')
  })

  it('treats a missing metric as normal', () => {
    expect(getTTFTThresholdLevel(null, 20000)).toBe('normal')
    expect(getTTFTThresholdLevel(undefined, 20000)).toBe('normal')
  })

  // 后端对 nil / <=0 的 ttft_p99_ms_max 回落到 10s 默认刻度并照常扣分，
  // 前端若把"未配置"当成永远正常，评分掉到风险区时诊断面板一条 TTFT 都不输出。
  it('falls back to the backend default scale when the threshold is missing or zero', () => {
    expect(DEFAULT_TTFT_P99_MS_MAX).toBe(10000)
    expect(getTTFTThresholdLevel(30000, null)).toBe('critical')
    expect(getTTFTThresholdLevel(30000, undefined)).toBe('critical')
    // 阈值 0 也是"未配置"，不能让 >= 恒真而把一切标红，而要用默认刻度
    expect(getTTFTThresholdLevel(30000, 0)).toBe('critical')
    expect(getTTFTThresholdLevel(9000, 0)).toBe('warning')
    expect(getTTFTThresholdLevel(1200, 0)).toBe('normal')
    expect(getTTFTThresholdLevel(0, 0)).toBe('normal')
  })
})

describe('getUpperBoundThresholdLevel', () => {
  it('grades error rates against their configured maximum', () => {
    expect(getUpperBoundThresholdLevel(1, 5)).toBe('normal')
    expect(getUpperBoundThresholdLevel(4, 5)).toBe('warning')
    expect(getUpperBoundThresholdLevel(5, 5)).toBe('critical')
    expect(getUpperBoundThresholdLevel(2, 0)).toBe('normal')
  })
})

describe('getSLAThresholdLevel', () => {
  it('grades SLA as higher-is-better with a warning buffer', () => {
    expect(getSLAThresholdLevel(99.7, 99.5)).toBe('normal')
    expect(getSLAThresholdLevel(99.55, 99.5)).toBe('warning')
    expect(getSLAThresholdLevel(99.4, 99.5)).toBe('critical')
    expect(getSLAThresholdLevel(99.4, null)).toBe('normal')
    expect(getSLAThresholdLevel(null, 99.5)).toBe('normal')
  })
})

describe('diagnosis error-rate levels', () => {
  it('aligns the request error-rate warning with the score scale at 1%', () => {
    // 线上 0.54% 曾经卡在"诊断说偏高、评分给满分"的矛盾区间里
    expect(getDiagnosisErrorRateLevel(0.54)).toBe('normal')
    expect(getDiagnosisErrorRateLevel(1)).toBe('normal')
    expect(getDiagnosisErrorRateLevel(1.5)).toBe('warning')
    expect(getDiagnosisErrorRateLevel(3.5)).toBe('critical')
    expect(getDiagnosisErrorRateLevel(null)).toBe('normal')
  })

  it('aligns the upstream error-rate warning with the same 1% scale', () => {
    expect(getDiagnosisUpstreamErrorRateLevel(0.54)).toBe('normal')
    expect(getDiagnosisUpstreamErrorRateLevel(1.5)).toBe('warning')
    expect(getDiagnosisUpstreamErrorRateLevel(5.5)).toBe('critical')
    expect(getDiagnosisUpstreamErrorRateLevel(null)).toBe('normal')
  })
})
