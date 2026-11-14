import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type RegistrationDraft = Pick<SettingsDraft, 'registration_enabled' | 'email_verify_enabled' | 'registration_email_domain_quota_enabled' | 'password_reset_enabled' | 'frontend_url' | 'totp_encryption_key_configured' | 'totp_enabled' | 'passkey_enabled' | 'passkey_configured' | 'passkey_rp_id' | 'passkey_rp_origins' | 'step_up_enabled' | 'session_binding_enabled' | 'audit_log_retention_days' | 'registration_email_suffix_whitelist'>
export const registrationFields = ['registration_enabled', 'email_verify_enabled', 'registration_email_domain_quota_enabled', 'password_reset_enabled', 'frontend_url', 'totp_encryption_key_configured', 'totp_enabled', 'passkey_enabled', 'passkey_configured', 'passkey_rp_id', 'passkey_rp_origins', 'step_up_enabled', 'session_binding_enabled', 'audit_log_retention_days', 'registration_email_suffix_whitelist'] as const
export function projectRegistration(settings: SettingsDraft): RegistrationDraft {
  return {
    registration_enabled: structuredClone(toRaw(settings.registration_enabled ?? (true))),
    email_verify_enabled: structuredClone(toRaw(settings.email_verify_enabled ?? (false))),
    registration_email_domain_quota_enabled: structuredClone(toRaw(settings.registration_email_domain_quota_enabled ?? (false))),
    password_reset_enabled: structuredClone(toRaw(settings.password_reset_enabled ?? (false))),
    frontend_url: structuredClone(toRaw(settings.frontend_url ?? (""))),
    totp_encryption_key_configured: structuredClone(toRaw(settings.totp_encryption_key_configured ?? (false))),
    totp_enabled: structuredClone(toRaw(settings.totp_enabled ?? (false))),
    passkey_enabled: structuredClone(toRaw(settings.passkey_enabled ?? (false))),
    passkey_configured: structuredClone(toRaw(settings.passkey_configured ?? (false))),
    passkey_rp_id: structuredClone(toRaw(settings.passkey_rp_id ?? (""))),
    passkey_rp_origins: structuredClone(toRaw(settings.passkey_rp_origins ?? ([]))),
    step_up_enabled: structuredClone(toRaw(settings.step_up_enabled ?? (false))),
    session_binding_enabled: structuredClone(toRaw(settings.session_binding_enabled ?? (false))),
    audit_log_retention_days: structuredClone(toRaw(settings.audit_log_retention_days ?? (180))),
    registration_email_suffix_whitelist: structuredClone(toRaw(settings.registration_email_suffix_whitelist ?? ([]))),
  }
}
