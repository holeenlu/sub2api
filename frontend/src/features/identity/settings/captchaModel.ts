import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type CaptchaDraft = Pick<SettingsDraft, 'turnstile_site_key' | 'turnstile_secret_key' | 'turnstile_secret_key_configured' | 'tencent_captcha_region' | 'tencent_captcha_app_id' | 'tencent_captcha_app_secret_key' | 'tencent_captcha_app_secret_key_configured' | 'tencent_captcha_cloud_secret_id' | 'tencent_captcha_cloud_secret_id_configured' | 'tencent_captcha_cloud_secret_key' | 'tencent_captcha_cloud_secret_key_configured' | 'aliyun_captcha_region' | 'aliyun_captcha_prefix' | 'aliyun_captcha_scene_id' | 'aliyun_captcha_access_key_id' | 'aliyun_captcha_access_key_secret' | 'aliyun_captcha_access_key_secret_configured' | 'turnstile_enabled' | 'tencent_captcha_enabled' | 'aliyun_captcha_enabled'>
export const captchaFields = ['turnstile_site_key', 'turnstile_secret_key', 'turnstile_secret_key_configured', 'tencent_captcha_region', 'tencent_captcha_app_id', 'tencent_captcha_app_secret_key', 'tencent_captcha_app_secret_key_configured', 'tencent_captcha_cloud_secret_id', 'tencent_captcha_cloud_secret_id_configured', 'tencent_captcha_cloud_secret_key', 'tencent_captcha_cloud_secret_key_configured', 'aliyun_captcha_region', 'aliyun_captcha_prefix', 'aliyun_captcha_scene_id', 'aliyun_captcha_access_key_id', 'aliyun_captcha_access_key_secret', 'aliyun_captcha_access_key_secret_configured', 'turnstile_enabled', 'tencent_captcha_enabled', 'aliyun_captcha_enabled'] as const
export function projectCaptcha(settings: SettingsDraft): CaptchaDraft {
  return {
    turnstile_site_key: structuredClone(toRaw(settings.turnstile_site_key ?? (""))),
    turnstile_secret_key: structuredClone(toRaw(settings.turnstile_secret_key ?? (""))),
    turnstile_secret_key_configured: structuredClone(toRaw(settings.turnstile_secret_key_configured ?? (false))),
    tencent_captcha_region: structuredClone(toRaw(settings.tencent_captcha_region ?? ("cn"))),
    tencent_captcha_app_id: structuredClone(toRaw(settings.tencent_captcha_app_id ?? (""))),
    tencent_captcha_app_secret_key: structuredClone(toRaw(settings.tencent_captcha_app_secret_key ?? (""))),
    tencent_captcha_app_secret_key_configured: structuredClone(toRaw(settings.tencent_captcha_app_secret_key_configured ?? (false))),
    tencent_captcha_cloud_secret_id: structuredClone(toRaw(settings.tencent_captcha_cloud_secret_id ?? (""))),
    tencent_captcha_cloud_secret_id_configured: structuredClone(toRaw(settings.tencent_captcha_cloud_secret_id_configured ?? (false))),
    tencent_captcha_cloud_secret_key: structuredClone(toRaw(settings.tencent_captcha_cloud_secret_key ?? (""))),
    tencent_captcha_cloud_secret_key_configured: structuredClone(toRaw(settings.tencent_captcha_cloud_secret_key_configured ?? (false))),
    aliyun_captcha_region: structuredClone(toRaw(settings.aliyun_captcha_region ?? ("cn"))),
    aliyun_captcha_prefix: structuredClone(toRaw(settings.aliyun_captcha_prefix ?? (""))),
    aliyun_captcha_scene_id: structuredClone(toRaw(settings.aliyun_captcha_scene_id ?? (""))),
    aliyun_captcha_access_key_id: structuredClone(toRaw(settings.aliyun_captcha_access_key_id ?? (""))),
    aliyun_captcha_access_key_secret: structuredClone(toRaw(settings.aliyun_captcha_access_key_secret ?? (""))),
    aliyun_captcha_access_key_secret_configured: structuredClone(toRaw(settings.aliyun_captcha_access_key_secret_configured ?? (false))),
    turnstile_enabled: structuredClone(toRaw(settings.turnstile_enabled ?? (false))),
    tencent_captcha_enabled: structuredClone(toRaw(settings.tencent_captcha_enabled ?? (false))),
    aliyun_captcha_enabled: structuredClone(toRaw(settings.aliyun_captcha_enabled ?? (false))),
  }
}
