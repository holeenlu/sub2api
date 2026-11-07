import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type RechargeBonusDraft = Pick<SettingsDraft, 'payment_enabled' | 'payment_recharge_bonus_tiers' | 'payment_recharge_bonus_mode' | 'payment_recharge_bonus_notice'>
export const rechargeBonusFields = ['payment_enabled', 'payment_recharge_bonus_tiers', 'payment_recharge_bonus_mode', 'payment_recharge_bonus_notice'] as const
export function projectRechargeBonus(settings: SettingsDraft): RechargeBonusDraft {
  return {
    payment_enabled: structuredClone(toRaw(settings.payment_enabled ?? (false))),
    payment_recharge_bonus_tiers: structuredClone(toRaw(settings.payment_recharge_bonus_tiers ?? ([]))),
    payment_recharge_bonus_mode: structuredClone(toRaw(settings.payment_recharge_bonus_mode ?? ("bonus"))),
    payment_recharge_bonus_notice: structuredClone(toRaw(settings.payment_recharge_bonus_notice ?? (""))),
  }
}
