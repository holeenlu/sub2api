import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type OidcDraft = Pick<SettingsDraft, 'oidc_connect_enabled' | 'oidc_connect_provider_name' | 'oidc_connect_client_id' | 'oidc_connect_client_secret' | 'oidc_connect_client_secret_configured' | 'oidc_connect_issuer_url' | 'oidc_connect_discovery_url' | 'oidc_connect_authorize_url' | 'oidc_connect_token_url' | 'oidc_connect_userinfo_url' | 'oidc_connect_jwks_url' | 'oidc_connect_scopes' | 'oidc_connect_redirect_url' | 'oidc_connect_frontend_redirect_url' | 'oidc_connect_token_auth_method' | 'oidc_connect_clock_skew_seconds' | 'oidc_connect_allowed_signing_algs' | 'oidc_connect_use_pkce' | 'oidc_connect_validate_id_token' | 'oidc_connect_require_email_verified' | 'oidc_connect_userinfo_email_path' | 'oidc_connect_userinfo_id_path' | 'oidc_connect_userinfo_username_path'>
export const oidcFields = ['oidc_connect_enabled', 'oidc_connect_provider_name', 'oidc_connect_client_id', 'oidc_connect_client_secret', 'oidc_connect_client_secret_configured', 'oidc_connect_issuer_url', 'oidc_connect_discovery_url', 'oidc_connect_authorize_url', 'oidc_connect_token_url', 'oidc_connect_userinfo_url', 'oidc_connect_jwks_url', 'oidc_connect_scopes', 'oidc_connect_redirect_url', 'oidc_connect_frontend_redirect_url', 'oidc_connect_token_auth_method', 'oidc_connect_clock_skew_seconds', 'oidc_connect_allowed_signing_algs', 'oidc_connect_use_pkce', 'oidc_connect_validate_id_token', 'oidc_connect_require_email_verified', 'oidc_connect_userinfo_email_path', 'oidc_connect_userinfo_id_path', 'oidc_connect_userinfo_username_path', 'api_base_url'] as const
export function projectOidc(settings: SettingsDraft): OidcDraft {
  return {
    oidc_connect_enabled: structuredClone(toRaw(settings.oidc_connect_enabled ?? (false))),
    oidc_connect_provider_name: structuredClone(toRaw(settings.oidc_connect_provider_name ?? ("OIDC"))),
    oidc_connect_client_id: structuredClone(toRaw(settings.oidc_connect_client_id ?? (""))),
    oidc_connect_client_secret: structuredClone(toRaw(settings.oidc_connect_client_secret ?? (""))),
    oidc_connect_client_secret_configured: structuredClone(toRaw(settings.oidc_connect_client_secret_configured ?? (false))),
    oidc_connect_issuer_url: structuredClone(toRaw(settings.oidc_connect_issuer_url ?? (""))),
    oidc_connect_discovery_url: structuredClone(toRaw(settings.oidc_connect_discovery_url ?? (""))),
    oidc_connect_authorize_url: structuredClone(toRaw(settings.oidc_connect_authorize_url ?? (""))),
    oidc_connect_token_url: structuredClone(toRaw(settings.oidc_connect_token_url ?? (""))),
    oidc_connect_userinfo_url: structuredClone(toRaw(settings.oidc_connect_userinfo_url ?? (""))),
    oidc_connect_jwks_url: structuredClone(toRaw(settings.oidc_connect_jwks_url ?? (""))),
    oidc_connect_scopes: structuredClone(toRaw(settings.oidc_connect_scopes ?? ("openid email profile"))),
    oidc_connect_redirect_url: structuredClone(toRaw(settings.oidc_connect_redirect_url ?? (""))),
    oidc_connect_frontend_redirect_url: structuredClone(toRaw(settings.oidc_connect_frontend_redirect_url ?? ("/auth/oidc/callback"))),
    oidc_connect_token_auth_method: structuredClone(toRaw(settings.oidc_connect_token_auth_method ?? ("client_secret_post"))),
    oidc_connect_clock_skew_seconds: structuredClone(toRaw(settings.oidc_connect_clock_skew_seconds ?? (120))),
    oidc_connect_allowed_signing_algs: structuredClone(toRaw(settings.oidc_connect_allowed_signing_algs ?? ("RS256,ES256,PS256"))),
    oidc_connect_use_pkce: structuredClone(toRaw(settings.oidc_connect_use_pkce ?? (false))),
    oidc_connect_validate_id_token: structuredClone(toRaw(settings.oidc_connect_validate_id_token ?? (false))),
    oidc_connect_require_email_verified: structuredClone(toRaw(settings.oidc_connect_require_email_verified ?? (false))),
    oidc_connect_userinfo_email_path: structuredClone(toRaw(settings.oidc_connect_userinfo_email_path ?? (""))),
    oidc_connect_userinfo_id_path: structuredClone(toRaw(settings.oidc_connect_userinfo_id_path ?? (""))),
    oidc_connect_userinfo_username_path: structuredClone(toRaw(settings.oidc_connect_userinfo_username_path ?? (""))),
  }
}
