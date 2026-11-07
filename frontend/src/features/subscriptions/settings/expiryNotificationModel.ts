import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type ExpiryNotificationDraft = Pick<SettingsDraft, 'subscription_expiry_notify_enabled'>
export const expiryNotificationFields = ['subscription_expiry_notify_enabled'] as const
export function projectExpiryNotification(settings: SettingsDraft): ExpiryNotificationDraft {
  return {
    subscription_expiry_notify_enabled: structuredClone(toRaw(settings.subscription_expiry_notify_enabled ?? (true))),
  }
}
