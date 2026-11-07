import { AUTH_TOKEN_KEY, AUTH_USER_KEY, REFRESH_TOKEN_KEY } from '@/shared/session'

const sessionKeys = new Set([AUTH_TOKEN_KEY, AUTH_USER_KEY, REFRESH_TOKEN_KEY])

/** Owns the browser subscription; identity owns restoring the current snapshot. */
export function observePeerSession(restore: () => void): () => void {
  const onStorage = (event: StorageEvent): void => {
    if (event.storageArea !== window.localStorage) return
    if (event.key === null || sessionKeys.has(event.key)) restore()
  }
  window.addEventListener('storage', onStorage)
  return () => window.removeEventListener('storage', onStorage)
}
