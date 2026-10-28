import catalog from '../../../../../backend/internal/service/scheduling_quota_defaults.json'

export type SchedulingQuotaProtection = typeof catalog.protection
export type SchedulingQuotaNumberKey = keyof typeof catalog.limits
export const schedulingQuotaNumberFields = Object.entries(catalog.limits).map(([key, bounds]) => ({
  key: key as SchedulingQuotaNumberKey,
  ...bounds
}))
export function schedulingQuotaPreset() {
  return {
    protection: { ...catalog.protection, anthropic_tier_thresholds_enabled: true },
    openai: { ...catalog.openai }
  }
}
export function validSchedulingQuotaProtection(value: SchedulingQuotaProtection): boolean {
  return schedulingQuotaNumberFields.every(({ key, min, max }) => Number.isFinite(value[key]) && value[key] >= min && value[key] <= max)
}
