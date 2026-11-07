import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type BalanceNotificationDraft = Pick<SettingsDraft, 'balance_low_notify_enabled' | 'balance_low_notify_threshold' | 'balance_low_notify_recharge_url'>
export const balanceNotificationFields = ['balance_low_notify_enabled', 'balance_low_notify_threshold', 'balance_low_notify_recharge_url'] as const
export function projectBalanceNotification(settings: SettingsDraft): BalanceNotificationDraft {
  return {
    balance_low_notify_enabled: structuredClone(toRaw(settings.balance_low_notify_enabled ?? (false))),
    balance_low_notify_threshold: structuredClone(toRaw(settings.balance_low_notify_threshold ?? (0))),
    balance_low_notify_recharge_url: structuredClone(toRaw(settings.balance_low_notify_recharge_url ?? (""))),
  }
}
