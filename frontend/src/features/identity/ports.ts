import { inject, type Component, type InjectionKey } from 'vue'
import type { PublicSettings } from '@/features/site/api/publicSettingsTypes';

/** UI effects owned by the application; identity never reads the app store. */
export interface IdentityNotifications {
  showError(message: string): unknown
  showSuccess(message: string): unknown
  showWarning(message: string): unknown
  showInfo(message: string): unknown
}

export type IdentityPublicSettings = Pick<PublicSettings,
  | 'registration_enabled' | 'email_verify_enabled' | 'site_name'
  | 'contact_info' | 'balance_low_notify_enabled' | 'balance_low_notify_threshold'
  | 'registration_email_domain_quota_enabled' | 'registration_email_suffix_whitelist'
  | 'turnstile_enabled' | 'turnstile_site_key'
  | 'tencent_captcha_enabled' | 'tencent_captcha_app_id' | 'tencent_captcha_region'
  | 'aliyun_captcha_enabled' | 'aliyun_captcha_scene_id' | 'aliyun_captcha_prefix' | 'aliyun_captcha_region'
  | 'linuxdo_oauth_enabled' | 'dingtalk_oauth_enabled' | 'backend_mode_enabled'
  | 'wechat_oauth_enabled' | 'wechat_oauth_open_enabled' | 'wechat_oauth_mp_enabled' | 'wechat_oauth_mobile_enabled'
  | 'oidc_oauth_enabled' | 'oidc_oauth_provider_name' | 'github_oauth_enabled' | 'google_oauth_enabled'
  | 'password_reset_enabled' | 'passkey_enabled'
  | 'login_agreement_enabled' | 'login_agreement_mode' | 'login_agreement_revision'
  | 'login_agreement_updated_at' | 'login_agreement_documents'
>

/** A live projection of the one public-settings owner, not another cache. */
export interface IdentitySettings {
  readonly current: IdentityPublicSettings | null
  readonly loaded: boolean
  load(): Promise<IdentityPublicSettings>
}

/** Supplier-origin policy belongs to the app composition. */
export interface IdentityPortal {
  readonly isSupplierPortal: boolean
  safePortalRedirect(value: unknown): string
}

export type IdentityClipboard = (text: string, successMessage?: string) => Promise<boolean>
export const identityClipboardKey: InjectionKey<IdentityClipboard> = Symbol('identity.clipboard')

export const identityNotificationsKey: InjectionKey<IdentityNotifications> = Symbol('identity.notifications')
export const identitySettingsKey: InjectionKey<IdentitySettings> = Symbol('identity.settings')
export const identityPortalKey: InjectionKey<IdentityPortal> = Symbol('identity.portal')
export const identityAuthShellKey: InjectionKey<Component> = Symbol('identity.authShell')

export function useIdentityNotifications(): IdentityNotifications {
  const notifications = inject(identityNotificationsKey)
  if (!notifications) throw new Error('Identity notifications are not provided')
  return notifications
}

export function useIdentitySettings(): IdentitySettings {
  const settings = inject(identitySettingsKey)
  if (!settings) throw new Error('Identity public settings are not provided')
  return settings
}

export function useIdentityPortal(): IdentityPortal {
  const portal = inject(identityPortalKey)
  if (!portal) throw new Error('Identity portal policy is not provided')
  return portal
}

/** The shell accepts the existing default and footer slots. */
export function useIdentityAuthShell(): Component {
  const shell = inject(identityAuthShellKey)
  if (!shell) throw new Error('Identity authentication shell is not provided')
  return shell
}

export function useIdentityClipboard(): IdentityClipboard {
  const copy = inject(identityClipboardKey)
  if (!copy) throw new Error('Identity clipboard is not provided')
  return copy
}
