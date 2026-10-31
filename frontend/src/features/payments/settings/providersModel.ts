import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type ProvidersDraft = Pick<SettingsDraft, 'payment_enabled' | 'payment_enabled_types'>
export const providersFields = ['payment_enabled', 'payment_enabled_types'] as const
export function projectProviders(settings: SettingsDraft): ProvidersDraft {
  return {
    payment_enabled: structuredClone(toRaw(settings.payment_enabled ?? (false))),
    payment_enabled_types: structuredClone(toRaw(settings.payment_enabled_types ?? ([]))),
  }
}
