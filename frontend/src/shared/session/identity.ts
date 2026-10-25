/**
 * Correlates browser work across token rotation without treating claims as authorization.
 * A new login has a new sid; tokens without sid retain their exact-token identity.
 */
export function authSessionStamp(token: string | null): string {
  if (!token) return ''

  try {
    const encoded = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')
    const claims: unknown = JSON.parse(atob(encoded))
    if (typeof claims !== 'object' || claims === null || Array.isArray(claims)) {
      return token
    }

    const userID = 'user_id' in claims ? claims.user_id : undefined
    const role = 'role' in claims ? claims.role : undefined
    const sessionID = 'sid' in claims ? claims.sid : undefined
    const tokenVersion = 'token_version' in claims ? claims.token_version : undefined
    return JSON.stringify([userID, role, sessionID || token, tokenVersion])
  } catch {
    return token
  }
}
