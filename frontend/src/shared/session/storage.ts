import { authSessionStamp } from './identity'

// These keys are the existing browser persistence contract, including across tabs.
export const AUTH_TOKEN_KEY = 'auth_token'
export const AUTH_USER_KEY = 'auth_user'
export const REFRESH_TOKEN_KEY = 'refresh_token'
export const TOKEN_EXPIRES_AT_KEY = 'token_expires_at'

/** An uncached read of identity and token storage, independent of any domain store. */
export interface AuthSessionSnapshot {
  readonly accessToken: string | null
  readonly refreshToken: string | null
  readonly expiresAt: number | null
  readonly userID: number | null
  readonly session: string
}

function readUserID(): number | null {
  const rawUser = localStorage.getItem(AUTH_USER_KEY)
  if (!rawUser) return null

  try {
    const user: unknown = JSON.parse(rawUser)
    if (typeof user !== 'object' || user === null || !('id' in user)) {
      return null
    }

    const id = Number(user.id)
    return Number.isFinite(id) && id > 0 ? id : null
  } catch {
    return null
  }
}

/** Reads a fresh snapshot; it neither restores a login nor owns authorization policy. */
export function readAuthSessionSnapshot(): AuthSessionSnapshot {
  const accessToken = localStorage.getItem(AUTH_TOKEN_KEY)
  const expiresAt = Number(localStorage.getItem(TOKEN_EXPIRES_AT_KEY))

  return {
    accessToken,
    refreshToken: localStorage.getItem(REFRESH_TOKEN_KEY),
    expiresAt: Number.isFinite(expiresAt) && expiresAt > 0 ? expiresAt : null,
    userID: readUserID(),
    session: authSessionStamp(accessToken)
  }
}
