import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'

export type UserDefaultsDraft = Pick<SettingsDraft, 'default_balance' | 'default_concurrency' | 'default_user_rpm_limit'>
export const userDefaultsFields = ['default_balance', 'default_concurrency', 'default_user_rpm_limit'] as const
export function projectUserDefaults(settings: SettingsDraft): UserDefaultsDraft { return { default_balance: structuredClone(toRaw(settings.default_balance ?? 0)), default_concurrency: structuredClone(toRaw(settings.default_concurrency ?? 1)), default_user_rpm_limit: structuredClone(toRaw(settings.default_user_rpm_limit ?? 0)) } }
