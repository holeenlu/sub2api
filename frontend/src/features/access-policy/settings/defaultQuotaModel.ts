import { toRaw } from 'vue'
import type { SettingsDraft, PlatformType } from '@/features/system/api'
import { normalizePlatformQuotasMap } from './platformQuotas'
export type DefaultQuotaDraft = Pick<SettingsDraft, 'default_platform_quotas'>
export const defaultQuotaFields = ['default_platform_quotas'] as const
export function projectDefaultQuota(settings: SettingsDraft, platformIds: readonly PlatformType[]): DefaultQuotaDraft { return { default_platform_quotas: structuredClone(toRaw(normalizePlatformQuotasMap(platformIds, settings.default_platform_quotas))) } }
