import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type EmailOAuthDraft = Pick<SettingsDraft, 'github_oauth_enabled' | 'github_oauth_client_id' | 'github_oauth_client_secret' | 'github_oauth_client_secret_configured' | 'github_oauth_redirect_url' | 'github_oauth_frontend_redirect_url' | 'google_oauth_enabled' | 'google_oauth_client_id' | 'google_oauth_client_secret' | 'google_oauth_client_secret_configured' | 'google_oauth_redirect_url' | 'google_oauth_frontend_redirect_url'>
export const emailOAuthFields = ['github_oauth_enabled', 'github_oauth_client_id', 'github_oauth_client_secret', 'github_oauth_client_secret_configured', 'github_oauth_redirect_url', 'github_oauth_frontend_redirect_url', 'google_oauth_enabled', 'google_oauth_client_id', 'google_oauth_client_secret', 'google_oauth_client_secret_configured', 'google_oauth_redirect_url', 'google_oauth_frontend_redirect_url', 'api_base_url'] as const
export function projectEmailOAuth(settings: SettingsDraft): EmailOAuthDraft {
  return {
    github_oauth_enabled: structuredClone(toRaw(settings.github_oauth_enabled ?? (false))),
    github_oauth_client_id: structuredClone(toRaw(settings.github_oauth_client_id ?? (""))),
    github_oauth_client_secret: structuredClone(toRaw(settings.github_oauth_client_secret ?? (""))),
    github_oauth_client_secret_configured: structuredClone(toRaw(settings.github_oauth_client_secret_configured ?? (false))),
    github_oauth_redirect_url: structuredClone(toRaw(settings.github_oauth_redirect_url ?? (""))),
    github_oauth_frontend_redirect_url: structuredClone(toRaw(settings.github_oauth_frontend_redirect_url ?? ("/auth/oauth/callback"))),
    google_oauth_enabled: structuredClone(toRaw(settings.google_oauth_enabled ?? (false))),
    google_oauth_client_id: structuredClone(toRaw(settings.google_oauth_client_id ?? (""))),
    google_oauth_client_secret: structuredClone(toRaw(settings.google_oauth_client_secret ?? (""))),
    google_oauth_client_secret_configured: structuredClone(toRaw(settings.google_oauth_client_secret_configured ?? (false))),
    google_oauth_redirect_url: structuredClone(toRaw(settings.google_oauth_redirect_url ?? (""))),
    google_oauth_frontend_redirect_url: structuredClone(toRaw(settings.google_oauth_frontend_redirect_url ?? ("/auth/oauth/callback"))),
  }
}
