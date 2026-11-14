import { describe, expect, it } from 'vitest'
import { normalizeAccountSchedulingThresholdsMap, sanitizeAccountSchedulingThresholdsMap } from '../schedulingThresholds'

describe('resource scheduling thresholds', () => {
  it('retains the Fable scope and defaults omitted scopes without adding balance-only platforms', () => {
    const result = normalizeAccountSchedulingThresholdsMap({ anthropic_fable: 42 })
    expect(result.anthropic_fable).toBe(42)
    expect(result.anthropic).toBe(100)
    expect(Object.keys(result)).toEqual(['openai', 'anthropic', 'grok', 'kimi', 'zhipu', 'minimax', 'opencode_go', 'command_code', 'anthropic_fable'])
    expect(result).not.toHaveProperty('deepseek')
  })

  it('preserves the existing integer range and non-finite fallback without mutating input', () => {
    const input = Object.freeze({ openai: 0, anthropic: 101, grok: 67.9, kimi: NaN, zhipu: Infinity, minimax: -2, anthropic_fable: 42.9 })
    expect(sanitizeAccountSchedulingThresholdsMap(input)).toEqual({ openai: 1, anthropic: 100, grok: 67, kimi: 100, zhipu: 100, minimax: 1, opencode_go: 100, command_code: 100, anthropic_fable: 42 })
    expect(input.grok).toBe(67.9)
    expect(normalizeAccountSchedulingThresholdsMap(null).openai).toBe(100)
  })
})
