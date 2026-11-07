import type { OAuthAdoptionDecision, OAuthTokenResponse } from '../api/auth'

export function parseFragmentParams(): URLSearchParams {
  const raw = typeof window !== 'undefined' ? window.location.hash : ''
  const hash = raw.startsWith('#') ? raw.slice(1) : raw
  return new URLSearchParams(hash)
}

export function readLegacyFragmentLogin(params: URLSearchParams): OAuthTokenResponse | null {
  const accessToken = params.get('access_token')?.trim() || ''
  if (!accessToken) {
    return null
  }

  const completion: OAuthTokenResponse = {
    access_token: accessToken
  }
  const refreshToken = params.get('refresh_token')?.trim() || ''
  if (refreshToken) {
    completion.refresh_token = refreshToken
  }
  const expiresIn = Number.parseInt(params.get('expires_in')?.trim() || '', 10)
  if (Number.isFinite(expiresIn) && expiresIn > 0) {
    completion.expires_in = expiresIn
  }
  const tokenType = params.get('token_type')?.trim() || ''
  if (tokenType) {
    completion.token_type = tokenType
  }
  return completion
}

export function sanitizeRedirectPath(path: string | null | undefined): string {
  if (!path) return '/dashboard'
  if (!path.startsWith('/')) return '/dashboard'
  if (path.startsWith('//')) return '/dashboard'
  if (path.includes('://')) return '/dashboard'
  if (path.includes('\n') || path.includes('\r')) return '/dashboard'
  return path
}

export function getRequestErrorMessage(error: unknown, fallback: string): string {
  const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return err.response?.data?.detail || err.response?.data?.message || err.message || fallback
}

export function isCreateAccountRecoveryError(error: unknown): boolean {
  const data = (error as {
    response?: {
      data?: {
        reason?: string
        error?: string
        code?: string
        step?: string
        intent?: string
      }
    }
  }).response?.data
  const states = [data?.reason, data?.error, data?.code, data?.step, data?.intent]
    .map(value => value?.trim().toLowerCase())
    .filter((value): value is string => Boolean(value))

  return states.includes('email_exists') ||
    states.includes('bind_login_required') ||
    states.includes('bind_login') ||
    states.includes('adopt_existing_user_by_email') ||
    states.includes('existing_account_required') ||
    states.includes('existing_account_binding_required')
}

export function serializeAdoptionDecision(decision?: OAuthAdoptionDecision): Record<string, boolean> {
  const payload: Record<string, boolean> = {}
  if (typeof decision?.adoptDisplayName === 'boolean') {
    payload.adopt_display_name = decision.adoptDisplayName
  }
  if (typeof decision?.adoptAvatar === 'boolean') {
    payload.adopt_avatar = decision.adoptAvatar
  }
  return payload
}

export function hasSuggestedProfile(completion: {
  suggested_display_name?: string
  suggested_avatar_url?: string
}): boolean {
  return Boolean(completion.suggested_display_name || completion.suggested_avatar_url)
}
