import type { UpdateSettingsRequest, SettingsDraft } from '@/features/system/api'
import { sanitizeRechargeBonusTiersForSubmit } from '@/features/payments/model/rechargeBonus'
export function preparePaymentSettings(patch: Pick<UpdateSettingsRequest, 'payment_recharge_bonus_tiers'>, draft: Pick<SettingsDraft, 'payment_recharge_bonus_tiers'>): Partial<UpdateSettingsRequest> {
  return 'payment_recharge_bonus_tiers' in patch ? { payment_recharge_bonus_tiers: sanitizeRechargeBonusTiersForSubmit(draft.payment_recharge_bonus_tiers) } : {}
}
