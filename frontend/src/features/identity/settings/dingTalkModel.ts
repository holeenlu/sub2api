import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type DingTalkDraft = Pick<SettingsDraft, 'dingtalk_connect_enabled' | 'dingtalk_connect_client_id' | 'dingtalk_connect_client_secret' | 'dingtalk_connect_client_secret_configured' | 'dingtalk_connect_redirect_url' | 'dingtalk_connect_corp_restriction_policy' | 'dingtalk_connect_bypass_registration' | 'dingtalk_connect_sync_display_name' | 'dingtalk_connect_sync_display_name_attr_key' | 'dingtalk_connect_sync_display_name_attr_name' | 'dingtalk_connect_sync_corp_email' | 'dingtalk_connect_sync_corp_email_attr_key' | 'dingtalk_connect_sync_corp_email_attr_name' | 'dingtalk_connect_sync_dept' | 'dingtalk_connect_sync_dept_attr_key' | 'dingtalk_connect_sync_dept_attr_name'>
export const dingTalkFields = ['dingtalk_connect_enabled', 'dingtalk_connect_client_id', 'dingtalk_connect_client_secret', 'dingtalk_connect_client_secret_configured', 'dingtalk_connect_redirect_url', 'dingtalk_connect_corp_restriction_policy', 'dingtalk_connect_bypass_registration', 'dingtalk_connect_sync_display_name', 'dingtalk_connect_sync_display_name_attr_key', 'dingtalk_connect_sync_display_name_attr_name', 'dingtalk_connect_sync_corp_email', 'dingtalk_connect_sync_corp_email_attr_key', 'dingtalk_connect_sync_corp_email_attr_name', 'dingtalk_connect_sync_dept', 'dingtalk_connect_sync_dept_attr_key', 'dingtalk_connect_sync_dept_attr_name'] as const
export function projectDingTalk(settings: SettingsDraft): DingTalkDraft {
  return {
    dingtalk_connect_enabled: structuredClone(toRaw(settings.dingtalk_connect_enabled ?? (false))),
    dingtalk_connect_client_id: structuredClone(toRaw(settings.dingtalk_connect_client_id ?? (""))),
    dingtalk_connect_client_secret: structuredClone(toRaw(settings.dingtalk_connect_client_secret ?? (""))),
    dingtalk_connect_client_secret_configured: structuredClone(toRaw(settings.dingtalk_connect_client_secret_configured ?? (false))),
    dingtalk_connect_redirect_url: structuredClone(toRaw(settings.dingtalk_connect_redirect_url ?? (""))),
    dingtalk_connect_corp_restriction_policy: structuredClone(toRaw(settings.dingtalk_connect_corp_restriction_policy ?? ("none"))),
    dingtalk_connect_bypass_registration: structuredClone(toRaw(settings.dingtalk_connect_bypass_registration ?? (false))),
    dingtalk_connect_sync_display_name: structuredClone(toRaw(settings.dingtalk_connect_sync_display_name ?? (false))),
    dingtalk_connect_sync_display_name_attr_key: structuredClone(toRaw(settings.dingtalk_connect_sync_display_name_attr_key ?? ("dingtalk_name"))),
    dingtalk_connect_sync_display_name_attr_name: structuredClone(toRaw(settings.dingtalk_connect_sync_display_name_attr_name ?? (''))),
    dingtalk_connect_sync_corp_email: structuredClone(toRaw(settings.dingtalk_connect_sync_corp_email ?? (false))),
    dingtalk_connect_sync_corp_email_attr_key: structuredClone(toRaw(settings.dingtalk_connect_sync_corp_email_attr_key ?? ("dingtalk_email"))),
    dingtalk_connect_sync_corp_email_attr_name: structuredClone(toRaw(settings.dingtalk_connect_sync_corp_email_attr_name ?? (''))),
    dingtalk_connect_sync_dept: structuredClone(toRaw(settings.dingtalk_connect_sync_dept ?? (false))),
    dingtalk_connect_sync_dept_attr_key: structuredClone(toRaw(settings.dingtalk_connect_sync_dept_attr_key ?? ("dingtalk_department"))),
    dingtalk_connect_sync_dept_attr_name: structuredClone(toRaw(settings.dingtalk_connect_sync_dept_attr_name ?? (''))),
  }
}
