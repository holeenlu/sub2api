import type { ProxyQualityCheckResult } from '@/features/resources/api/proxyTypes';

export function proxyQualityStatusKey(status?: string): string {
  const keys: Record<string, string> = {
    healthy: 'qualityStatusHealthy', pass: 'qualityStatusPass',
    warn: 'qualityStatusWarn', fail: 'qualityStatusFail',
    failed: 'qualityStatusFail', challenge: 'qualityStatusChallenge'
  }
  return status && keys[status] ? 'admin.proxies.' + keys[status] : 'common.unknown'
}

export function proxyQualityStatusClass(status?: string): string {
  if (status === 'pass' || status === 'healthy') return 'badge-success'
  if (status === 'warn') return 'badge-warning'
  if (status === 'fail' || status === 'failed' || status === 'challenge') return 'badge-danger'
  return 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
}

export function proxyQualityOverallStatus(report: ProxyQualityCheckResult): string {
  if (report.challenge_count > 0) return 'challenge'
  if (report.failed_count > 0) return 'failed'
  if (report.warn_count > 0) return 'warn'
  return report.passed_count > 0 ? 'healthy' : 'unknown'
}

export function proxyQualityTargetLabel(target: string, t: (key: string) => string): string {
  if (target === 'base_connectivity') return t('admin.proxies.qualityTargetBase')
  const labels: Record<string, string> = {
    openai: 'OpenAI', anthropic: 'Anthropic', gemini: 'Gemini', grok: 'Grok',
    kimi: 'Kimi', zhipu: 'Zhipu GLM', deepseek: 'DeepSeek', minimax: 'MiniMax'
  }
  return labels[target] || target
}
