import { toRaw } from 'vue'
import { resolveWeChatConnectModeCapabilities } from './weChatConnect'
import type { SettingsDraft } from '@/features/system/api'
export type WeChatDraft = Pick<SettingsDraft, 'wechat_connect_enabled' | 'wechat_connect_open_enabled' | 'wechat_connect_open_app_id' | 'wechat_connect_open_app_secret' | 'wechat_connect_open_app_secret_configured' | 'wechat_connect_mp_enabled' | 'wechat_connect_mp_app_id' | 'wechat_connect_mp_app_secret' | 'wechat_connect_mp_app_secret_configured' | 'wechat_connect_mobile_enabled' | 'wechat_connect_mobile_app_id' | 'wechat_connect_mobile_app_secret' | 'wechat_connect_mobile_app_secret_configured' | 'wechat_connect_redirect_url' | 'wechat_connect_frontend_redirect_url' | 'wechat_connect_mode' | 'wechat_connect_scopes'>
export const weChatFields = ['wechat_connect_enabled', 'wechat_connect_open_enabled', 'wechat_connect_open_app_id', 'wechat_connect_open_app_secret', 'wechat_connect_open_app_secret_configured', 'wechat_connect_mp_enabled', 'wechat_connect_mp_app_id', 'wechat_connect_mp_app_secret', 'wechat_connect_mp_app_secret_configured', 'wechat_connect_mobile_enabled', 'wechat_connect_mobile_app_id', 'wechat_connect_mobile_app_secret', 'wechat_connect_mobile_app_secret_configured', 'wechat_connect_redirect_url', 'wechat_connect_frontend_redirect_url', 'wechat_connect_mode', 'wechat_connect_scopes'] as const
export function projectWeChat(settings: SettingsDraft): WeChatDraft {
  const modes = resolveWeChatConnectModeCapabilities(settings.wechat_connect_open_enabled, settings.wechat_connect_mp_enabled, settings.wechat_connect_mobile_enabled, settings.wechat_connect_mode)
  const legacyId = settings.wechat_connect_app_id || ''
  const legacySecretConfigured = settings.wechat_connect_app_secret_configured === true
  return {
    wechat_connect_enabled: structuredClone(toRaw(settings.wechat_connect_enabled ?? (false))),
    wechat_connect_open_enabled: structuredClone(toRaw(modes.openEnabled)),
    wechat_connect_open_app_id: structuredClone(toRaw(settings.wechat_connect_open_app_id || (modes.openEnabled ? legacyId : ""))),
    wechat_connect_open_app_secret: structuredClone(toRaw(settings.wechat_connect_open_app_secret ?? (""))),
    wechat_connect_open_app_secret_configured: structuredClone(toRaw(settings.wechat_connect_open_app_secret_configured || (modes.openEnabled && legacySecretConfigured))),
    wechat_connect_mp_enabled: structuredClone(toRaw(modes.mpEnabled)),
    wechat_connect_mp_app_id: structuredClone(toRaw(settings.wechat_connect_mp_app_id || (modes.mpEnabled ? legacyId : ""))),
    wechat_connect_mp_app_secret: structuredClone(toRaw(settings.wechat_connect_mp_app_secret ?? (""))),
    wechat_connect_mp_app_secret_configured: structuredClone(toRaw(settings.wechat_connect_mp_app_secret_configured || (modes.mpEnabled && legacySecretConfigured))),
    wechat_connect_mobile_enabled: structuredClone(toRaw(modes.mobileEnabled)),
    wechat_connect_mobile_app_id: structuredClone(toRaw(settings.wechat_connect_mobile_app_id || (modes.mobileEnabled ? legacyId : ""))),
    wechat_connect_mobile_app_secret: structuredClone(toRaw(settings.wechat_connect_mobile_app_secret ?? (""))),
    wechat_connect_mobile_app_secret_configured: structuredClone(toRaw(settings.wechat_connect_mobile_app_secret_configured || (modes.mobileEnabled && legacySecretConfigured))),
    wechat_connect_redirect_url: structuredClone(toRaw(settings.wechat_connect_redirect_url ?? (""))),
    wechat_connect_frontend_redirect_url: structuredClone(toRaw(settings.wechat_connect_frontend_redirect_url ?? ("/auth/wechat/callback"))),
    wechat_connect_mode: structuredClone(toRaw(settings.wechat_connect_mode ?? ("open"))),
    wechat_connect_scopes: structuredClone(toRaw(settings.wechat_connect_scopes ?? ("snsapi_login"))),
  }
}
