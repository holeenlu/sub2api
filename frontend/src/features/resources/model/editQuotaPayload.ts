import type { Account } from '@/features/resources/api/accountTypes';
import { supportsAccountQuota } from '@/features/resources/model/accountQuota'
import type { EditSubmissionInput } from './editSubmission'



/** Applies only quota fields to the local outgoing payload. */
export function applyQuotaUpdate(input: Pick<EditSubmissionInput, "editQuotaLimit" | "editQuotaDailyLimit" | "editQuotaWeeklyLimit" | "editDailyResetMode" | "editDailyResetHour" | "editWeeklyResetMode" | "editWeeklyResetDay" | "editWeeklyResetHour" | "editResetTimezone" | "writeQuotaNotifyToExtra">, account: Account, updatePayload: Record<string, unknown>): boolean {
  if (supportsAccountQuota(account.type)) {
    const currentExtra = (updatePayload.extra as Record<string, unknown>) ||
      (account.extra as Record<string, unknown>) || {}
    const newExtra: Record<string, unknown> = { ...currentExtra }
    // 上游倍率自动探测对全部 API-key 平台开放（sub2api 上游即可应答），
    // Bedrock 凭证无静态 Key 不参与。
    if (account.type === 'apikey') {
      delete newExtra.upstream_billing_probe_enabled
      delete newExtra.upstream_billing_rate_sync_enabled
    }
    // Total quota
    if (input.editQuotaLimit.value != null && input.editQuotaLimit.value > 0) {
      newExtra.quota_limit = input.editQuotaLimit.value
    } else {
      delete newExtra.quota_limit
    }
    // Daily quota
    if (input.editQuotaDailyLimit.value != null && input.editQuotaDailyLimit.value > 0) {
      newExtra.quota_daily_limit = input.editQuotaDailyLimit.value
    } else {
      delete newExtra.quota_daily_limit
      delete newExtra.quota_daily_used
      delete newExtra.quota_daily_start
    }
    // Weekly quota
    if (input.editQuotaWeeklyLimit.value != null && input.editQuotaWeeklyLimit.value > 0) {
      newExtra.quota_weekly_limit = input.editQuotaWeeklyLimit.value
    } else {
      delete newExtra.quota_weekly_limit
      delete newExtra.quota_weekly_used
      delete newExtra.quota_weekly_start
    }
    // Quota reset mode config
    if (input.editDailyResetMode.value === 'fixed') {
      newExtra.quota_daily_reset_mode = 'fixed'
      newExtra.quota_daily_reset_hour = input.editDailyResetHour.value ?? 0
    } else {
      delete newExtra.quota_daily_reset_mode
      delete newExtra.quota_daily_reset_hour
    }
    if (input.editWeeklyResetMode.value === 'fixed') {
      newExtra.quota_weekly_reset_mode = 'fixed'
      newExtra.quota_weekly_reset_day = input.editWeeklyResetDay.value ?? 1
      newExtra.quota_weekly_reset_hour = input.editWeeklyResetHour.value ?? 0
    } else {
      delete newExtra.quota_weekly_reset_mode
      delete newExtra.quota_weekly_reset_day
      delete newExtra.quota_weekly_reset_hour
    }
    if (input.editDailyResetMode.value === 'fixed' || input.editWeeklyResetMode.value === 'fixed') {
      newExtra.quota_reset_timezone = input.editResetTimezone.value || 'UTC'
    } else {
      delete newExtra.quota_reset_timezone
    }
    // Quota notify config
    input.writeQuotaNotifyToExtra(newExtra, 'update')
    updatePayload.extra = newExtra
  }
  return true
}
