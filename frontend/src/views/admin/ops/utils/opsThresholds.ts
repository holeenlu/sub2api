export type ThresholdLevel = 'normal' | 'warning' | 'critical'

// TTFT 主要由上游 prefill 决定，长上下文流式请求的 p99 常在数秒到十几秒。
// 与后端 dashboardTTFTDefaultFullScoreMs 保持一致：既是表单默认值，也是未配置时的判级刻度。
export const DEFAULT_TTFT_P99_MS_MAX = 10_000

// 阈值 <= 0 视作"未配置"：后端把 0 当作未设置并回退到默认刻度，前端保持同一口径。
function isConfigured(threshold: number | null | undefined): threshold is number {
  return threshold != null && Number.isFinite(threshold) && threshold > 0
}

// "越低越好"的指标（TTFT、错误率）：达到阈值变红，达到阈值的 80% 变黄。
export function getUpperBoundThresholdLevel(
  value: number | null | undefined,
  threshold: number | null | undefined
): ThresholdLevel {
  if (value == null || !isConfigured(threshold)) return 'normal'
  if (value >= threshold) return 'critical'
  if (value >= threshold * 0.8) return 'warning'
  return 'normal'
}

// SLA 是"越高越好"：低于阈值变红，阈值 +0.1% 的缓冲区内变黄。
export function getSLAThresholdLevel(
  slaPercent: number | null | undefined,
  threshold: number | null | undefined
): ThresholdLevel {
  if (slaPercent == null || !isConfigured(threshold)) return 'normal'
  const warningBuffer = 0.1
  if (slaPercent < threshold) return 'critical'
  if (slaPercent < threshold + warningBuffer) return 'warning'
  return 'normal'
}

// TTFT 是唯一在后端也参与评分的阈值：阈值缺失（加载失败）或被保存成 0 时，后端仍按
// 默认刻度扣分，前端必须用同一把尺子，否则评分掉到风险区而诊断面板一条 TTFT 都不输出。
export function getTTFTThresholdLevel(
  ttftMs: number | null | undefined,
  thresholdMs: number | null | undefined
): ThresholdLevel {
  const effective =
    thresholdMs != null && Number.isFinite(thresholdMs) && thresholdMs > 0
      ? thresholdMs
      : DEFAULT_TTFT_P99_MS_MAX
  return getUpperBoundThresholdLevel(ttftMs, effective)
}

// 诊断面板的错误率刻度与后端 computeBusinessHealth 对齐：<=1% 满分，10% 归零。
// 此前诊断在 0.5% 就报"偏高"，而评分给满分，两边自相矛盾。
export const ERROR_RATE_WARNING_PERCENT = 1
export const ERROR_RATE_CRITICAL_PERCENT = 3
export const UPSTREAM_ERROR_RATE_CRITICAL_PERCENT = 5

export function getDiagnosisErrorRateLevel(errorRatePercent: number | null | undefined): ThresholdLevel {
  if (errorRatePercent == null) return 'normal'
  if (errorRatePercent > ERROR_RATE_CRITICAL_PERCENT) return 'critical'
  if (errorRatePercent > ERROR_RATE_WARNING_PERCENT) return 'warning'
  return 'normal'
}

export function getDiagnosisUpstreamErrorRateLevel(
  upstreamErrorRatePercent: number | null | undefined
): ThresholdLevel {
  if (upstreamErrorRatePercent == null) return 'normal'
  if (upstreamErrorRatePercent > UPSTREAM_ERROR_RATE_CRITICAL_PERCENT) return 'critical'
  if (upstreamErrorRatePercent > ERROR_RATE_WARNING_PERCENT) return 'warning'
  return 'normal'
}
