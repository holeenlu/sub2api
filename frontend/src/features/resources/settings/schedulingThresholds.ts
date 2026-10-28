import type { AccountSchedulingThresholdsMap, SchedulingThresholdPlatformType, SchedulingThresholdScopeType } from '@/features/system/api'

// 与后端 AllowedSchedulingThresholdPlatforms 保持一致（deepseek 为余额型，
// 走余额检测而非用量阈值；minimax Coding/Token Plan、OpenCode GO 与 Command Code 有滚动窗口）。
export const SCHEDULING_THRESHOLD_PLATFORMS: SchedulingThresholdPlatformType[] = [
  "openai",
  "anthropic",
  "grok",
  "kimi",
  "zhipu",
  "minimax",
  "opencode_go",
  "command_code",
]

// 与后端 AllowedSchedulingThresholdScopes 保持一致：平台 + 非平台 scope。
export const SCHEDULING_THRESHOLD_SCOPES: SchedulingThresholdScopeType[] = [
  ...SCHEDULING_THRESHOLD_PLATFORMS,
  "anthropic_fable",
]

export function normalizeAccountSchedulingThresholdsMap(
  input?: Partial<Record<SchedulingThresholdScopeType, number>> | null,
): AccountSchedulingThresholdsMap {
  const result = {} as AccountSchedulingThresholdsMap
  for (const scope of SCHEDULING_THRESHOLD_SCOPES) {
    const value = input?.[scope]
    result[scope] = typeof value === "number" && Number.isFinite(value)
      ? Math.min(100, Math.max(1, Math.trunc(value)))
      : 100
  }
  return result
}

export function sanitizeAccountSchedulingThresholdsMap(
  input?: Partial<Record<SchedulingThresholdPlatformType, number>> | null,
): AccountSchedulingThresholdsMap {
  return normalizeAccountSchedulingThresholdsMap(input)
}

