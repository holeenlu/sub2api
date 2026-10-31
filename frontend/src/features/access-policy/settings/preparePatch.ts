import type { UpdateSettingsRequest, PlatformType } from '@/features/system/api'
import { sanitizePlatformQuotasMap } from './platformQuotas'
export function prepareAccessPolicySettings(patch: Pick<UpdateSettingsRequest, 'default_platform_quotas' | 'openai_fast_policy_settings'>, platformIds: readonly PlatformType[]): Partial<UpdateSettingsRequest> {
  const result: Partial<UpdateSettingsRequest> = {}
  if ('default_platform_quotas' in patch) result.default_platform_quotas = sanitizePlatformQuotasMap(platformIds, patch.default_platform_quotas)
  if (patch.openai_fast_policy_settings) result.openai_fast_policy_settings = { rules: patch.openai_fast_policy_settings.rules.map(rule => {
    const models = (rule.model_whitelist || []).map(model => model.trim()).filter(Boolean)
    return { ...rule, user_ids: rule.user_ids?.length ? [...rule.user_ids] : undefined, error_message: rule.action === 'block' ? rule.error_message : undefined, model_whitelist: models.length ? models : undefined, fallback_action: models.length ? rule.fallback_action || 'pass' : undefined, fallback_error_message: models.length && rule.fallback_action === 'block' ? rule.fallback_error_message : undefined }
  }) }
  return result
}
