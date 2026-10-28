import { toRaw } from 'vue'
import type { NotifyEmailEntry } from '@/features/identity/api/profileTypes';
import type { SettingsDraft } from '@/features/system/api'
export type QuotaNotificationDraft = Pick<SettingsDraft, 'account_quota_notify_enabled' | 'account_quota_notify_emails'>
export const quotaNotificationFields = ['account_quota_notify_enabled', 'account_quota_notify_emails'] as const
export function projectQuotaNotification(settings: SettingsDraft): QuotaNotificationDraft {
  return {
    account_quota_notify_enabled: structuredClone(toRaw(settings.account_quota_notify_enabled ?? (false))),
    account_quota_notify_emails: structuredClone(toRaw(settings.account_quota_notify_emails ?? ([] as NotifyEmailEntry[]))),
  }
}
