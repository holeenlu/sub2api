import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type SmtpDraft = Pick<SettingsDraft, 'email_verify_enabled' | 'smtp_host' | 'smtp_port' | 'smtp_username' | 'smtp_password' | 'smtp_password_configured' | 'smtp_from_email' | 'smtp_from_name' | 'smtp_use_tls'>
export const smtpFields = ['email_verify_enabled', 'smtp_host', 'smtp_port', 'smtp_username', 'smtp_password', 'smtp_password_configured', 'smtp_from_email', 'smtp_from_name', 'smtp_use_tls'] as const
export function projectSmtp(settings: SettingsDraft): SmtpDraft {
  return {
    email_verify_enabled: structuredClone(toRaw(settings.email_verify_enabled ?? (false))),
    smtp_host: structuredClone(toRaw(settings.smtp_host ?? (""))),
    smtp_port: structuredClone(toRaw(settings.smtp_port ?? (587))),
    smtp_username: structuredClone(toRaw(settings.smtp_username ?? (""))),
    smtp_password: structuredClone(toRaw(settings.smtp_password ?? (""))),
    smtp_password_configured: structuredClone(toRaw(settings.smtp_password_configured ?? (false))),
    smtp_from_email: structuredClone(toRaw(settings.smtp_from_email ?? (""))),
    smtp_from_name: structuredClone(toRaw(settings.smtp_from_name ?? (""))),
    smtp_use_tls: structuredClone(toRaw(settings.smtp_use_tls ?? (true))),
  }
}
