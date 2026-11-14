import type { Account } from '@/features/resources/api/accountTypes';
import { accountSubscriptionPlan } from '@/features/resources'
import { sanitizeUrl } from '@/shared/lib/url'

type AccountDisplay = Pick<Account, 'platform' | 'type' | 'extra' | 'credentials' | 'parent_email' | 'parent_plan_type'>
function record(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined
}

/** Read-only presentation of recorded account facts; never decides scheduling eligibility. */
export function createAccountDisplay(t: (key: string) => string) {
  const GROK_QUOTA_SIGNAL_MAX_AGE_MS = 24 * 60 * 60 * 1000
  const GROK_QUOTA_SIGNAL_MAX_FUTURE_SKEW_MS = 5 * 60 * 1000

  function firstNonBlankString(...values: unknown[]): string | undefined {
    return values.find((value): value is string => (
      typeof value === 'string' && value.trim().length > 0
    ))
  }

  function normalizeGrokPlanKey(value: unknown): string {
    if (typeof value !== 'string') return ''
    return value
      .trim()
      .toLowerCase()
      .replace(/[\s_-]+/g, '')
  }

  function grokPersistedQuotaSnapshot(extra: Record<string, unknown>): Record<string, unknown> | undefined {
    const usage = extra.grok_usage_snapshot
    if (usage && typeof usage === 'object' && !Array.isArray(usage)) {
      return usage as Record<string, unknown>
    }
    const legacy = extra.grok_quota_snapshot
    if (legacy && typeof legacy === 'object' && !Array.isArray(legacy)) {
      return legacy as Record<string, unknown>
    }
    return undefined
  }

  function isGrokQuotaTimestampFresh(raw: unknown): boolean {
    const value = String(raw || '').trim()
    if (!value) return false
    const observedAt = Date.parse(value)
    if (!Number.isFinite(observedAt)) return false
    const age = Date.now() - observedAt
    return age <= GROK_QUOTA_SIGNAL_MAX_AGE_MS && age >= -GROK_QUOTA_SIGNAL_MAX_FUTURE_SKEW_MS
  }

  function isGrok45ResponsesQuotaModel(model: unknown): boolean {
    const value = String(model || '')
      .trim()
      .toLowerCase()
      .replace(/^(x-ai|xai)\//, '')
    return value === 'grok-4.5' || value.startsWith('grok-4.5-')
  }

  function grokQuotaLooksHeavy(snapshot: Record<string, unknown> | undefined): boolean {
    const req = Number(record(snapshot?.requests)?.limit ?? 0)
    const tok = Number(record(snapshot?.tokens)?.limit ?? 0)
    return req >= 8300 || tok >= 53_000_000
  }

  function grok45ResponsesPlanIsHeavy(snapshot: Record<string, unknown> | undefined): boolean {
    if (!snapshot) return false
    const hint = normalizeGrokPlanKey(snapshot.plan_from_45_responses)
    if (hint === 'supergrokheavy' && isGrokQuotaTimestampFresh(snapshot.plan_from_45_responses_at)) {
      return true
    }
    const observedAt = snapshot.last_headers_seen_at || snapshot.updated_at
    return (
      isGrok45ResponsesQuotaModel(snapshot.model) &&
      isGrokQuotaTimestampFresh(observedAt) &&
      grokQuotaLooksHeavy(snapshot)
    )
  }

  // JWT / unambiguous credentials outrank snapshots. SuperGrokPro is ambiguous
  // (covers SuperGrok and Heavy). 8300/53M only upgrades when the window came
  // from grok-4.5 Responses (or a carried 4.5 hint).
  function getAccountPlanType(row: AccountDisplay): string | undefined {
    if (!row) return undefined
    if (row.platform === 'grok') {
      const extra = (row.extra || {})
      const billing = record(extra.grok_billing_snapshot)
      const usage = record(extra.grok_usage_snapshot)
      const legacyQuota = record(extra.grok_quota_snapshot)
      const quota = grokPersistedQuotaSnapshot(extra)
      const cred = firstNonBlankString(row.credentials?.subscription_tier)
      const credKey = normalizeGrokPlanKey(cred)
      if (credKey && credKey !== 'supergrokpro') {
        return cred
      }
      if (
        grok45ResponsesPlanIsHeavy(quota) &&
        (credKey === 'supergrokpro' ||
          normalizeGrokPlanKey(billing?.plan) === 'supergrok' ||
          normalizeGrokPlanKey(billing?.plan) === 'supergrokpro')
      ) {
        return 'SuperGrok Heavy'
      }
      if (credKey === 'supergrokpro') {
        return firstNonBlankString(billing?.plan) || 'SuperGrok'
      }
      return firstNonBlankString(
        billing?.plan,
        usage?.subscription_tier,
        legacyQuota?.subscription_tier,
        extra.subscription_tier,
        row.credentials?.plan_type,
        row.parent_plan_type
      )
    }
    return accountSubscriptionPlan(
      row.platform,
      firstNonBlankString(row.credentials?.plan_type, row.parent_plan_type),
      firstNonBlankString(row.credentials?.rate_limit_tier),
      firstNonBlankString(row.credentials?.subscription_type)
    ) || undefined
  }

  function getOpenAIAuthMode(row: AccountDisplay): string | undefined {
    if (!row || row.platform !== 'openai' || row.type !== 'oauth') return undefined
    const authMode = row.credentials?.auth_mode
    return typeof authMode === 'string' && authMode.trim() ? authMode : undefined
  }

  // Antigravity 订阅等级辅助函数
  function getAntigravityTierFromRow(row: AccountDisplay): string | null {
    if (row.platform !== 'antigravity') return null
    const extra = row.extra
    if (!extra) return null
    const lca = record(extra.load_code_assist)
    if (!lca) return null
    const paid = record(lca.paidTier)
    if (paid && typeof paid.id === 'string') return paid.id
    const current = record(lca.currentTier)
    if (current && typeof current.id === 'string') return current.id
    return null
  }

  function getAntigravityTierLabel(row: AccountDisplay): string | null {
    const tier = getAntigravityTierFromRow(row)
    switch (tier) {
      case 'free-tier': return t('admin.accounts.tier.free')
      case 'g1-pro-tier': return t('admin.accounts.tier.pro')
      case 'g1-ultra-tier': return t('admin.accounts.tier.ultra')
      default: return null
    }
  }

  // 账号显示邮箱:优先账号自身(extra/credentials),影子账号回退母账号 parent_email。
  // 供名称单元格 v-if/标题/文本三处共用,避免同一回退链在模板里重复三次。
  function accountDisplayEmail(row: AccountDisplay): string {
    return firstNonBlankString(row.extra?.email_address, row.extra?.email, row.credentials?.email, row.parent_email) || ''
  }

  function accountHomepageUrl(row: Account): string {
    if (row.type !== 'apikey' || typeof row.credentials?.base_url !== 'string') return ''
    const baseUrl = sanitizeUrl(row.credentials.base_url)
    return baseUrl ? new URL(baseUrl).origin : ''
  }

  function getAntigravityTierClass(row: AccountDisplay): string {
    const tier = getAntigravityTierFromRow(row)
    switch (tier) {
      case 'free-tier': return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
      case 'g1-pro-tier': return 'bg-blue-100 text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
      case 'g1-ultra-tier': return 'bg-purple-100 text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
      default: return ''
    }
  }

  return { getAccountPlanType, getOpenAIAuthMode, getAntigravityTierLabel, getAntigravityTierClass, accountDisplayEmail, accountHomepageUrl }
}
