import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { CanceledError } from 'axios'
import { readAuthSessionSnapshot } from '@/shared/session'
import type { RefreshTokenResponse } from '../../api/auth'
import type { AuthResponse } from '../../api/types'
import type { User } from '../../api/profileTypes'
import { useAuthStore } from '../auth'

const api = vi.hoisted(() => ({
  refreshToken: vi.fn<() => Promise<RefreshTokenResponse>>(),
  getCurrentUser: vi.fn<() => Promise<{ data: User }>>(),
  login: vi.fn<() => Promise<AuthResponse>>(),
  logout: vi.fn<() => Promise<void>>()
}))

vi.mock('../../api/auth', () => ({ authAPI: api, isTotp2FARequired: () => false }))

// Synthetic browser-session fixtures; no real credentials or network are used.
const user: User = {
  id: 1, username: 'fixture', email: 'fixture@example.com', role: 'user',
  balance: 100, concurrency: 5, status: 'active', allowed_groups: null,
  balance_notify_enabled: false, balance_notify_threshold: null, balance_notify_extra_emails: [],
  created_at: '2024-01-01', updated_at: '2024-01-01'
}

function tokens(jti: string, withSessionID = true): RefreshTokenResponse {
  const claims = { user_id: user.id, role: user.role, jti, ...(withSessionID ? { sid: 'fixture-session' } : {}) }
  return {
    access_token: `header.${btoa(JSON.stringify(claims))}.signature`,
    refresh_token: `fixture-refresh-${jti}`,
    expires_in: 600,
    token_type: 'Bearer'
  }
}

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolvePromise: ((value: T) => void) | undefined
  const promise = new Promise<T>(resolve => { resolvePromise = resolve })
  return {
    promise,
    resolve(value: T): void {
      if (!resolvePromise) throw new Error('Deferred promise was not initialized')
      resolvePromise(value)
    }
  }
}

/** Model the existing coordinator's commit order, before its promise returns to identity. */
function persist(pair: RefreshTokenResponse): number {
  const expiresAt = Date.now() + pair.expires_in * 1000
  localStorage.setItem('auth_token', pair.access_token)
  localStorage.setItem('token_expires_at', String(expiresAt))
  localStorage.setItem('refresh_token', pair.refresh_token)
  return expiresAt
}

describe('identity proactive refresh ownership', () => {
  let auth: ReturnType<typeof useAuthStore>

  beforeEach(() => {
    vi.useFakeTimers()
    vi.resetAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
    auth = useAuthStore()
    api.getCurrentUser.mockResolvedValue({ data: user })
    api.logout.mockResolvedValue(undefined)
  })

  afterEach(() => {
    auth.$dispose()
    vi.clearAllTimers()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  function restore(pair = tokens('initial')): void {
    localStorage.setItem('auth_user', JSON.stringify(user))
    persist({ ...pair, expires_in: 121 })
    auth.checkAuth()
  }

  it.each([true, false])('adopts committed rotation and its exact deadline (sid=%s)', async withSessionID => {
    const pending = deferred<RefreshTokenResponse>()
    const initial = tokens('initial', withSessionID)
    const rotated = tokens('rotated', withSessionID)
    const next = tokens('next', withSessionID)
    api.refreshToken.mockReturnValueOnce(pending.promise).mockImplementation(async () => {
      persist(next)
      return next
    })
    restore(initial)
    const scope = auth.identityScopeKey
    const epoch = auth.sessionEpoch
    await vi.advanceTimersByTimeAsync(1000)
    expect(api.refreshToken).toHaveBeenCalledTimes(1)

    const expiresAt = persist(rotated)
    // Delivery can lag the coordinator commit; the store must not extend the deadline.
    await vi.advanceTimersByTimeAsync(750)
    const writes = vi.spyOn(Storage.prototype, 'setItem')
    pending.resolve(rotated)
    await vi.advanceTimersByTimeAsync(0)
    expect(auth.token).toBe(rotated.access_token)
    expect(auth.user).toEqual(user)
    expect(readAuthSessionSnapshot()).toMatchObject({
      accessToken: rotated.access_token, refreshToken: rotated.refresh_token, expiresAt
    })
    expect(writes).not.toHaveBeenCalled()
    writes.mockRestore()
    if (withSessionID) {
      expect(auth.sessionEpoch).toBe(epoch)
      expect(auth.identityScopeKey).toBe(scope)
    }

    await vi.advanceTimersByTimeAsync(expiresAt - 120_000 - Date.now() - 1)
    expect(api.refreshToken).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(api.refreshToken).toHaveBeenCalledTimes(2)
    expect(auth.token).toBe(next.access_token)
  })

  it.each(['logout-pending', 'logout', 'same-user-login', 'restore', 'oauth-adoption', 'terminal-401', 'dispose'])(
    'ignores completion after %s without reviving or replacing timers', async operation => {
      const pending = deferred<RefreshTokenResponse>()
      const pendingLogout = deferred<void>()
      const pair = tokens('initial')
      api.refreshToken.mockReturnValueOnce(pending.promise)
      restore(pair)
      await vi.advanceTimersByTimeAsync(1000)
      expect(api.refreshToken).toHaveBeenCalledTimes(1)
      // Keep the pair unchanged to exercise the lifecycle fence independently of token equality.
      persist(pair)
      let logout: Promise<void> | undefined
      switch (operation) {
        case 'logout-pending':
          api.logout.mockReturnValueOnce(pendingLogout.promise)
          logout = auth.logout()
          break
        case 'logout':
          await auth.logout()
          break
        case 'same-user-login':
          api.login.mockResolvedValueOnce({ ...pair, user })
          await auth.login({ email: user.email, password: 'fixture-password' })
          break
        case 'restore':
          auth.checkAuth()
          break
        case 'oauth-adoption':
          await auth.setToken(pair.access_token)
          break
        case 'terminal-401':
          api.getCurrentUser.mockRejectedValueOnce({ status: 401 })
          await expect(auth.refreshUser()).rejects.toMatchObject({ status: 401 })
          break
        case 'dispose':
          auth.$dispose()
          break
      }
      await vi.advanceTimersByTimeAsync(0)
      const token = auth.token
      const profile = auth.user
      const snapshot = readAuthSessionSnapshot()
      const timerCount = vi.getTimerCount()
      if (operation.startsWith('logout') || operation === 'terminal-401' || operation === 'dispose') {
        expect(timerCount).toBe(0)
      }
      const schedule = vi.spyOn(globalThis, 'setTimeout')
      pending.resolve(pair)
      await vi.advanceTimersByTimeAsync(0)
      expect(auth.token).toBe(token)
      expect(auth.user).toEqual(profile)
      expect(readAuthSessionSnapshot()).toEqual(snapshot)
      expect(vi.getTimerCount()).toBe(timerCount)
      expect(schedule).not.toHaveBeenCalled()
      pendingLogout.resolve(undefined)
      await logout
    }
  )

  it.each(['access', 'refresh', 'user', 'missing-user', 'malformed-user', 'missing-expiry'])(
    'ignores a completion whose persisted %s snapshot no longer matches', async mismatch => {
      const pending = deferred<RefreshTokenResponse>()
      const initial = tokens('initial')
      const rotated = tokens('rotated')
      api.refreshToken.mockReturnValueOnce(pending.promise)
      restore(initial)
      await vi.advanceTimersByTimeAsync(1000)
      expect(api.refreshToken).toHaveBeenCalledTimes(1)
      persist(rotated)
      switch (mismatch) {
        case 'access': localStorage.setItem('auth_token', tokens('peer').access_token); break
        case 'refresh': localStorage.setItem('refresh_token', tokens('peer').refresh_token); break
        case 'user': localStorage.setItem('auth_user', JSON.stringify({ ...user, id: 2 })); break
        case 'missing-user': localStorage.removeItem('auth_user'); break
        case 'malformed-user': localStorage.setItem('auth_user', '{invalid'); break
        case 'missing-expiry': localStorage.removeItem('token_expires_at'); break
      }
      await vi.advanceTimersByTimeAsync(0)
      const snapshot = readAuthSessionSnapshot()
      const storedUser = localStorage.getItem('auth_user')
      const timerCount = vi.getTimerCount()
      const schedule = vi.spyOn(globalThis, 'setTimeout')
      pending.resolve(rotated)
      await vi.advanceTimersByTimeAsync(0)
      expect(auth.token).toBe(initial.access_token)
      expect(auth.user).toEqual(user)
      expect(readAuthSessionSnapshot()).toEqual(snapshot)
      expect(localStorage.getItem('auth_user')).toBe(storedUser)
      expect(vi.getTimerCount()).toBe(timerCount)
      expect(schedule).not.toHaveBeenCalled()
    }
  )

  it.each([
    { name: 'network', error: new Error('Offline') },
    { name: 'rate limit', error: { status: 429 } },
    { name: 'unavailable', error: { status: 503 } },
    { name: 'cancellation', error: new CanceledError('Authentication changed') }
  ])('preserves the session on $name failure', async ({ error }) => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    api.refreshToken.mockRejectedValueOnce(error)
    restore()
    const snapshot = readAuthSessionSnapshot()
    const scope = auth.identityScopeKey
    await vi.advanceTimersByTimeAsync(1000)
    expect(api.refreshToken).toHaveBeenCalledTimes(1)
    expect(auth.isAuthenticated).toBe(true)
    expect(auth.identityScopeKey).toBe(scope)
    expect(readAuthSessionSnapshot()).toEqual(snapshot)
    expect(auth.token).toBe(snapshot.accessToken)
  })
})
