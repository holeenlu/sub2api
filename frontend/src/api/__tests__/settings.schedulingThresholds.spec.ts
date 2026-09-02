import { describe, expect, it } from 'vitest'

import {
  SCHEDULING_THRESHOLD_PLATFORMS,
  SCHEDULING_THRESHOLD_SCOPES,
  normalizeAccountSchedulingThresholdsMap,
  sanitizeAccountSchedulingThresholdsMap,
} from '@/api/admin/settings'

describe('account scheduling threshold scopes', () => {
  it('exposes anthropic_fable as a non-platform scope on top of the platform list', () => {
    // 后端 AllowedSchedulingThresholdScopes = 平台 + anthropic_fable，两边必须一致，
    // 否则设置页要么少渲染一张卡片，要么提交出后端会 400 的未知 key。
    expect(SCHEDULING_THRESHOLD_SCOPES).toEqual([
      ...SCHEDULING_THRESHOLD_PLATFORMS,
      'anthropic_fable',
    ])
    expect(SCHEDULING_THRESHOLD_PLATFORMS).not.toContain('anthropic_fable')
  })

  it('fills anthropic_fable with 100 when the backend omits it', () => {
    const normalized = normalizeAccountSchedulingThresholdsMap({ anthropic: 90 })

    expect(normalized.anthropic).toBe(90)
    expect(normalized.anthropic_fable).toBe(100)
    for (const scope of SCHEDULING_THRESHOLD_SCOPES) {
      expect(normalized[scope]).toBeTypeOf('number')
    }
  })

  it('keeps and clamps a configured anthropic_fable value', () => {
    expect(normalizeAccountSchedulingThresholdsMap({ anthropic_fable: 50 }).anthropic_fable).toBe(50)
    expect(normalizeAccountSchedulingThresholdsMap({ anthropic_fable: 0 }).anthropic_fable).toBe(1)
    expect(normalizeAccountSchedulingThresholdsMap({ anthropic_fable: 140 }).anthropic_fable).toBe(100)
    expect(normalizeAccountSchedulingThresholdsMap({ anthropic_fable: 62.7 }).anthropic_fable).toBe(62)
  })

  it('carries anthropic_fable through the submit-time sanitizer', () => {
    const payload = sanitizeAccountSchedulingThresholdsMap({ anthropic: 90, anthropic_fable: 50 })

    expect(payload).toMatchObject({ anthropic: 90, anthropic_fable: 50 })
  })
})
