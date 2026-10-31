/** Decode only for browser work correlation; server verification remains authoritative. */
function readClaims(token: string | null): object | undefined {
  if (!token) return undefined
  try {
    const encoded = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')
    const claims: unknown = JSON.parse(atob(encoded))
    return typeof claims === 'object' && claims !== null && !Array.isArray(claims) ? claims : undefined
  } catch {
    return undefined
  }
}

/** Missing claims are not an actor assertion; an explicit invalid actor is unavailable. */
export function tokenActorID(token: string | null): number | null | undefined {
  const claims = readClaims(token)
  if (!claims || !('user_id' in claims)) return undefined
  const value = claims.user_id
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value <= 0) return null
  return value
}

/** A new login has a new sid; tokens without sid retain their exact-token identity. */
export function authSessionStamp(token: string | null): string {
  if (!token) return ''
  const claims = readClaims(token)
  if (!claims) return token
  const userID = 'user_id' in claims ? claims.user_id : undefined
  const role = 'role' in claims ? claims.role : undefined
  const sessionID = 'sid' in claims ? claims.sid : undefined
  const tokenVersion = 'token_version' in claims ? claims.token_version : undefined
  return JSON.stringify([userID, role, sessionID || token, tokenVersion])
}
